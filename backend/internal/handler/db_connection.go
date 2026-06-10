package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/pool"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
)

type DBConnectionHandler struct {
	repo  *repository.DBConnectionRepo
	audit *repository.AuditRepo
}

func NewDBConnectionHandler(repo *repository.DBConnectionRepo, audit *repository.AuditRepo) *DBConnectionHandler {
	return &DBConnectionHandler{repo: repo, audit: audit}
}

// GET /db-connections
func (h *DBConnectionHandler) List(w http.ResponseWriter, r *http.Request) {
	conns, err := h.repo.List(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list failed")
		return
	}
	if conns == nil {
		conns = []model.DBConnection{}
	}
	jsonOK(w, map[string]any{"connections": conns})
}

// POST /db-connections
func (h *DBConnectionHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string  `json:"name"`
		DBType       string  `json:"db_type"`
		Host         string  `json:"host"`
		Port         uint16  `json:"port"`
		DatabaseName *string `json:"database_name"`
		Username     string  `json:"username"`
		Password     string  `json:"password"`
		SSLMode      string  `json:"ssl_mode"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.Host == "" || req.Username == "" || req.Password == "" || req.Port == 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "name, host, port, username, password are required")
		return
	}
	if req.DBType == "" {
		req.DBType = "mysql"
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
		DatabaseName: req.DatabaseName,
		Username:     req.Username,
		SSLMode:      req.SSLMode,
		CreatedBy:    userID,
	}

	created, err := h.repo.Create(r.Context(), c, req.Password)
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
		Details:      map[string]string{"action": "create", "name": created.Name},
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

	if conn.DBType == "redis" {
		redisPassword, err := h.repo.DecryptPassword(conn)
		if err != nil {
			jsonOK(w, map[string]any{"ok": false, "error": "decrypt error"})
			return
		}
		addr := pool.BuildRedisAddr(conn.Host, conn.Port)
		client := pool.RedisGlobal().GetOrCreate(conn.ID, addr, redisPassword, 0)
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := client.Ping(ctx).Err(); err != nil {
			jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		jsonOK(w, map[string]any{"ok": true})
		return
	}

	password, err := h.repo.DecryptPassword(conn)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "decrypt error")
		return
	}

	driver, dsn := pool.BuildDSN(conn, password)
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
		Name         *string `json:"name"`
		DBType       *string `json:"db_type"`
		Host         *string `json:"host"`
		Port         *uint16 `json:"port"`
		DatabaseName *string `json:"database_name"`
		Username     *string `json:"username"`
		Password     *string `json:"password"`
		SSLMode      *string `json:"ssl_mode"`
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
		databaseName = req.DatabaseName
	}
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

	pool.Global().Invalidate(id)
	pool.RedisGlobal().Invalidate(id)

	userID := middleware.UserIDFromCtx(r.Context())
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "setting_change",
		ResourceType: "db_connection",
		ResourceID:   &id,
		Details:      map[string]string{"action": "update", "name": name},
	})

	updated, _ := h.repo.GetByID(r.Context(), id)
	jsonOK(w, updated)
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
