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

type RedisSensitiveKeyPrefixHandler struct {
	prefixes *repository.RedisSensitiveKeyPrefixRepo
	dbConns  *repository.DBConnectionRepo
	audit    *repository.AuditRepo
}

func NewRedisSensitiveKeyPrefixHandler(prefixes *repository.RedisSensitiveKeyPrefixRepo, dbConns *repository.DBConnectionRepo, audit *repository.AuditRepo) *RedisSensitiveKeyPrefixHandler {
	return &RedisSensitiveKeyPrefixHandler{prefixes: prefixes, dbConns: dbConns, audit: audit}
}

func (h *RedisSensitiveKeyPrefixHandler) List(w http.ResponseWriter, r *http.Request) {
	prefixes, err := h.prefixes.List(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list redis sensitive key prefixes failed")
		return
	}
	if prefixes == nil {
		prefixes = []model.RedisSensitiveKeyPrefix{}
	}
	jsonOK(w, map[string]any{"prefixes": prefixes})
}

func (h *RedisSensitiveKeyPrefixHandler) Create(w http.ResponseWriter, r *http.Request) {
	req, ok := h.parsePayload(w, r)
	if !ok {
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	prefix := &model.RedisSensitiveKeyPrefix{
		DBConnectionID: req.DBConnectionID,
		RedisDBIndex:   req.RedisDBIndex,
		KeyPrefix:      req.KeyPrefix,
		Reason:         req.Reason,
		IsActive:       req.IsActive,
		CreatedBy:      &userID,
	}
	created, err := h.prefixes.Create(r.Context(), prefix)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create redis sensitive key prefix failed")
		return
	}

	h.auditChange(r, &userID, "create", created)
	jsonCreated(w, created)
}

func (h *RedisSensitiveKeyPrefixHandler) Patch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	existing, err := h.prefixes.GetByID(r.Context(), id)
	if err != nil || existing == nil {
		jsonErr(w, http.StatusNotFound, "redis sensitive key prefix not found")
		return
	}

	req, ok := h.parsePayload(w, r)
	if !ok {
		return
	}
	existing.DBConnectionID = req.DBConnectionID
	existing.RedisDBIndex = req.RedisDBIndex
	existing.KeyPrefix = req.KeyPrefix
	existing.Reason = req.Reason
	existing.IsActive = req.IsActive

	updated, err := h.prefixes.Patch(r.Context(), existing)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "patch redis sensitive key prefix failed")
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	h.auditChange(r, &userID, "update", updated)
	jsonOK(w, updated)
}

func (h *RedisSensitiveKeyPrefixHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	existing, err := h.prefixes.GetByID(r.Context(), id)
	if err != nil || existing == nil {
		jsonErr(w, http.StatusNotFound, "redis sensitive key prefix not found")
		return
	}
	if err := h.prefixes.Delete(r.Context(), id); err != nil {
		jsonErr(w, http.StatusInternalServerError, "delete redis sensitive key prefix failed")
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	h.auditChange(r, &userID, "delete", existing)
	w.WriteHeader(http.StatusNoContent)
}

func (h *RedisSensitiveKeyPrefixHandler) parsePayload(w http.ResponseWriter, r *http.Request) (*struct {
	DBConnectionID uint64  `json:"db_connection_id"`
	RedisDBIndex   *int    `json:"redis_db_index"`
	KeyPrefix      string  `json:"key_prefix"`
	Reason         *string `json:"reason"`
	IsActive       bool    `json:"is_active"`
}, bool) {
	var req struct {
		DBConnectionID uint64  `json:"db_connection_id"`
		RedisDBIndex   *int    `json:"redis_db_index"`
		KeyPrefix      string  `json:"key_prefix"`
		Reason         *string `json:"reason"`
		IsActive       *bool   `json:"is_active"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return nil, false
	}
	req.KeyPrefix = strings.TrimSpace(req.KeyPrefix)
	if req.Reason != nil {
		trimmed := strings.TrimSpace(*req.Reason)
		if trimmed == "" {
			req.Reason = nil
		} else {
			req.Reason = &trimmed
		}
	}
	if req.DBConnectionID == 0 || req.KeyPrefix == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "db_connection_id and key_prefix are required")
		return nil, false
	}
	if req.RedisDBIndex != nil && (*req.RedisDBIndex < 0 || *req.RedisDBIndex > 15) {
		jsonErr(w, http.StatusUnprocessableEntity, "redis_db_index must be between 0 and 15")
		return nil, false
	}
	if !h.validateRedisConnection(w, r, req.DBConnectionID) {
		return nil, false
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	return &struct {
		DBConnectionID uint64  `json:"db_connection_id"`
		RedisDBIndex   *int    `json:"redis_db_index"`
		KeyPrefix      string  `json:"key_prefix"`
		Reason         *string `json:"reason"`
		IsActive       bool    `json:"is_active"`
	}{
		DBConnectionID: req.DBConnectionID,
		RedisDBIndex:   req.RedisDBIndex,
		KeyPrefix:      req.KeyPrefix,
		Reason:         req.Reason,
		IsActive:       isActive,
	}, true
}

func (h *RedisSensitiveKeyPrefixHandler) validateRedisConnection(w http.ResponseWriter, r *http.Request, connID uint64) bool {
	conn, err := h.dbConns.GetByID(r.Context(), connID)
	if err != nil || conn == nil {
		jsonErr(w, http.StatusNotFound, "db connection not found")
		return false
	}
	if conn.DBType != "redis" {
		jsonErr(w, http.StatusUnprocessableEntity, "only redis connections support sensitive key prefixes")
		return false
	}
	return true
}

func (h *RedisSensitiveKeyPrefixHandler) auditChange(r *http.Request, userID *uint64, action string, prefix *model.RedisSensitiveKeyPrefix) {
	if h.audit == nil || prefix == nil {
		return
	}
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "setting_change",
		ResourceType: "redis_sensitive_key_prefix",
		ResourceID:   &prefix.ID,
		Details: map[string]any{
			"action":           action,
			"db_connection_id": prefix.DBConnectionID,
			"redis_db_index":   prefix.RedisDBIndex,
			"key_prefix":       prefix.KeyPrefix,
			"is_active":        prefix.IsActive,
		},
		IPAddress: clientIP(r),
	})
}
