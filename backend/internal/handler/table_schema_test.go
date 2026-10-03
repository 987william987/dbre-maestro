package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/dbre-maestro/maestro/internal/tableschema"
	"github.com/go-chi/chi/v5"
)

func tableSchemaRequest(method, target, connectionID string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	ctx := context.WithValue(r.Context(), middleware.CtxUserID, uint64(7))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("connectionID", connectionID)
	return r.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
}

type tableSchemaSyncJobStoreStub struct {
	jobs         []model.TableSchemaSyncJob
	job          *model.TableSchemaSyncJob
	items        []model.TableSchemaSyncJobItem
	cancelled    bool
	created      repository.TableSchemaSyncCreateInput
	createResult *model.TableSchemaSyncJob
	createErr    error
	total        uint64
}

func (s *tableSchemaSyncJobStoreStub) Create(_ context.Context, input repository.TableSchemaSyncCreateInput) (*model.TableSchemaSyncJob, error) {
	s.created = input
	if s.createErr != nil {
		return nil, s.createErr
	}
	if s.createResult != nil {
		return s.createResult, nil
	}
	return s.job, nil
}

func TestTableSchemaRetryCreatesLinkedJobFromVerifiedPlan(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	prior := &model.TableSchemaSyncJob{ID: 42, SourceConnectionID: 1, SourceDatabase: "source", TargetConnectionID: 2, TargetDatabase: "target", TransformationConfig: []byte(`{}`), Status: model.TableSchemaSyncFailed}
	created := *prior
	created.ID = 43
	created.RetryOfJobID = &prior.ID
	store := &tableSchemaSyncJobStoreStub{job: prior, createResult: &created, items: []model.TableSchemaSyncJobItem{{ID: 1, TableName: "orders"}}}
	audits := 0
	h := &TableSchemaHandler{
		syncJobs:  store,
		canAccess: func(context.Context, uint64, uint64) (bool, error) { return true, nil },
		get: func(_ context.Context, id uint64) (*model.DBConnection, error) {
			return &model.DBConnection{ID: id, DBType: "mysql"}, nil
		},
		open: func(context.Context, tableschema.CredentialResolver, *model.DBConnection) (*sql.DB, error) {
			return db, nil
		},
		openRW: func(context.Context, tableschema.CredentialResolver, *model.DBConnection) (*sql.DB, error) {
			return db, nil
		},
		buildRetry: func(context.Context, tableschema.Queryer, tableschema.Queryer, *model.TableSchemaSyncJob, []model.TableSchemaSyncJobItem) ([]tableschema.RetryItem, error) {
			return []tableschema.RetryItem{{TableName: "orders", DependencyOrder: 1, SourceDDLSHA256: "hash"}}, nil
		},
		logAudit: func(context.Context, repository.AuditEntry) error { audits++; return nil },
	}
	recorder := httptest.NewRecorder()
	h.RetrySyncJob(recorder, tableSchemaJobRequest(http.MethodPost, "/42/retry", "42"))
	if recorder.Code != http.StatusCreated || store.created.RetryOfJobID == nil || *store.created.RetryOfJobID != 42 || audits != 1 {
		t.Fatalf("response = %d %s, input = %+v, audits = %d", recorder.Code, recorder.Body.String(), store.created, audits)
	}
	store.createResult = nil
	store.createErr = repository.ErrTableSchemaSyncTargetActive
	recorder = httptest.NewRecorder()
	h.RetrySyncJob(recorder, tableSchemaJobRequest(http.MethodPost, "/42/retry", "42"))
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), `"code":"sync_conflict"`) || audits != 1 {
		t.Fatalf("conflict response = %d %s, audits = %d", recorder.Code, recorder.Body.String(), audits)
	}
}
func (s *tableSchemaSyncJobStoreStub) List(context.Context, uint, uint) ([]model.TableSchemaSyncJob, error) {
	return s.jobs, nil
}
func (s *tableSchemaSyncJobStoreStub) ListScoped(context.Context, []uint64, uint, uint) ([]model.TableSchemaSyncJob, uint64, error) {
	return s.jobs, s.total, nil
}
func (s *tableSchemaSyncJobStoreStub) GetByID(context.Context, uint64) (*model.TableSchemaSyncJob, error) {
	return s.job, nil
}
func (s *tableSchemaSyncJobStoreStub) ListItems(context.Context, uint64) ([]model.TableSchemaSyncJobItem, error) {
	return s.items, nil
}
func (s *tableSchemaSyncJobStoreStub) RequestCancel(context.Context, uint64, uint64) (bool, error) {
	return s.cancelled, nil
}

