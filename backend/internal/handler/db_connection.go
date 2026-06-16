package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/pool"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
)

type DBConnectionHandler struct {
	repo  *repository.DBConnectionRepo
	users *repository.UserRepo
	audit *repository.AuditRepo
}

type connectionCredentialPayload struct {
	CredentialRole string `json:"credential_role"`
	Username       string `json:"username"`
	Password       string `json:"password"`
}

func NewDBConnectionHandler(repo *repository.DBConnectionRepo, users *repository.UserRepo, audit *repository.AuditRepo) *DBConnectionHandler {
	return &DBConnectionHandler{repo: repo, users: users, audit: audit}
}

// GET /db-connections
// Users with write permission see all connections; readers are filtered by their effective DB scope.
func (h *DBConnectionHandler) List(w http.ResponseWriter, r *http.Request) {
	conns, err := h.repo.List(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list failed")
		return
	}
	if conns == nil {
		conns = []model.DBConnection{}
	}

	if !middleware.HasPermission(r.Context(), "db_connections.write") {
		userID := middleware.UserIDFromCtx(r.Context())
		accessibleIDs, err := h.users.GetEffectiveDBConnectionIDs(r.Context(), userID)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "db scope check failed")
			return
		}
		accessible := make(map[uint64]bool, len(accessibleIDs))
		for _, id := range accessibleIDs {
			accessible[id] = true
		}
		filtered := conns[:0]
		for _, c := range conns {
			if accessible[c.ID] {
				filtered = append(filtered, c)
			}
		}
		conns = filtered
		if conns == nil {
			conns = []model.DBConnection{}
		}
	}

	jsonOK(w, map[string]any{"connections": conns})
}

// POST /db-connections
func (h *DBConnectionHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string                        `json:"name"`
		DBType       string                        `json:"db_type"`
		Host         string                        `json:"host"`
		Port         uint16                        `json:"port"`
		DatabaseName *string                       `json:"database_name"`
		Username     string                        `json:"username"`
		Password     string                        `json:"password"`
		SSLMode      string                        `json:"ssl_mode"`
		Credentials  []connectionCredentialPayload `json:"credentials"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DBType == "" {
		req.DBType = "mysql"
	}
	if req.Name == "" || req.Host == "" || req.Port == 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "name, host, and port are required")
		return
	}
	if req.Password == "" && len(req.Credentials) == 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "name, host, port, password are required")
		return
	}
	credentials := normalizeCredentialPayloads(req.Credentials)
	if req.DBType != "redis" && req.Username == "" && !hasCredentialRole(credentials, model.DBCredentialRoleReadonly) {
		jsonErr(w, http.StatusUnprocessableEntity, "readonly username is required for mysql/postgres connections")
		return
	}
	if req.SSLMode == "" {
		req.SSLMode = "prefer"
	}

	userID := middleware.UserIDFromCtx(r.Context())
	c := &model.DBConnection{
		Name:         req.Name,
		DBType:       req.DBType,
		Host:         req.Host,
		Port:         req.Port,
		DatabaseName: normalizeDatabaseName(req.DBType, req.DatabaseName),
		Username:     req.Username,
		SSLMode:      req.SSLMode,
		CreatedBy:    userID,
	}

	created, err := h.repo.Create(r.Context(), c, req.Password, credentials)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create failed")
		return
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "setting_change",
		ResourceType: "db_connection",
		ResourceID:   &created.ID,
		Details: map[string]any{
			"action":           "create",
			"name":             created.Name,
			"credential_roles": extractCredentialRoles(credentials),
		},
	})

	jsonCreated(w, created)
}

// POST /db-connections/{id}/test — verify connectivity using query_pool
func (h *DBConnectionHandler) Test(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	conn, err := h.repo.GetByID(r.Context(), id)
	if err != nil || conn == nil {
		jsonErr(w, http.StatusNotFound, "connection not found")
		return
	}

	role := strings.TrimSpace(r.URL.Query().Get("credential_role"))
	if role == "" {
		if conn.DBType == "redis" {
			role = model.DBCredentialRoleReadonly
		} else {
			role = model.DBCredentialRoleReadonly
		}
	}

	resolvedConn, password, err := h.repo.ResolveCredential(conn, role)
	if err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	if conn.DBType == "redis" {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := pool.RedisGlobal().Ping(ctx, pool.RedisConnOptions{
			ConnID:   resolvedConn.ID,
			Host:     resolvedConn.Host,
			Port:     resolvedConn.Port,
			Username: resolvedConn.Username,
			Password: password,
			DB:       0,
			SSLMode:  resolvedConn.SSLMode,
		}); err != nil {
			jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		jsonOK(w, map[string]any{"ok": true})
		return
	}

	driver, dsn := pool.BuildDSN(resolvedConn, password)
	pools, err := pool.Global().GetOrCreate(conn.ID, driver, dsn)
	if err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := pools.QueryPool.PingContext(ctx); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	jsonOK(w, map[string]any{"ok": true})
}

// PATCH /db-connections/{id}
// All fields are optional; omit to leave unchanged.
// Password is only updated when provided and non-empty.
func (h *DBConnectionHandler) Patch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	existing, err := h.repo.GetByID(r.Context(), id)
	if err != nil || existing == nil {
		jsonErr(w, http.StatusNotFound, "connection not found")
		return
	}

	var req struct {
		Name         *string                        `json:"name"`
		DBType       *string                        `json:"db_type"`
		Host         *string                        `json:"host"`
		Port         *uint16                        `json:"port"`
		DatabaseName json.RawMessage                `json:"database_name"`
		Username     *string                        `json:"username"`
		Password     *string                        `json:"password"`
		SSLMode      *string                        `json:"ssl_mode"`
		Credentials  *[]connectionCredentialPayload `json:"credentials"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	name := existing.Name
	if req.Name != nil {
		name = *req.Name
	}
	dbType := existing.DBType
	if req.DBType != nil {
		dbType = *req.DBType
	}
	host := existing.Host
	if req.Host != nil {
		host = *req.Host
	}
	port := existing.Port
	if req.Port != nil {
		port = *req.Port
	}
	databaseName := existing.DatabaseName
	if req.DatabaseName != nil {
		if string(req.DatabaseName) == "null" {
			databaseName = nil
		} else {
			var nextDatabaseName string
			if err := json.Unmarshal(req.DatabaseName, &nextDatabaseName); err != nil {
				jsonErr(w, http.StatusBadRequest, "invalid request body")
				return
			}
			databaseName = &nextDatabaseName
		}
	}
	databaseName = normalizeDatabaseName(dbType, databaseName)
	username := existing.Username
	if req.Username != nil {
		username = *req.Username
	}
	sslMode := existing.SSLMode
	if req.SSLMode != nil {
		sslMode = *req.SSLMode
	}

	if err := h.repo.Update(r.Context(), id, name, dbType, host, port, databaseName, username, sslMode); err != nil {
		jsonErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	if req.Password != nil && *req.Password != "" {
		if err := h.repo.UpdatePassword(r.Context(), id, *req.Password); err != nil {
			jsonErr(w, http.StatusInternalServerError, "update password failed")
			return
		}
	}
	credentials := normalizeCredentialPayloads(existingCredentials(existing, req.Credentials))
	if req.Credentials != nil {
		if err := h.repo.ReplaceCredentials(r.Context(), id, credentials); err != nil {
			jsonErr(w, http.StatusInternalServerError, "update credentials failed")
			return
		}
	}

	pool.Global().Invalidate(id)
	pool.RedisGlobal().Invalidate(id)

	userID := middleware.UserIDFromCtx(r.Context())
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "setting_change",
		ResourceType: "db_connection",
		ResourceID:   &id,
		Details: map[string]any{
			"action":           "update",
			"name":             name,
			"credential_roles": extractCredentialRoles(credentials),
		},
	})

	updated, _ := h.repo.GetByID(r.Context(), id)
	jsonOK(w, updated)
}

