package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/repository"
)

type FrontendReloadHandler struct {
	audit       *repository.AuditRepo
	rateLimiter requestRateLimiter
}

func NewFrontendReloadHandler(audit *repository.AuditRepo) *FrontendReloadHandler {
	return &FrontendReloadHandler{
		audit:       audit,
		rateLimiter: newRequestRateLimiter(30, time.Minute),
	}
}

var validFrontendReloadReasons = map[string]bool{
	"chunk-load-error":    true,
	"render-error":        true,
	"window-error":        true,
	"unhandled-rejection": true,
	"visibility-change":   true,
	"periodic-poll":       true,
}

// POST /frontend/reload-events — best-effort telemetry beacon the frontend
// fires right before it self-heals a stale-bundle error via reload. No auth
// required: the access token may already be stale/expired at exactly the
// moment this fires. Always responds 204 regardless of outcome since the
// frontend uses navigator.sendBeacon and never reads the response.
func (h *FrontendReloadHandler) ReportReload(w http.ResponseWriter, r *http.Request) {
	defer w.WriteHeader(http.StatusNoContent)

	if h.rateLimiter != nil && !h.rateLimiter.Allow(clientIP(r), time.Now()) {
		return
	}

	var req struct {
		Reason            string `json:"reason"`
		ErrorMessage      string `json:"error_message"`
		Route             string `json:"route"`
		PreviousSignature string `json:"previous_signature"`
		CurrentSignature  string `json:"current_signature"`
	}
	if err := bindJSON(r, &req); err != nil {
		return
	}

	reason := strings.TrimSpace(req.Reason)
	if !validFrontendReloadReasons[reason] {
		reason = "unknown"
	}

	if h.audit == nil {
		return
	}
	_ = h.audit.Log(r.Context(), repository.AuditEntry{
		ActionType:   "frontend_stale_bundle_reload",
		ResourceType: "frontend",
		IPAddress:    clientIP(r),
		Details: map[string]any{
			"reason":             reason,
			"error_message":      truncate(strings.TrimSpace(req.ErrorMessage), 500),
			"route":              truncate(strings.TrimSpace(req.Route), 300),
			"previous_signature": truncate(strings.TrimSpace(req.PreviousSignature), 500),
			"current_signature":  truncate(strings.TrimSpace(req.CurrentSignature), 500),
			"user_agent":         truncate(r.Header.Get("User-Agent"), 300),
		},
	})
}