func tableSchemaJobRequest(method, target, id string) *http.Request {
	r := tableSchemaRequest(method, target, "")
	rctx := chi.RouteContext(r.Context())
	rctx.URLParams.Add("id", id)
	return r
}

func TestTableSchemaSyncJobLifecycleEnforcesScopeAndState(t *testing.T) {
	allowed := map[uint64]bool{1: true, 2: true}
	canAccess := func(_ context.Context, _ uint64, connectionID uint64) (bool, error) {
		return allowed[connectionID], nil
	}
	job := model.TableSchemaSyncJob{ID: 42, SourceConnectionID: 1, TargetConnectionID: 2, Status: model.TableSchemaSyncRunning}

	t.Run("list hides jobs unless both connection scopes are allowed", func(t *testing.T) {
		store := &tableSchemaSyncJobStoreStub{jobs: []model.TableSchemaSyncJob{job}, total: 1}
		h := &TableSchemaHandler{syncJobs: store, canAccess: canAccess, list: func(context.Context, uint64) ([]model.DBConnection, error) {
			return []model.DBConnection{{ID: 1}, {ID: 2}}, nil
		}}
		recorder := httptest.NewRecorder()
		h.ListSyncJobs(recorder, tableSchemaRequest(http.MethodGet, "/", ""))
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"id":42`) || !strings.Contains(recorder.Body.String(), `"total":1`) {
			t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("detail does not disclose an out of scope job", func(t *testing.T) {
		store := &tableSchemaSyncJobStoreStub{job: &model.TableSchemaSyncJob{ID: 42, SourceConnectionID: 1, TargetConnectionID: 99}}
		h := &TableSchemaHandler{syncJobs: store, canAccess: canAccess}
		recorder := httptest.NewRecorder()
		h.GetSyncJob(recorder, tableSchemaJobRequest(http.MethodGet, "/42", "42"))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("cancel records audit only after a conditional transition", func(t *testing.T) {
		store := &tableSchemaSyncJobStoreStub{job: &job, cancelled: true}
		audits := 0
		h := &TableSchemaHandler{syncJobs: store, canAccess: canAccess, logAudit: func(context.Context, repository.AuditEntry) error { audits++; return nil }}
		recorder := httptest.NewRecorder()
		h.CancelSyncJob(recorder, tableSchemaJobRequest(http.MethodPost, "/42/cancel", "42"))
		if recorder.Code != http.StatusOK || audits != 1 {
			t.Fatalf("response = %d %s, audits = %d", recorder.Code, recorder.Body.String(), audits)
		}

		store.cancelled = false
		recorder = httptest.NewRecorder()
		h.CancelSyncJob(recorder, tableSchemaJobRequest(http.MethodPost, "/42/cancel", "42"))
		if recorder.Code != http.StatusConflict || audits != 1 {
			t.Fatalf("response = %d %s, audits = %d", recorder.Code, recorder.Body.String(), audits)
		}
	})
}

func TestTableSchemaConnectionsAreScopedToMySQL(t *testing.T) {
	h := &TableSchemaHandler{list: func(context.Context, uint64) ([]model.DBConnection, error) {
		return []model.DBConnection{{ID: 1, Name: "mysql", DBType: "mysql"}, {ID: 2, Name: "pg", DBType: "postgres"}}, nil
	}}
	recorder := httptest.NewRecorder()
	h.Connections(recorder, tableSchemaRequest(http.MethodGet, "/", ""))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"name":"mysql"`) || strings.Contains(recorder.Body.String(), `"name":"pg"`) {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestTableSchemaMetadataRejectsOutOfScopeAndNonMySQL(t *testing.T) {
	t.Run("scope", func(t *testing.T) {
		h := &TableSchemaHandler{canAccess: func(context.Context, uint64, uint64) (bool, error) { return false, nil }}
		recorder := httptest.NewRecorder()
		h.Databases(recorder, tableSchemaRequest(http.MethodGet, "/", "9"))
		if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"source_scope_denied"`) {
			t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
		}
	})
	t.Run("non mysql or deleted", func(t *testing.T) {
		h := &TableSchemaHandler{
			canAccess: func(context.Context, uint64, uint64) (bool, error) { return true, nil },
			get: func(context.Context, uint64) (*model.DBConnection, error) {
				return &model.DBConnection{DBType: "postgres"}, nil
			},
		}
		recorder := httptest.NewRecorder()
		h.Databases(recorder, tableSchemaRequest(http.MethodGet, "/", "9"))
		if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), `"code":"unsupported_engine"`) {
			t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
		}
	})
}

func TestTableSchemaReadonlyConnectionFailureIsStable(t *testing.T) {
	h := &TableSchemaHandler{
		canAccess: func(context.Context, uint64, uint64) (bool, error) { return true, nil },
		get: func(context.Context, uint64) (*model.DBConnection, error) {
			return &model.DBConnection{DBType: "mysql"}, nil
		},
		open: func(context.Context, tableschema.CredentialResolver, *model.DBConnection) (*sql.DB, error) {
			return nil, errors.New("credential missing")
		},
	}
	recorder := httptest.NewRecorder()
	h.Databases(recorder, tableSchemaRequest(http.MethodGet, "/", "9"))
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), `"code":"connection_unavailable"`) {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestTableSchemaDatabasesAndTables(t *testing.T) {
	newHandler := func(db *sql.DB) *TableSchemaHandler {
		return &TableSchemaHandler{
			canAccess: func(context.Context, uint64, uint64) (bool, error) { return true, nil },
			get: func(context.Context, uint64) (*model.DBConnection, error) {
				return &model.DBConnection{DBType: "mysql"}, nil
			},
			open: func(context.Context, tableschema.CredentialResolver, *model.DBConnection) (*sql.DB, error) {
				return db, nil
			},
		}
	}
	t.Run("databases", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		mock.ExpectQuery("SELECT SCHEMA_NAME").WillReturnRows(sqlmock.NewRows([]string{"SCHEMA_NAME"}).AddRow("app"))
		recorder := httptest.NewRecorder()
		newHandler(db).Databases(recorder, tableSchemaRequest(http.MethodGet, "/", "9"))
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"app"`) {
			t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
		}
	})
	t.Run("tables and dependencies", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		mock.ExpectQuery("FROM information_schema.TABLES").WithArgs("app").WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME", "ENGINE", "CHARACTER_SET_NAME", "TABLE_COLLATION", "ROW_FORMAT", "AUTO_INCREMENT"}).AddRow("orders", "InnoDB", "utf8mb4", "utf8mb4_unicode_ci", "Dynamic", nil))
		mock.ExpectQuery("FROM information_schema.KEY_COLUMN_USAGE").WithArgs("app").WillReturnRows(sqlmock.NewRows([]string{"TABLE_SCHEMA", "TABLE_NAME", "REFERENCED_TABLE_SCHEMA", "REFERENCED_TABLE_NAME"}).AddRow("app", "orders", "app", "users"))
		recorder := httptest.NewRecorder()
		newHandler(db).Tables(recorder, tableSchemaRequest(http.MethodGet, "/?database=app", "9"))
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"referenced_table":"users"`) {
			t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
		}
	})
}

func TestTableSchemaTablesRequiresDatabaseBeforeOpeningConnection(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&TableSchemaHandler{}).Tables(recorder, tableSchemaRequest(http.MethodGet, "/", "9"))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"invalid_table_selection"`) {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestTableSchemaSyncJobRejectsInvalidBody(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&TableSchemaHandler{}).CreateSyncJob(recorder, httptest.NewRequest(http.MethodPost, "/", nil))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"invalid_table_selection"`) {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func exportHandlerForTest(t *testing.T, result *tableschema.ExportResult, buildErr error, audit func(repository.AuditEntry)) *TableSchemaHandler {
	t.Helper()
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &TableSchemaHandler{
		canAccess: func(context.Context, uint64, uint64) (bool, error) { return true, nil },
		get: func(context.Context, uint64) (*model.DBConnection, error) {
			return &model.DBConnection{ID: 9, DBType: "mysql"}, nil
		},
		open: func(context.Context, tableschema.CredentialResolver, *model.DBConnection) (*sql.DB, error) {
			return db, nil
		},
		buildExport: func(context.Context, tableschema.Queryer, tableschema.ExportRequest) (*tableschema.ExportResult, error) {
			return result, buildErr
		},
		logAudit: func(_ context.Context, entry repository.AuditEntry) error {
			if audit != nil {
				audit(entry)
			}
			return nil
		},
	}
}

func exportRequestBody() string {
	return `{"source":{"connection_id":9,"database":"sales","tables":["orders"]},"transformation":{"reset_auto_increment":true,"engine":"","charset":"","collation":"","row_format":""}}`
}

func TestTableSchemaExportPreviewAndDownloadSharePreparedResult(t *testing.T) {
	result := &tableschema.ExportResult{Database: "sales", Order: []string{"orders"}, Tables: []tableschema.ExportTable{{Name: "orders"}}, Warnings: []string{}, ExternalDependencies: []tableschema.Dependency{}, Script: "CREATE TABLE `sales`.`orders` (`id` bigint);\n"}
	t.Run("preview", func(t *testing.T) {
		var action string
		h := exportHandlerForTest(t, result, nil, func(entry repository.AuditEntry) { action = entry.ActionType })
		r := tableSchemaRequest(http.MethodPost, "/", "")
		r.Body = io.NopCloser(strings.NewReader(exportRequestBody()))
		recorder := httptest.NewRecorder()
		h.ExportPreview(recorder, r)
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"script":"CREATE TABLE`) || action != "table_schema_export_preview" {
			t.Fatalf("response = %d %s, audit = %q", recorder.Code, recorder.Body.String(), action)
		}
	})
	t.Run("download", func(t *testing.T) {
		h := exportHandlerForTest(t, result, nil, nil)
		r := tableSchemaRequest(http.MethodPost, "/", "")
		r.Body = io.NopCloser(strings.NewReader(strings.Replace(exportRequestBody(), `"sales"`, `"Sales\r\nInjected"`, 1)))
		recorder := httptest.NewRecorder()
		h.Export(recorder, r)
		if recorder.Code != http.StatusOK || recorder.Body.String() != result.Script || recorder.Header().Get("Content-Disposition") != `attachment; filename="table-schema-sales-injected.sql"` {
			t.Fatalf("response = %d headers=%v body=%q", recorder.Code, recorder.Header(), recorder.Body.String())
		}
	})
}

