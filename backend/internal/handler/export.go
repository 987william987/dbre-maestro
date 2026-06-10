package handler

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/notification"
	"github.com/dbre-maestro/maestro/internal/pool"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/dbre-maestro/maestro/internal/sqlreview"
	"github.com/go-chi/chi/v5"
)

type ExportHandler struct {
	exports      *repository.ExportRepo
	dbConns      *repository.DBConnectionRepo
	audit        *repository.AuditRepo
	maskingRules *repository.MaskingRuleRepo
	notifRepo    *repository.NotificationRepo
	lark         *notification.Client
}

func NewExportHandler(
	exports *repository.ExportRepo,
	dbConns *repository.DBConnectionRepo,
	audit *repository.AuditRepo,
	maskingRules *repository.MaskingRuleRepo,
	notifRepo *repository.NotificationRepo,
	lark *notification.Client,
) *ExportHandler {
	return &ExportHandler{
		exports:      exports,
		dbConns:      dbConns,
		audit:        audit,
		maskingRules: maskingRules,
		notifRepo:    notifRepo,
		lark:         lark,
	}
}

func (h *ExportHandler) notifyLark(ctx context.Context, title, body string) {
	if h.lark == nil {
		return
	}
	result := h.lark.Send(ctx, notification.Message{Title: title, Body: body})
	if result.Err != nil {
		h.audit.Log(ctx, repository.AuditEntry{
			ActionType: "notification_failure",
			Details:    map[string]any{"err": result.Err.Error()},
		})
	}
}

func (h *ExportHandler) sendInApp(ctx context.Context, userID uint64, notifType, title, body, resType string, resID uint64) {
	if h.notifRepo == nil {
		return
	}
	_ = h.notifRepo.Create(ctx, userID, notifType, title, body, &resType, &resID)
}

// POST /exports
// Non-sensitive (no masking rules on the connection): immediate token, status=ready.
// Sensitive (has masking rules): status=pending, requires DBA/reviewer approval.
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

	if err := sqlreview.CheckReadOnly(req.SQLContent); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	// Determine sensitivity: if any masking rules exist for the connection, require approval.
	rules, err := h.maskingRules.ListForConnection(r.Context(), req.DBConnectionID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "check masking rules failed")
		return
	}
	sensitive := len(rules) > 0

	status := model.ExportStatusReady
	if sensitive {
		status = model.ExportStatusPending
	}

	exportReq := &model.ExportRequest{
		RequesterID:    userID,
		SQLContent:     req.SQLContent,
		DBConnectionID: req.DBConnectionID,
	}

	id, token, err := h.exports.Create(r.Context(), exportReq, status)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create export failed")
		return
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "export_create",
		ResourceType: "export",
		ResourceID:   &id,
		Details:      map[string]string{"sensitive": fmt.Sprintf("%v", sensitive)},
		IPAddress:    clientIP(r),
	})

	resp := map[string]any{
		"id":        id,
		"status":    string(status),
		"sensitive": sensitive,
	}
	if !sensitive {
		resp["download_url"] = fmt.Sprintf("/exports/download/%s", token)
		resp["expires_at"] = time.Now().Add(24 * time.Hour)
	}
	jsonCreated(w, resp)
}

// GET /exports
// Sensitive export reviewers see all; others see only their own.
func (h *ExportHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())

	var requesterID *uint64
	if !middleware.HasPermission(r.Context(), "sql_editor.sensitive_review") {
		requesterID = &userID
	}

	exports, err := h.exports.List(r.Context(), requesterID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list exports failed")
		return
	}
	if exports == nil {
		exports = []model.ExportRequest{}
	}
	jsonOK(w, map[string]any{"exports": exports})
}

// POST /exports/{id}/approve — Reviewer/DBA/Admin
func (h *ExportHandler) Approve(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	req, err := h.exports.GetByID(r.Context(), id)
	if err != nil || req == nil {
		jsonErr(w, http.StatusNotFound, "export not found")
		return
	}
	if req.Status != model.ExportStatusPending {
		jsonErr(w, http.StatusConflict, "only pending exports can be approved")
		return
	}

	actorID := middleware.UserIDFromCtx(r.Context())
	if err := h.exports.UpdateStatus(r.Context(), id, model.ExportStatusReady, &actorID); err != nil {
		jsonErr(w, http.StatusInternalServerError, "approve failed")
		return
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "export_approve",
		ResourceType: "export",
		ResourceID:   &id,
		IPAddress:    clientIP(r),
	})

	approveBody := fmt.Sprintf("導出申請 #%d 已審批通過，請在有效期內下載", id)
	h.notifyLark(r.Context(), "導出申請已通過", approveBody)
	h.sendInApp(r.Context(), req.RequesterID, "export_approved", "導出申請已通過", approveBody, "export", id)

	jsonOK(w, map[string]any{
		"id":           id,
		"status":       string(model.ExportStatusReady),
		"download_url": fmt.Sprintf("/exports/download/%s", req.DownloadToken),
		"expires_at":   req.ExpiresAt,
	})
}

// POST /exports/{id}/reject — Reviewer/DBA/Admin
func (h *ExportHandler) Reject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	req, err := h.exports.GetByID(r.Context(), id)
	if err != nil || req == nil {
		jsonErr(w, http.StatusNotFound, "export not found")
		return
	}
	if req.Status != model.ExportStatusPending {
		jsonErr(w, http.StatusConflict, "only pending exports can be rejected")
		return
	}

	actorID := middleware.UserIDFromCtx(r.Context())
	if err := h.exports.UpdateStatus(r.Context(), id, model.ExportStatusRejected, &actorID); err != nil {
		jsonErr(w, http.StatusInternalServerError, "reject failed")
		return
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "export_reject",
		ResourceType: "export",
		ResourceID:   &id,
		IPAddress:    clientIP(r),
	})

	rejectBody := fmt.Sprintf("導出申請 #%d 已被拒絕", id)
	h.notifyLark(r.Context(), "導出申請已拒絕", rejectBody)
	h.sendInApp(r.Context(), req.RequesterID, "export_rejected", "導出申請已拒絕", rejectBody, "export", id)

	w.WriteHeader(http.StatusNoContent)
}

// GET /exports/download/{token} — validate token, execute SQL, stream CSV
// Token is the only auth; expired → 403; token-not-found → 403 (no enumeration).
// Status must be "ready" to allow download.
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
	// 403 for not-found, expired, or not-yet-approved — no token enumeration
	if req == nil || time.Now().After(req.ExpiresAt) || req.Status != model.ExportStatusReady {
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

	h.exports.MarkDownloaded(r.Context(), token)

	userID := middleware.UserIDFromCtx(r.Context())
	reqID := req.ID
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID: func() *uint64 {
			if userID != 0 {
				return &userID
			}
			return nil
		}(),
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "export_download",
		ResourceType: "export",
		ResourceID:   &reqID,
		IPAddress:    clientIP(r),
	})
}
