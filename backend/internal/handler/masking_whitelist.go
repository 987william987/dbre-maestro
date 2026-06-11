package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
)

type MaskingWhitelistHandler struct {
	dbConns   *repository.DBConnectionRepo
	whitelist *repository.MaskingWhitelistRepo
	audit     *repository.AuditRepo
}

func NewMaskingWhitelistHandler(dbConns *repository.DBConnectionRepo, whitelist *repository.MaskingWhitelistRepo, audit *repository.AuditRepo) *MaskingWhitelistHandler {
	return &MaskingWhitelistHandler{dbConns: dbConns, whitelist: whitelist, audit: audit}
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

func (h *MaskingWhitelistHandler) Create(w http.ResponseWriter, r *http.Request) {
	req, ok := parseMaskingWhitelistPayload(w, r)
	if !ok {
		return
	}
	if !h.validateMySQLConnection(w, r, req.DBConnectionID) {
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	entry := &model.MaskingWhitelist{
		DBConnectionID: req.DBConnectionID,
		DatabaseName:   req.DatabaseName,
		TableName:      req.TableName,
		ColumnName:     req.ColumnName,
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
		Details:      map[string]string{"database": created.DatabaseName, "table": created.TableName, "column": created.ColumnName},
	})

	jsonCreated(w, created)
}

// PATCH /masking-whitelist/{id}
func (h *MaskingWhitelistHandler) Patch(w http.ResponseWriter, r *http.Request) {
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

	req, ok := parseMaskingWhitelistPayload(w, r)
	if !ok {
		return
	}
	if !h.validateMySQLConnection(w, r, req.DBConnectionID) {
		return
	}

	existing.DBConnectionID = req.DBConnectionID
	existing.DatabaseName = req.DatabaseName
	existing.TableName = req.TableName
	existing.ColumnName = req.ColumnName

	updated, err := h.whitelist.Patch(r.Context(), existing)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "patch failed")
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "setting_change",
		ResourceType: "masking_whitelist",
		ResourceID:   &id,
		Details:      map[string]string{"database": updated.DatabaseName, "table": updated.TableName, "column": updated.ColumnName, "action": "update"},
	})

	jsonOK(w, updated)
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

func parseMaskingWhitelistPayload(w http.ResponseWriter, r *http.Request) (*struct {
	DBConnectionID uint64 `json:"db_connection_id"`
	DatabaseName   string `json:"database_name"`
	TableName      string `json:"table_name"`
	ColumnName     string `json:"column_name"`
}, bool) {
	var req struct {
		DBConnectionID uint64 `json:"db_connection_id"`
		DatabaseName   string `json:"database_name"`
		TableName      string `json:"table_name"`
		ColumnName     string `json:"column_name"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return nil, false
	}
	req.DatabaseName = strings.TrimSpace(req.DatabaseName)
	req.TableName = strings.TrimSpace(req.TableName)
	req.ColumnName = strings.TrimSpace(req.ColumnName)
	if req.DBConnectionID == 0 || req.DatabaseName == "" || req.TableName == "" || req.ColumnName == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "db_connection_id, database_name, table_name, and column_name are required")
		return nil, false
	}
	return &req, true
}

func (h *MaskingWhitelistHandler) validateMySQLConnection(w http.ResponseWriter, r *http.Request, connID uint64) bool {
	conn, err := h.dbConns.GetByID(r.Context(), connID)
	if err != nil || conn == nil {
		jsonErr(w, http.StatusNotFound, "db connection not found")
		return false
	}
	if conn.DBType != "mysql" {
		jsonErr(w, http.StatusUnprocessableEntity, "only mysql connections support masking whitelist")
		return false
	}
	return true
}
