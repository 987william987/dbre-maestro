package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/onlineddl"
	"github.com/go-chi/chi/v5"
)

func TestWriteOnlineDDLErrorUsesStableStatusAndCode(t *testing.T) {
	tests := []struct {
		err  error
		code int
	}{
		{onlineddl.ErrStaleVersion, http.StatusConflict},
		{onlineddl.ErrConflict, http.StatusConflict},
		{onlineddl.ErrInvalidTransition, http.StatusConflict},
		{onlineddl.ErrInvalidParameters, http.StatusUnprocessableEntity},
		{onlineddl.ErrUnsupportedStatement, http.StatusUnprocessableEntity},
		{onlineddl.ErrPreflightChanged, http.StatusUnprocessableEntity},
		{onlineddl.ErrModeDisabled, http.StatusUnprocessableEntity},
		{onlineddl.ErrControlNotSupported, http.StatusUnprocessableEntity},
		{onlineddl.ErrToolUnavailable, http.StatusServiceUnavailable},
		{errors.New("storage failed"), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		recorder := httptest.NewRecorder()
		writeOnlineDDLError(recorder, tt.err)
		if recorder.Code != tt.code || !strings.Contains(recorder.Body.String(), `"error":"`+tt.err.Error()+`"`) {
			t.Fatalf("err=%v status=%d body=%s", tt.err, recorder.Code, recorder.Body.String())
		}
	}
}

func TestOnlineDDLDryRunFailsClosedWithoutCoordinator(t *testing.T) {
	handler := &TicketHandler{}
	router := chi.NewRouter()
	router.Post("/tickets/{id}/online-ddl/dry-run", handler.OnlineDDLDryRun)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/tickets/TK-1/online-ddl/dry-run", strings.NewReader(`{"execution_id":7,"mode":"gh-ost","parameters":{}}`))
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), onlineddl.ErrToolUnavailable.Error()) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestOnlineDDLDryRunRequiresExecutionID(t *testing.T) {
	handler := &TicketHandler{onlineDDL: onlineDDLServiceStub{}}
	router := chi.NewRouter()
	router.Post("/tickets/{id}/online-ddl/dry-run", handler.OnlineDDLDryRun)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/tickets/TK-1/online-ddl/dry-run", strings.NewReader(`{"mode":"gh-ost","parameters":{}}`))
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), onlineddl.ErrInvalidParameters.Error()) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

type onlineDDLServiceStub struct{ TicketOnlineDDLService }

type onlineDDLQueueStub struct {
	run      *model.OnlineDDLRun
	getErr   error
	queueErr error
	queued   int
	mode     string
}

func (s *onlineDDLQueueStub) Get(context.Context, uint64, uint64) (*model.OnlineDDLRun, error) {
	return s.run, s.getErr
}
func (s *onlineDDLQueueStub) Queue(_ context.Context, _ *model.Ticket, _ *model.TicketExecution, _ uint64, mode string, _ onlineddl.Parameters) (*model.OnlineDDLRun, error) {
	s.queued++
	s.mode = mode
	return s.run, s.queueErr
}
func (*onlineDDLQueueStub) DryRun(context.Context, *model.Ticket, *model.TicketExecution, uint64, string, onlineddl.Parameters) (any, error) {
	return nil, nil
}
func (*onlineDDLQueueStub) Pause(context.Context, *model.OnlineDDLRun, uint64, uint64) (*model.OnlineDDLRun, error) {
	return nil, nil
}
func (*onlineDDLQueueStub) Resume(context.Context, *model.OnlineDDLRun, uint64, uint64) (*model.OnlineDDLRun, error) {
	return nil, nil
}
func (*onlineDDLQueueStub) Cancel(context.Context, *model.OnlineDDLRun, uint64, uint64) (*model.OnlineDDLRun, error) {
	return nil, nil
}
func (*onlineDDLQueueStub) Tune(context.Context, *model.OnlineDDLRun, uint64, uint64, onlineddl.RuntimeTuning) (*model.OnlineDDLRun, error) {
	return nil, nil
}

