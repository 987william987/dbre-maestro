package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dbre-maestro/maestro/internal/middleware"
)

func TestRequireTicketsWorkspaceReadAllowsTicketsApply(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tickets", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.CtxPermissions, []string{"tickets.apply"}))

	called := false
	handler := requireTicketsWorkspaceRead(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("handler should be called for tickets.apply")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequireTicketsWorkspaceReadAllowsSensitiveApply(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tickets", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.CtxPermissions, []string{"sql_editor.sensitive_apply"}))

	called := false
	handler := requireTicketsWorkspaceRead(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("handler should be called for sql_editor.sensitive_apply")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
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
