package handler

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/dbre-maestro/maestro/internal/masking"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/notification"
	"github.com/dbre-maestro/maestro/internal/pool"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/dbre-maestro/maestro/internal/sqlparse"
	"github.com/dbre-maestro/maestro/internal/sqlreview"
	"github.com/go-chi/chi/v5"
)

type ExportHandler struct {
	exports             *repository.ExportRepo
	tickets             *repository.TicketRepo
	dbConns             *repository.DBConnectionRepo
	users               *repository.UserRepo
	audit               *repository.AuditRepo
	masking             *maskingRuntime
	notifRepo           *repository.NotificationRepo
	lark                *notification.Client
	downloadRateLimiter *exportDownloadRateLimiter
}

func NewExportHandler(
	exports *repository.ExportRepo,
	tickets *repository.TicketRepo,
	dbConns *repository.DBConnectionRepo,
	users *repository.UserRepo,
	audit *repository.AuditRepo,
	maskingRules *repository.MaskingRuleRepo,
	whitelist *repository.MaskingWhitelistRepo,
	engine *masking.Engine,
	notifRepo *repository.NotificationRepo,
	lark *notification.Client,
) *ExportHandler {
	return &ExportHandler{
		exports:             exports,
		tickets:             tickets,
		dbConns:             dbConns,
		users:               users,
		audit:               audit,
		masking:             newMaskingRuntime(users, maskingRules, whitelist, tickets, engine),
		notifRepo:           notifRepo,
		lark:                lark,
		downloadRateLimiter: newExportDownloadRateLimiter(3, time.Minute),
	}
}

type exportDownloadRateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	history map[string][]time.Time
}

func newExportDownloadRateLimiter(limit int, window time.Duration) *exportDownloadRateLimiter {
	return &exportDownloadRateLimiter{
		limit:   limit,
		window:  window,
		history: make(map[string][]time.Time),
	}
}

