package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

func TestValidateBinlogExportRequestRequiresExplicitUnfilteredAcknowledgement(t *testing.T) {
	start := time.Now().UTC()
	end := start.Add(5 * time.Minute)
	_, message := validateBinlogExportRequest(createMySQLBinlogExportRequest{
		SourceConnectionID: 1, RangeMode: "time", Timezone: "UTC", StartTime: &start, EndTime: &end,
	}, 7)
	if message != "acknowledge_unfiltered is required without database and table filters" {
		t.Fatalf("validation message = %q", message)
	}
}

func TestJSONBinlogMessageErrorIncludesStableCode(t *testing.T) {
	recorder := httptest.NewRecorder()
	jsonBinlogMessageErr(recorder, 422, "end_time cannot be in the future")
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "invalid_range" || body["error"] == "" {
		t.Fatalf("body = %#v", body)
	}
}

func TestBinlogInventoryChangedInvalidatesTimestampCacheOnRotationOrGrowth(t *testing.T) {
	base := []binlogFileInfo{{Name: "mysql-bin.000001", SizeBytes: 100}}
	if binlogInventoryChanged(base, []binlogFileInfo{{Name: "mysql-bin.000001", SizeBytes: 100}}) {
		t.Fatal("identical inventory must keep timestamp cache")
	}
	if !binlogInventoryChanged(base, []binlogFileInfo{{Name: "mysql-bin.000001", SizeBytes: 101}}) {
		t.Fatal("active file growth must invalidate timestamp cache")
	}
	if !binlogInventoryChanged(base, append(base, binlogFileInfo{Name: "mysql-bin.000002", SizeBytes: 4})) {
		t.Fatal("binlog rotation must invalidate timestamp cache")
	}
}

func TestValidateBinlogExportRequestAppliesStricterUnfilteredLimit(t *testing.T) {
	start := time.Now().UTC()
	end := start.Add(unfilteredBinlogExportMaxRange + time.Second)
	_, message := validateBinlogExportRequest(createMySQLBinlogExportRequest{
		SourceConnectionID: 1, RangeMode: "time", Timezone: "UTC", StartTime: &start, EndTime: &end, AcknowledgeUnfiltered: true,
	}, 7)
	if !strings.Contains(message, "time range exceeds") {
		t.Fatalf("validation message = %q", message)
	}
}

func TestValidateBinlogExportRequestPreservesCaseSensitiveTableNames(t *testing.T) {
	end := time.Now().UTC().Add(-time.Second)
	start := end.Add(-time.Hour)
	database := "AppDB"
	job, message := validateBinlogExportRequest(createMySQLBinlogExportRequest{
		SourceConnectionID: 1, RangeMode: "time", Timezone: "Asia/Taipei", StartTime: &start, EndTime: &end,
		Database: &database, Tables: []string{"OrderItems"}, DMLTypes: []string{"UPDATE"},
	}, 7)
	if message != "" {
		t.Fatal(message)
	}
	if job.SourceTables[0] != "OrderItems" || job.DMLTypes[0] != "update" {
		t.Fatalf("unexpected normalized filters: tables=%v dml=%v", job.SourceTables, job.DMLTypes)
	}
}

func TestValidateBinlogExportRequestRejectsCrossFileUnfilteredPositionRange(t *testing.T) {
	startFile, endFile := "mysql-bin.000001", "mysql-bin.000002"
	startPos, endPos := uint64(4), uint64(900)
	_, message := validateBinlogExportRequest(createMySQLBinlogExportRequest{
		SourceConnectionID: 1, RangeMode: "position", StartFile: &startFile, StartPos: &startPos,
		EndFile: &endFile, EndPos: &endPos, AcknowledgeUnfiltered: true,
	}, 7)
	if message != "unfiltered position export must stay within one binlog file" {
		t.Fatalf("validation message = %q", message)
	}
}

func TestValidateBinlogExportRequestRejectsFutureEndTime(t *testing.T) {
	end := time.Now().UTC().Add(time.Minute)
	start := end.Add(-time.Minute)
	database := "app"
	_, message := validateBinlogExportRequest(createMySQLBinlogExportRequest{
		SourceConnectionID: 1, RangeMode: "time", Timezone: "UTC", StartTime: &start, EndTime: &end, Database: &database,
	}, 7)
	if message != "end_time cannot be in the future" {
		t.Fatalf("validation message = %q", message)
	}
}

