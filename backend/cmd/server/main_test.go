package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/go-chi/chi/v5"
)

func TestRequireTicketsWorkspaceReadAllowsTicketsRead(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tickets", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.CtxPermissions, []string{"tickets.read"}))

	called := false
	handler := requireTicketsWorkspaceRead(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("handler should be called for tickets.read")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequireTicketsWorkspaceReadRejectsTicketsApply(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tickets", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.CtxPermissions, []string{"tickets.apply"}))

	handler := requireTicketsWorkspaceRead(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestRequireTicketsWorkspaceReadRejectsUnrelatedPermission(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tickets", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.CtxPermissions, []string{"masking_rules.read"}))

	handler := requireTicketsWorkspaceRead(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestRedactedRequestURIRedactsExportDownloadToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/exports/download/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef?foo=bar", nil)

	got := redactedRequestURI(req)
	if got != "/api/exports/download/[redacted]?foo=bar" {
		t.Fatalf("redactedRequestURI() = %q", got)
	}
	if strings.Contains(got, "0123456789abcdef") {
		t.Fatalf("redacted URI leaked token: %q", got)
	}
}

func TestIsLongRunningRequestMatchesExecutionRoutes(t *testing.T) {
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/query", nil),
		httptest.NewRequest(http.MethodPost, "/api/query/", nil),
		httptest.NewRequest(http.MethodPost, "/api/tickets/42/execute", nil),
		httptest.NewRequest(http.MethodPost, "/api/tickets/TK-20260729-123456000-ABCDEF/execute", nil),
	} {
		if !isLongRunningRequest(req) {
			t.Fatalf("isLongRunningRequest(%s %s) = false, want true", req.Method, req.URL.Path)
		}
	}

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/query", nil),
		httptest.NewRequest(http.MethodPost, "/api/query/saved-queries", nil),
		httptest.NewRequest(http.MethodPost, "/api/query-access", nil),
		httptest.NewRequest(http.MethodPost, "/api/tickets/42/stop", nil),
		httptest.NewRequest(http.MethodGet, "/api/tickets/42/execute", nil),
	} {
		if isLongRunningRequest(req) {
			t.Fatalf("isLongRunningRequest(%s %s) = true, want false", req.Method, req.URL.Path)
		}
	}
}

func TestRegisterStaticFilesServesAsset(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(staticDir, "assets"), 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("index"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "assets", "app.js"), []byte("console.log('ok')"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	r := chi.NewRouter()
	registerStaticFiles(r, staticDir)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "console.log('ok')" {
		t.Fatalf("body = %q, want asset body", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q, want immutable asset cache", got)
	}
}

func TestRegisterStaticFilesFallsBackToIndexForSPARoutes(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("index"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	r := chi.NewRouter()
	registerStaticFiles(r, staticDir)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tickets/TK-20260622-ABC123", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "index" {
		t.Fatalf("body = %q, want index fallback", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache for SPA fallback", got)
	}
}

func TestRegisterStaticFilesServesRootIndexWithNoCache(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("index"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	r := chi.NewRouter()
	registerStaticFiles(r, staticDir)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache for index", got)
	}
}

func TestRegisterStaticFilesDoesNotFallbackForMissingAssets(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(staticDir, "assets"), 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("index"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	r := chi.NewRouter()
	registerStaticFiles(r, staticDir)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := rec.Body.String(); got == "index" {
		t.Fatal("missing asset should not receive SPA fallback")
	}
}

func TestRegisterStaticFilesDoesNotFallbackForAPIRoutes(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("index"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	r := chi.NewRouter()
	registerStaticFiles(r, staticDir)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/not-exist", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := rec.Body.String(); got == "index" {
		t.Fatal("api route should not receive SPA fallback")
	}
}
