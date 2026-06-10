package handler

import (
	"net/http"
	"strconv"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
)

type MaskingWhitelistHandler struct {
	whitelist  *repository.MaskingWhitelistRepo
	authGroups *repository.AuthGroupRepo
	audit      *repository.AuditRepo
}

func NewMaskingWhitelistHandler(whitelist *repository.MaskingWhitelistRepo, authGroups *repository.AuthGroupRepo, audit *repository.AuditRepo) *MaskingWhitelistHandler {
	return &MaskingWhitelistHandler{whitelist: whitelist, authGroups: authGroups, audit: audit}
}

// GET /masking-whitelist
func (h *MaskingWhitelistHandler) List(w http.ResponseWriter, r *http.Request) {
	entries, err := h.whitelist.List(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list failed")
		return
	}
	if entries == nil {
		entries = []model.MaskingWhitelist{}
	}
	jsonOK(w, map[string]any{"whitelist": entries})
}

// POST /masking-whitelist
// Body: { "db_connection_id": 1, "table_name": "users", "column_name": "phone",
//
//	"user_id": 5 }   — or "auth_group": "dba"
func (h *MaskingWhitelistHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DBConnectionID *uint64 `json:"db_connection_id"`
		TableName      string  `json:"table_name"`
		ColumnName     string  `json:"column_name"`
		UserID         *uint64 `json:"user_id"`
		AuthGroup      *string `json:"auth_group"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TableName == "" || req.ColumnName == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "table_name and column_name are required")
		return
	}
	if req.UserID == nil && req.AuthGroup == nil {
		jsonErr(w, http.StatusUnprocessableEntity, "one of user_id or auth_group is required")
		return
	}
	var authGroupID *uint64
	if req.AuthGroup != nil {
		authGroup, err := h.authGroups.GetByKey(r.Context(), *req.AuthGroup)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "load auth group failed")
			return
		}
		if authGroup == nil {
			jsonErr(w, http.StatusUnprocessableEntity, "invalid auth_group")
			return
		}
		authGroupID = &authGroup.ID
	}

	userID := middleware.UserIDFromCtx(r.Context())
	entry := &model.MaskingWhitelist{
		DBConnectionID: req.DBConnectionID,
		TableName:      req.TableName,
		ColumnName:     req.ColumnName,
		UserID:         req.UserID,
		AuthGroupID:    authGroupID,
		AuthGroup:      req.AuthGroup,
		CreatedBy:      userID,
	}

	created, err := h.whitelist.Create(r.Context(), entry)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create failed")
		return
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "setting_change",
		ResourceType: "masking_whitelist",
		ResourceID:   &created.ID,
		Details:      map[string]string{"table": created.TableName, "column": created.ColumnName},
	})

	jsonCreated(w, created)
}

// DELETE /masking-whitelist/{id}
func (h *MaskingWhitelistHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	existing, err := h.whitelist.GetByID(r.Context(), id)
	if err != nil || existing == nil {
		jsonErr(w, http.StatusNotFound, "whitelist entry not found")
		return
	}

	if err := h.whitelist.Delete(r.Context(), id); err != nil {
		jsonErr(w, http.StatusInternalServerError, "delete failed")
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "setting_change",
		ResourceType: "masking_whitelist",
		ResourceID:   &id,
		Details:      map[string]string{"action": "delete"},
	})

	w.WriteHeader(http.StatusNoContent)
}
