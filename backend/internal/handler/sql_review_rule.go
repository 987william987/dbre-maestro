package handler

import (
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
		Enabled   *bool  `json:"enabled"`
		Threshold *int64 `json:"threshold"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Enabled == nil && req.Threshold == nil {
		jsonErr(w, http.StatusUnprocessableEntity, "at least one of enabled or threshold must be provided")
		return
	}
	if req.Threshold != nil && *req.Threshold < 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "threshold must be non-negative")
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	if err := h.rules.Patch(r.Context(), name, req.Enabled, req.Threshold, userID); err != nil {
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
		},
	})

	updated, _ := h.rules.GetByName(r.Context(), name)
	jsonOK(w, updated)
}
