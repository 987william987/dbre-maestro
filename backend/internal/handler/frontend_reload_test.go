package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFrontendReloadHandlerAcceptsValidReason(t *testing.T) {
	h := NewFrontendReloadHandler()

	body := `{"reason":"render-error","error_message":"NotFoundError: insertBefore","route":"/sql-editor","previous_signature":"a","current_signature":"b"}`
	req := httptest.NewRequest(http.MethodPost, "/frontend/reload-events", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ReportReload(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestFrontendReloadHandlerNormalizesUnknownReason(t *testing.T) {
	h := NewFrontendReloadHandler()

	body := `{"reason":"something-i-made-up"}`
	req := httptest.NewRequest(http.MethodPost, "/frontend/reload-events", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ReportReload(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestFrontendReloadHandlerMalformedBodyDoesNotFail(t *testing.T) {
	h := NewFrontendReloadHandler()

	req := httptest.NewRequest(http.MethodPost, "/frontend/reload-events", strings.NewReader(`not json`))
	rec := httptest.NewRecorder()
	h.ReportReload(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestFrontendReloadHandlerRateLimited(t *testing.T) {
	h := NewFrontendReloadHandler()
	h.rateLimiter = newRequestRateLimiter(1, time.Minute)

	firstReq := httptest.NewRequest(http.MethodPost, "/frontend/reload-events", strings.NewReader(`{"reason":"periodic-poll"}`))
	firstReq.RemoteAddr = "10.0.0.1:12345"
	firstRec := httptest.NewRecorder()
	h.ReportReload(firstRec, firstReq)
	if firstRec.Code != http.StatusNoContent {
		t.Fatalf("first status = %d, want %d", firstRec.Code, http.StatusNoContent)
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/frontend/reload-events", strings.NewReader(`{"reason":"periodic-poll"}`))
	secondReq.RemoteAddr = "10.0.0.1:12345"
	secondRec := httptest.NewRecorder()
	h.ReportReload(secondRec, secondReq)
	if secondRec.Code != http.StatusNoContent {
		t.Fatalf("second status = %d, want %d", secondRec.Code, http.StatusNoContent)
	}
}
