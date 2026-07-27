package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/jmoiron/sqlx"
)

func newTestFrontendReloadHandler(t *testing.T) (*FrontendReloadHandler, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	h := NewFrontendReloadHandler(repository.NewAuditRepo(sqlxDB))
	return h, mock
}

func TestFrontendReloadHandlerLogsValidReason(t *testing.T) {
	h, mock := newTestFrontendReloadHandler(t)

	mock.ExpectExec(`INSERT INTO audit_logs`).
		WithArgs(nil, "", "frontend_stale_bundle_reload", "frontend", nil, auditDetailsReason("render-error"), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	body := `{"reason":"render-error","error_message":"NotFoundError: insertBefore","route":"/sql-editor","previous_signature":"a","current_signature":"b"}`
	req := httptest.NewRequest(http.MethodPost, "/frontend/reload-events", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ReportReload(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFrontendReloadHandlerNormalizesUnknownReason(t *testing.T) {
	h, mock := newTestFrontendReloadHandler(t)

	mock.ExpectExec(`INSERT INTO audit_logs`).
		WithArgs(nil, "", "frontend_stale_bundle_reload", "frontend", nil, auditDetailsReason("unknown"), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	body := `{"reason":"something-i-made-up"}`
	req := httptest.NewRequest(http.MethodPost, "/frontend/reload-events", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ReportReload(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFrontendReloadHandlerMalformedBodyDoesNotWrite(t *testing.T) {
	h, mock := newTestFrontendReloadHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/frontend/reload-events", strings.NewReader(`not json`))
	rec := httptest.NewRecorder()
	h.ReportReload(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected DB call: %v", err)
	}
}

func TestFrontendReloadHandlerRateLimited(t *testing.T) {
	h, mock := newTestFrontendReloadHandler(t)
	h.rateLimiter = newRequestRateLimiter(1, time.Minute)

	mock.ExpectExec(`INSERT INTO audit_logs`).
		WithArgs(nil, "", "frontend_stale_bundle_reload", "frontend", nil, auditDetailsReason("periodic-poll"), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	firstReq := httptest.NewRequest(http.MethodPost, "/frontend/reload-events", strings.NewReader(`{"reason":"periodic-poll"}`))
	firstReq.RemoteAddr = "10.0.0.1:12345"
	firstRec := httptest.NewRecorder()
	h.ReportReload(firstRec, firstReq)
	if firstRec.Code != http.StatusNoContent {
		t.Fatalf("first status = %d, want %d", firstRec.Code, http.StatusNoContent)
	}

	// Second request from the same IP within the window must be dropped
	// silently (no second INSERT expectation set up above — sqlmock will
	// fail the test if ReportReload tries to write again).
	secondReq := httptest.NewRequest(http.MethodPost, "/frontend/reload-events", strings.NewReader(`{"reason":"periodic-poll"}`))
	secondReq.RemoteAddr = "10.0.0.1:12345"
	secondRec := httptest.NewRecorder()
	h.ReportReload(secondRec, secondReq)
	if secondRec.Code != http.StatusNoContent {
		t.Fatalf("second status = %d, want %d", secondRec.Code, http.StatusNoContent)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