func TestValidateAndSnapshotBinlogRangeFreezesTimeEndPosition(t *testing.T) {
	start := time.Now().UTC().Add(-time.Minute)
	end := time.Now().UTC()
	job := &model.MySQLBinlogExportJob{RangeMode: "time", RequestedStartTime: &start, RequestedEndTime: &end}
	files := []binlogFileInfo{{Name: "mysql-bin.000001", SizeBytes: 900}}
	code, message := validateAndSnapshotBinlogRange(job, files, mysqlBinlogPosition{File: "mysql-bin.000001", Pos: 850})
	if code != "" || message != "" || job.RequestedEndFile == nil || *job.RequestedEndFile != "mysql-bin.000001" || job.RequestedEndPos == nil || *job.RequestedEndPos != 850 {
		t.Fatalf("snapshot = (%q, %q, %#v)", code, message, job)
	}
}

func TestValidateAndSnapshotBinlogRangeRejectsPurgedAndOutOfBoundsPositions(t *testing.T) {
	startFile, endFile := "mysql-bin.000001", "mysql-bin.000002"
	startPos, endPos := uint64(4), uint64(901)
	job := &model.MySQLBinlogExportJob{RangeMode: "position", RequestedStartFile: &startFile, RequestedStartPos: &startPos, RequestedEndFile: &endFile, RequestedEndPos: &endPos}
	files := []binlogFileInfo{{Name: startFile, SizeBytes: 500}, {Name: endFile, SizeBytes: 900}}
	code, _ := validateAndSnapshotBinlogRange(job, files, mysqlBinlogPosition{File: endFile, Pos: 900})
	if code != "binlog_position_invalid" {
		t.Fatalf("code = %q", code)
	}
	missing := "mysql-bin.000000"
	job.RequestedStartFile = &missing
	code, _ = validateAndSnapshotBinlogRange(job, files, mysqlBinlogPosition{File: endFile, Pos: 900})
	if code != "binlog_purged" {
		t.Fatalf("code = %q", code)
	}
}

func TestValidateBinlogExportRequestRejectsUnsafeIdentifiersAndDMLTypes(t *testing.T) {
	end := time.Now().UTC().Add(-time.Second)
	start := end.Add(-time.Minute)
	for name, req := range map[string]createMySQLBinlogExportRequest{
		"database": {SourceConnectionID: 1, RangeMode: "time", StartTime: &start, EndTime: &end, Database: stringPtr("app; DROP DATABASE app")},
		"table":    {SourceConnectionID: 1, RangeMode: "time", StartTime: &start, EndTime: &end, Database: stringPtr("app"), Tables: []string{"orders --"}},
		"dml":      {SourceConnectionID: 1, RangeMode: "time", StartTime: &start, EndTime: &end, Database: stringPtr("app"), DMLTypes: []string{"select"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, message := validateBinlogExportRequest(req, 7); message == "" {
				t.Fatal("unsafe filter unexpectedly passed validation")
			}
		})
	}
}

func TestBinlogExportGetHidesJobAfterDBScopeRevocation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	sqlDB := sqlx.NewDb(db, "sqlmock")
	h := NewMySQLBinlogExportHandler(
		repository.NewMySQLBinlogExportRepo(sqlDB, []byte("01234567890123456789012345678901")), nil,
		repository.NewUserRepo(sqlDB), nil, nil,
	)
	mock.ExpectQuery("SELECT \\* FROM mysql_binlog_export_jobs WHERE id = \\?").WithArgs(uint64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "source_connection_id"}).AddRow(42, 99))
	mock.ExpectQuery("SELECT \\* FROM users WHERE id = \\?").WithArgs(uint64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "is_protected"}).AddRow(7, false))
	mock.ExpectQuery("SELECT EXISTS").WithArgs(uint64(7), sqlmock.AnyArg(), uint64(7), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("SELECT DISTINCT db_connection_id").
		WithArgs(uint64(7), uint64(7), sqlmock.AnyArg(), uint64(7), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"db_connection_id"}))

	req := httptest.NewRequest(http.MethodGet, "/api/binlog-exports/42", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, uint64(7))
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("id", "42")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, routeCtx)
	recorder := httptest.NewRecorder()
	h.Get(recorder, req.WithContext(ctx))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
