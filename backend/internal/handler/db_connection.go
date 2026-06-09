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

	password, err := h.repo.DecryptPassword(conn)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "decrypt error")
		return
	}

	dbName := ""
	if conn.DatabaseName != nil {
		dbName = *conn.DatabaseName
	}
	dsn := pool.BuildMySQLDSN(conn.Host, conn.Port, conn.Username, password, dbName)

	pools, err := pool.Global().GetOrCreate(conn.ID, dsn)
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

// DELETE /db-connections/{id}
func (h *DBConnectionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	pool.Global().Invalidate(id)

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
