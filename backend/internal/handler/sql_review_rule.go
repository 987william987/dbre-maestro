package handler

import (
	"fmt"
	"net/http"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
)

type SQLReviewRuleHandler struct {
	rules *repository.SQLReviewRuleRepo
	audit *repository.AuditRepo
}

func NewSQLReviewRuleHandler(rules *repository.SQLReviewRuleRepo, audit *repository.AuditRepo) *SQLReviewRuleHandler {
	return &SQLReviewRuleHandler{rules: rules, audit: audit}
}

// GET /sql-review-rules — DBA+
func (h *SQLReviewRuleHandler) List(w http.ResponseWriter, r *http.Request) {
	rules, err := h.rules.List(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list sql review rules failed")
		return
	}
	if rules == nil {
		rules = []model.SQLReviewRule{}
	}
	jsonOK(w, map[string]any{"rules": rules})
}

// PATCH /sql-review-rules/{name} — DBA+
// Body: { "enabled": true, "threshold": 5000 }
// Both fields are optional; omit to leave unchanged.
func (h *SQLReviewRuleHandler) Patch(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	existing, err := h.rules.GetByName(r.Context(), name)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if existing == nil {
		jsonErr(w, http.StatusNotFound, "rule not found")
		return
	}

	var req struct {
		Enabled   *bool   `json:"enabled"`
		Threshold *int64  `json:"threshold"`
		Severity  *string `json:"severity"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Enabled == nil && req.Threshold == nil && req.Severity == nil {
		jsonErr(w, http.StatusUnprocessableEntity, "at least one of enabled, threshold, or severity must be provided")
		return
	}
	if req.Threshold != nil && !sqlReviewRuleSupportsThreshold(name) {
		jsonErr(w, http.StatusUnprocessableEntity, fmt.Sprintf("rule %q does not support threshold", name))
		return
	}
	if req.Threshold != nil && *req.Threshold < 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "threshold must be non-negative")
		return
	}
	if req.Severity != nil && *req.Severity != "error" && *req.Severity != "warning" {
		jsonErr(w, http.StatusUnprocessableEntity, "severity must be error or warning")
		return
	}
	if req.Severity != nil && *req.Severity == "warning" && sqlReviewRuleRequiresError(name) {
		jsonErr(w, http.StatusUnprocessableEntity, "this rule must use error severity because the SQL parser cannot process the prohibited statement")
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	if err := h.rules.Patch(r.Context(), name, req.Enabled, req.Threshold, req.Severity, userID); err != nil {
		jsonErr(w, http.StatusInternalServerError, "update rule failed")
		return
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:    &userID,
		ActorName:  middleware.UsernameFromCtx(r.Context()),
		ActionType: "setting_change",
		Details: map[string]any{
			"rule":      name,
			"enabled":   req.Enabled,
			"threshold": req.Threshold,
			"severity":  req.Severity,
		},
	})

	updated, _ := h.rules.GetByName(r.Context(), name)
	jsonOK(w, updated)
}

func sqlReviewRuleRequiresError(name string) bool {
	return name == "prohibit_trigger" || name == "prohibit_stored_function" || name == "prohibit_event"
}

func sqlReviewRuleSupportsThreshold(name string) bool {
	return name == "high_row_count"
}