func TestTableSchemaExportErrorsRemainStable(t *testing.T) {
	h := exportHandlerForTest(t, nil, &tableschema.StableError{Code: tableschema.ErrorExportTooLarge, Message: "too large"}, nil)
	r := tableSchemaRequest(http.MethodPost, "/", "")
	r.Body = io.NopCloser(strings.NewReader(exportRequestBody()))
	recorder := httptest.NewRecorder()
	h.ExportPreview(recorder, r)
	if recorder.Code != http.StatusRequestEntityTooLarge || !strings.Contains(recorder.Body.String(), `"code":"export_too_large"`) {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestTableSchemaExportRejectsOversizedOrUnknownRequestInput(t *testing.T) {
	for name, body := range map[string]string{
		"oversized":     exportRequestBody() + strings.Repeat(" ", 65<<10),
		"unknown field": strings.TrimSuffix(exportRequestBody(), "}") + `,"unexpected":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			r := tableSchemaRequest(http.MethodPost, "/", "")
			r.Body = io.NopCloser(strings.NewReader(body))
			recorder := httptest.NewRecorder()
			(&TableSchemaHandler{}).ExportPreview(recorder, r)
			if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"invalid_table_selection"`) {
				t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestTableSchemaExportAuditNeverContainsDDL(t *testing.T) {
	request := tableschema.ExportRequest{Source: tableschema.SourceSelection{ConnectionID: 9, Database: "sales", Tables: []string{"orders"}}}
	result := &tableschema.ExportResult{Tables: []tableschema.ExportTable{{Name: "orders"}}, Script: "CREATE TABLE secret"}
	raw, err := json.Marshal(tableSchemaExportAuditDetails(request, result, "succeeded", ""))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "CREATE TABLE") || strings.Contains(string(raw), "secret") {
		t.Fatalf("audit leaked DDL: %s", raw)
	}
}

type failingResponseWriter struct {
	header http.Header
}

func (w *failingResponseWriter) Header() http.Header { return w.header }
func (w *failingResponseWriter) WriteHeader(int)     {}
func (w *failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("client disconnected")
}

func TestTableSchemaDownloadAuditsClientWriteFailure(t *testing.T) {
	result := &tableschema.ExportResult{Database: "sales", Tables: []tableschema.ExportTable{{Name: "orders"}}, Script: "CREATE TABLE `sales`.`orders` (`id` bigint);\n"}
	var details map[string]any
	h := exportHandlerForTest(t, result, nil, func(entry repository.AuditEntry) {
		details, _ = entry.Details.(map[string]any)
	})
	r := tableSchemaRequest(http.MethodPost, "/", "")
	r.Body = io.NopCloser(strings.NewReader(exportRequestBody()))
	h.Export(&failingResponseWriter{header: http.Header{}}, r)
	if details["status"] != "failed" || details["error_code"] != "client_write_failed" {
		t.Fatalf("audit details = %#v", details)
	}
}

func TestTableSchemaExportTimeoutHasStableResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeTableSchemaExportError(recorder, context.DeadlineExceeded, tableschema.ErrorMetadataQueryFailed)
	if recorder.Code != http.StatusGatewayTimeout || !strings.Contains(recorder.Body.String(), `"code":"metadata_query_failed"`) {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestTableSchemaSyncTargetEnforcesScopeAndReadwriteOpener(t *testing.T) {
	t.Run("scope denied", func(t *testing.T) {
		h := &TableSchemaHandler{canAccess: func(context.Context, uint64, uint64) (bool, error) { return false, nil }}
		recorder := httptest.NewRecorder()
		h.openScopedTargetMySQL(recorder, tableSchemaRequest(http.MethodPost, "/", ""), 2)
		if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"target_scope_denied"`) {
			t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
		}
	})
	t.Run("readwrite opener", func(t *testing.T) {
		db, _, _ := sqlmock.New()
		defer db.Close()
		called := false
		h := &TableSchemaHandler{connections: nil, canAccess: func(context.Context, uint64, uint64) (bool, error) { return true, nil }, get: func(context.Context, uint64) (*model.DBConnection, error) {
			return &model.DBConnection{DBType: "mysql"}, nil
		}, openRW: func(context.Context, tableschema.CredentialResolver, *model.DBConnection) (*sql.DB, error) {
			called = true
			return db, nil
		}}
		recorder := httptest.NewRecorder()
		opened, ok := h.openScopedTargetMySQL(recorder, tableSchemaRequest(http.MethodPost, "/", ""), 2)
		if !ok || !called {
			t.Fatalf("ok=%v called=%v response=%s", ok, called, recorder.Body.String())
		}
		_ = opened.Close()
	})
}