func normalizeDatabaseName(dbType string, databaseName *string) *string {
	if dbType != "postgres" && dbType != "postgresql" {
		return databaseName
	}
	if databaseName == nil || strings.TrimSpace(*databaseName) == "" {
		defaultDatabase := "postgres"
		return &defaultDatabase
	}
	trimmed := strings.TrimSpace(*databaseName)
	return &trimmed
}

func normalizeCredentialPayloads(payloads []connectionCredentialPayload) []model.DBConnectionCredentialInput {
	items := make([]model.DBConnectionCredentialInput, 0, len(payloads))
	for _, payload := range payloads {
		role := strings.TrimSpace(payload.CredentialRole)
		username := strings.TrimSpace(payload.Username)
		if role == "" {
			continue
		}
		items = append(items, model.DBConnectionCredentialInput{
			CredentialRole: role,
			Username:       username,
			Password:       payload.Password,
		})
	}
	return items
}

func hasCredentialRole(credentials []model.DBConnectionCredentialInput, role string) bool {
	for _, credential := range credentials {
		if credential.CredentialRole == role && credential.Username != "" {
			return true
		}
	}
	return false
}

func extractCredentialRoles(credentials []model.DBConnectionCredentialInput) []string {
	roles := make([]string, 0, len(credentials))
	for _, credential := range credentials {
		if credential.CredentialRole == "" {
			continue
		}
		roles = append(roles, credential.CredentialRole)
	}
	return roles
}

func existingCredentials(existing *model.DBConnection, patch *[]connectionCredentialPayload) []connectionCredentialPayload {
	if patch != nil {
		return *patch
	}
	items := make([]connectionCredentialPayload, 0, len(existing.Credentials))
	for _, credential := range existing.Credentials {
		items = append(items, connectionCredentialPayload{
			CredentialRole: credential.CredentialRole,
			Username:       credential.Username,
		})
	}
	return items
}

// DELETE /db-connections/{id}
func (h *DBConnectionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	pool.Global().Invalidate(id)
	pool.RedisGlobal().Invalidate(id)

	if err := h.repo.Delete(r.Context(), id); err != nil {
		jsonErr(w, http.StatusInternalServerError, "delete failed")
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "setting_change",
		ResourceType: "db_connection",
		ResourceID:   &id,
		Details:      map[string]string{"action": "delete"},
	})

	w.WriteHeader(http.StatusNoContent)
}
