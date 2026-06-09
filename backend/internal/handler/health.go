package handler

import (
	"net/http"

	"github.com/jmoiron/sqlx"
)

const Version = "0.1.0"

type HealthHandler struct {
	db *sqlx.DB
}

func NewHealthHandler(db *sqlx.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

// T8: GET /health — checks meta DB connection, returns 503 if unhealthy
func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	dbStatus := "ok"
	if err := h.db.PingContext(r.Context()); err != nil {
		dbStatus = "error"
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	jsonOK(w, map[string]string{
		"status":   dbStatus,
		"version":  Version,
		"database": dbStatus,
	})
}
