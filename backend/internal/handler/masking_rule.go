package handler

import (
	"net/http"
	"strconv"

	"github.com/dbre-maestro/maestro/internal/masking"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
)

type MaskingRuleHandler struct {
	rules *repository.MaskingRuleRepo
	audit *repository.AuditRepo
	cache *masking.RuleCache
}

func NewMaskingRuleHandler(rules *repository.MaskingRuleRepo, audit *repository.AuditRepo, cache *masking.RuleCache) *MaskingRuleHandler {
	return &MaskingRuleHandler{rules: rules, audit: audit, cache: cache}
}

// GET /masking-rules
func (h *MaskingRuleHandler) List(w http.ResponseWriter, r *http.Request) {
	rules, err := h.rules.List(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list masking rules failed")
		return
	}
	if rules == nil {
		rules = []model.MaskingRule{}
	}
	jsonOK(w, map[string]any{"rules": rules})
}

// POST /masking-rules
func (h *MaskingRuleHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DBConnectionID *uint64 `json:"db_connection_id"`
		TableName      string  `json:"table_name"`
		ColumnName     string  `json:"column_name"`
		MaskMode       string  `json:"mask_mode"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TableName == "" || req.ColumnName == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "table_name and column_name are required")
		return
	}
	switch masking.MaskMode(req.MaskMode) {
	case masking.MaskModeFull, masking.MaskModePartial, masking.MaskModeHash:
	case "":
		req.MaskMode = string(masking.MaskModeFull)
	default:
		jsonErr(w, http.StatusUnprocessableEntity, "mask_mode must be full, partial, or hash")
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	rule := &model.MaskingRule{
		DBConnectionID: req.DBConnectionID,
		TableName:      req.TableName,
		ColumnName:     req.ColumnName,
		MaskMode:       req.MaskMode,
		CreatedBy:      userID,
	}

	created, err := h.rules.Create(r.Context(), rule)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create masking rule failed")
		return
	}

	h.cache.Invalidate()
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "setting_change",
		ResourceType: "masking_rule",
		ResourceID:   &created.ID,
		Details:      map[string]string{"table": created.TableName, "column": created.ColumnName, "mode": created.MaskMode},
	})

	jsonCreated(w, created)
}

// DELETE /masking-rules/{id}
func (h *MaskingRuleHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	existing, err := h.rules.GetByID(r.Context(), id)
	if err != nil || existing == nil {
		jsonErr(w, http.StatusNotFound, "masking rule not found")
		return
	}

	if err := h.rules.Delete(r.Context(), id); err != nil {
		jsonErr(w, http.StatusInternalServerError, "delete masking rule failed")
		return
	}

	h.cache.Invalidate()
	userID := middleware.UserIDFromCtx(r.Context())
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "setting_change",
		ResourceType: "masking_rule",
		ResourceID:   &id,
		Details:      map[string]string{"action": "delete"},
	})

	w.WriteHeader(http.StatusNoContent)
}