func TestQueueOnlineDDLUsesModeChosenByExecuteRequest(t *testing.T) {
	ticket := &model.Ticket{ID: 2}
	execution := &model.TicketExecution{ID: 3, TicketID: 2}
	service := &onlineDDLQueueStub{}
	handler := &TicketHandler{onlineDDL: service}
	service.queueErr = onlineddl.ErrPreflightChanged
	queued, err := handler.queueOnlineDDL(context.Background(), ticket, execution, 4, onlineddl.ModeGhost, onlineddl.DefaultParameters(onlineddl.ModeGhost))
	if !errors.Is(err, onlineddl.ErrPreflightChanged) || queued || service.queued != 1 {
		t.Fatalf("drift queued=%v calls=%d err=%v", queued, service.queued, err)
	}
	if service.mode != onlineddl.ModeGhost {
		t.Fatalf("mode=%q", service.mode)
	}
}

func TestCanonicalOnlineDDLToolVersionOnlyAcceptsPinnedVersionText(t *testing.T) {
	if got := canonicalOnlineDDLToolVersion(onlineddl.ModeGhost, "gh-ost 1.1.6"); got != "1.1.6" {
		t.Fatalf("ghost=%q", got)
	}
	if got := canonicalOnlineDDLToolVersion(onlineddl.ModePTOSC, "pt-online-schema-change 3.7.1"); got != "3.7.1" {
		t.Fatalf("ptosc=%q", got)
	}
	if got := canonicalOnlineDDLToolVersion(onlineddl.ModeGhost, "gh-ost 1.1.5"); got != "gh-ost 1.1.5" {
		t.Fatalf("unsupported=%q", got)
	}
}

func TestOnlineDDLOnlySupportsMySQLConnections(t *testing.T) {
	if !onlineDDLSupportsConnection(&model.DBConnection{DBType: " mysql "}) {
		t.Fatal("mysql connection must support online DDL tools")
	}
	for _, dbType := range []string{"postgres", "postgresql", "redis", "mariadb", ""} {
		if onlineDDLSupportsConnection(&model.DBConnection{DBType: dbType}) {
			t.Fatalf("db type %q unexpectedly supports online DDL tools", dbType)
		}
	}
}

func TestOnlineDDLOutcomeEvidenceUsesBaselineOnlyForAmbiguousExit(t *testing.T) {
	baseline := [32]byte{1}
	artifacts := onlineddl.ArtifactSummary{Items: []onlineddl.Artifact{{Kind: "ghost_table", Exists: true}}}
	unchanged := onlineDDLOutcomeEvidence(errors.New("tool failed"), false, baseline, baseline, onlineddl.ArtifactSummary{})
	if !unchanged.OriginalUnchanged || unchanged.TargetDDLApplied || unchanged.ProcessSucceeded {
		t.Fatalf("unchanged=%+v", unchanged)
	}
	changed := onlineDDLOutcomeEvidence(errors.New("cancelled"), true, baseline, [32]byte{2}, artifacts)
	if !changed.CancelRequested || !changed.TargetDDLApplied || changed.OriginalUnchanged || !changed.Artifacts.HasArtifacts() {
		t.Fatalf("changed=%+v", changed)
	}
}

func TestOnlineDDLCreateHashIgnoresOnlyAutoIncrementCounter(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SHOW CREATE TABLE").WillReturnRows(sqlmock.NewRows([]string{"Table", "Create Table"}).AddRow("orders", "CREATE TABLE `orders` (`id` bigint) ENGINE=InnoDB AUTO_INCREMENT=10 DEFAULT CHARSET=utf8mb4"))
	first, err := onlineDDLCreateHash(context.Background(), db, "app", "orders")
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SHOW CREATE TABLE").WillReturnRows(sqlmock.NewRows([]string{"Table", "Create Table"}).AddRow("orders", "CREATE TABLE `orders` (`id` bigint) ENGINE=InnoDB AUTO_INCREMENT=99 DEFAULT CHARSET=utf8mb4"))
	second, err := onlineDDLCreateHash(context.Background(), db, "app", "orders")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("AUTO_INCREMENT counter changed DDL identity")
	}
}