func (l *exportDownloadRateLimiter) Allow(key string, now time.Time) bool {
	if l == nil || key == "" || l.limit <= 0 || l.window <= 0 {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := now.Add(-l.window)
	current := l.history[key]
	filtered := current[:0]
	for _, hitAt := range current {
		if hitAt.After(cutoff) {
			filtered = append(filtered, hitAt)
		}
	}
	if len(filtered) >= l.limit {
		l.history[key] = append([]time.Time(nil), filtered...)
		return false
	}

	filtered = append(filtered, now)
	l.history[key] = append([]time.Time(nil), filtered...)
	return true
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

func (h *ExportHandler) notifyReviewers(ctx context.Context, ticketID, submitterID uint64, title, body string) {
	reviewerIDs, err := listActiveUserIDsByPermissions(ctx, h.users, []string{permissionSQLEditorExportReview})
	if err != nil {
		return
	}
	for _, reviewerID := range reviewerIDs {
		if reviewerID == submitterID {
			continue
		}
		h.sendInApp(ctx, reviewerID, "ticket_pending_review", title, body, "ticket", ticketID)
	}
}

// POST /exports
// Creates a sql_export ticket from SQL Editor context.
func (h *ExportHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())

	var req struct {
		SQLContent     string `json:"sql_content"`
		DBConnectionID uint64 `json:"db_connection_id"`
		DatabaseName   string `json:"database_name"`
		SchemaName     string `json:"schema_name"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.SQLContent == "" || req.DBConnectionID == 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "sql_content and db_connection_id are required")
		return
	}
	conn, err := h.dbConns.GetByID(r.Context(), req.DBConnectionID)
	if err != nil || conn == nil {
		jsonErr(w, http.StatusNotFound, "db connection not found")
		return
	}

	if err := sqlreview.CheckReadOnly(sqlparse.DialectFromDBType(conn.DBType), req.SQLContent); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	hasAccess, err := userCanAccessConnection(r.Context(), h.users, userID, req.DBConnectionID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "db scope check failed")
		return
	}
	if !hasAccess {
		jsonErr(w, http.StatusForbidden, "access to this connection is not allowed")
		return
	}

	analysis, err := analyzeSQLScopes(r.Context(), h.dbConns, h.masking, conn, req.SQLContent, buildQueryExecutionContext(req.DatabaseName, req.SchemaName))
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "analyze export query failed: "+err.Error())
		return
	}
	title := fmt.Sprintf("SQL Export / %s", conn.Name)
	description := fmt.Sprintf("由 SQL Editor 建立的導出申請。Sensitive=%t", analysis.ContainsSensitive)
	ticket, err := h.tickets.CreateWithScopes(r.Context(), &model.Ticket{
		Title:          title,
		Description:    &description,
		SQLContent:     req.SQLContent,
		TicketType:     model.TicketTypeSQLExport,
		DBConnectionID: &req.DBConnectionID,
		SubmitterID:    userID,
	}, analysis.Scopes)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create export ticket failed")
		return
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "ticket_submit",
		ResourceType: "ticket",
		ResourceID:   &ticket.ID,
		Details:      map[string]any{"ticket_type": ticket.TicketType, "contains_sensitive": analysis.ContainsSensitive},
		IPAddress:    clientIP(r),
	})
	body := fmt.Sprintf("工單 %s 已提交，等待匯出審核", ticket.TicketNo)
	h.sendInApp(r.Context(), userID, "ticket_submitted", "匯出工單已建立", body, "ticket", ticket.ID)
	h.notifyReviewers(r.Context(), ticket.ID, userID, "新的匯出工單待審核", body)

	jsonCreated(w, map[string]any{
		"ticket_id":          ticket.ID,
		"ticket_no":          ticket.TicketNo,
		"status":             string(ticket.Status),
		"contains_sensitive": analysis.ContainsSensitive,
		"scope_count":        len(analysis.Scopes),
	})
}

// GET /exports
// Sensitive export reviewers see all; others see only their own.
func (h *ExportHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())

	var requesterID *uint64
	if !middleware.HasPermission(r.Context(), permissionSQLEditorExportReview) {
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
		"download_url": fmt.Sprintf("/api/exports/download/%s", req.DownloadToken),
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
	if !h.downloadRateLimiter.Allow(token, time.Now()) {
		http.Error(w, "一分鐘內最多只能下載三次，請稍後再試", http.StatusTooManyRequests)
		return
	}

	conn, err := h.dbConns.GetByID(r.Context(), req.DBConnectionID)
	if err != nil || conn == nil {
		http.Error(w, "connection not found", http.StatusInternalServerError)
		return
	}

	resolvedConn, password, err := h.dbConns.ResolveCredential(conn, model.DBCredentialRoleReadonly)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	driver, dsn := pool.BuildDSN(resolvedConn, password)
	pools, err := pool.Global().GetOrCreate(conn.ID, driver, dsn)
	if err != nil {
		http.Error(w, "pool error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	queryCtx, err := h.exportQueryExecutionContext(r.Context(), req, resolvedConn)
	if err != nil {
		http.Error(w, "query context failed", http.StatusInternalServerError)
		return
	}

	result, err := executeQueryForConnection(ctx, resolvedConn, password, pools.QueryPool, req.SQLContent, queryCtx, defaultSQLEditorTimeoutSettings())
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}

	shouldApplyMasking := true
	if req.TicketID != nil && h.tickets != nil {
		ticket, err := h.tickets.GetByID(r.Context(), *req.TicketID)
		if err != nil {
			http.Error(w, "ticket lookup failed", http.StatusInternalServerError)
			return
		}
		if ticket != nil && ticket.TicketType == model.TicketTypeSQLExport && ticket.Status == model.TicketStatusApproved {
			shouldApplyMasking = false
		}
	}
	if shouldApplyMasking {
		if _, _, err := h.masking.applyResult(r.Context(), resolvedConn, req.RequesterID, result); err != nil {
			http.Error(w, "masking failed", http.StatusUnprocessableEntity)
			return
		}
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="export-%s.csv"`, token[:8]))

	cw := csv.NewWriter(w)
	_ = cw.Write(result.Columns)
	for _, row := range result.Rows {
		record := make([]string, len(row))
		for i, v := range row {
			if v == nil {
				record[i] = ""
			} else {
				record[i] = fmt.Sprintf("%v", v)
			}
		}
		_ = cw.Write(record)
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

func (h *ExportHandler) exportQueryExecutionContext(ctx context.Context, req *model.ExportRequest, conn *model.DBConnection) (queryExecutionContext, error) {
	queryCtx := exportQueryExecutionContextFromScopes(conn, nil)

	if req == nil || req.TicketID == nil || h.tickets == nil {
		return queryCtx, nil
	}

	scopes, err := h.tickets.ListScopes(ctx, *req.TicketID)
	if err != nil {
		return queryCtx, err
	}
	return exportQueryExecutionContextFromScopes(conn, scopes), nil
}

func exportQueryExecutionContextFromScopes(conn *model.DBConnection, scopes []model.TicketScope) queryExecutionContext {
	queryCtx := queryExecutionContext{}

	for _, scope := range scopes {
		if queryCtx.DatabaseName == "" && scope.DatabaseName != nil {
			queryCtx.DatabaseName = *scope.DatabaseName
		}
		if queryCtx.SchemaName == "" && scope.SchemaName != nil {
			queryCtx.SchemaName = *scope.SchemaName
		}
		if queryCtx.DatabaseName != "" && queryCtx.SchemaName != "" {
			break
		}
	}

	if queryCtx.DatabaseName == "" && conn != nil && conn.DatabaseName != nil {
		queryCtx.DatabaseName = *conn.DatabaseName
	}

	return queryCtx
}
