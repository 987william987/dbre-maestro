package handler

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"time"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/pool"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/dbre-maestro/maestro/internal/sqlreview"
	"github.com/go-chi/chi/v5"
)

type ExportHandler struct {
	exports *repository.ExportRepo
	dbConns *repository.DBConnectionRepo
	audit   *repository.AuditRepo
}

func NewExportHandler(exports *repository.ExportRepo, dbConns *repository.DBConnectionRepo, audit *repository.AuditRepo) *ExportHandler {
	return &ExportHandler{exports: exports, dbConns: dbConns, audit: audit}
}

// POST /exports — create export request (non-sensitive: immediate token + ready status)
func (h *ExportHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())

	var req struct {
		SQLContent     string `json:"sql_content"`
		DBConnectionID uint64 `json:"db_connection_id"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.SQLContent == "" || req.DBConnectionID == 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "sql_content and db_connection_id are required")
		return
	}

	// Enforce SQL Editor read-only whitelist for exports too
	if err := sqlreview.CheckReadOnly(req.SQLContent); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	exportReq := &model.ExportRequest{
		RequesterID:    userID,
		SQLContent:     req.SQLContent,
		DBConnectionID: req.DBConnectionID,
	}

	token, err := h.exports.Create(r.Context(), exportReq)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create export failed")
		return
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "export_create",
		ResourceType: "export",
		IPAddress:    clientIP(r),
	})

	jsonCreated(w, map[string]any{
		"download_url": fmt.Sprintf("/exports/download/%s", token),
		"expires_at":   time.Now().Add(24 * time.Hour),
	})
}

// GET /exports/download/{token} — validate token, execute SQL, stream CSV
// TE6: token is the only auth; expired → 403; token-not-found → 403 (no enumeration)
func (h *ExportHandler) Download(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if len(token) != 64 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	req, err := h.exports.GetByToken(r.Context(), token)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Return 403 for both "not found" and "expired" — avoid token enumeration
	if req == nil || time.Now().After(req.ExpiresAt) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	conn, err := h.dbConns.GetByID(r.Context(), req.DBConnectionID)
	if err != nil || conn == nil {
		http.Error(w, "connection not found", http.StatusInternalServerError)
		return
	}

	password, err := h.dbConns.DecryptPassword(conn)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	driver, dsn := pool.BuildDSN(conn, password)
	pools, err := pool.Global().GetOrCreate(conn.ID, driver, dsn)
	if err != nil {
		http.Error(w, "pool error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	rows, err := pools.QueryPool.QueryContext(ctx, req.SQLContent)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		http.Error(w, "columns error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="export-%s.csv"`, token[:8]))

	cw := csv.NewWriter(w)
	cw.Write(cols)

	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			break
		}
		record := make([]string, len(cols))
		for i, v := range vals {
			if v == nil {
				record[i] = ""
			} else {
				record[i] = fmt.Sprintf("%v", v)
			}
		}
		cw.Write(record)
	}
	cw.Flush()

	// Mark downloaded_at (best-effort, don't fail the response)
	h.exports.MarkDownloaded(r.Context(), token)

	userID := middleware.UserIDFromCtx(r.Context())
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      func() *uint64 { if userID != 0 { return &userID }; return nil }(),
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "export_download",
		ResourceType: "export",
		IPAddress:    clientIP(r),
	})
}
