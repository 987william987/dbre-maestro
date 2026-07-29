package handler

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type FrontendReloadHandler struct {
	rateLimiter requestRateLimiter
}

func NewFrontendReloadHandler() *FrontendReloadHandler {
	return &FrontendReloadHandler{
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

	slog.Info("frontend stale bundle reload",
		"reason", reason,
		"error_message", truncate(strings.TrimSpace(req.ErrorMessage), 500),
		"route", truncate(strings.TrimSpace(req.Route), 300),
		"previous_signature", truncate(strings.TrimSpace(req.PreviousSignature), 500),
		"current_signature", truncate(strings.TrimSpace(req.CurrentSignature), 500),
		"user_agent", truncate(r.Header.Get("User-Agent"), 300),
		"ip", clientIP(r),
	)
}
