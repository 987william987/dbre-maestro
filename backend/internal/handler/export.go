package handler

import (
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/masking"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/notification"
	"github.com/dbre-maestro/maestro/internal/pool"
	"github.com/dbre-maestro/maestro/internal/queryaccess"
	"github.com/dbre-maestro/maestro/internal/realtime"
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
	settings            *repository.SettingsRepo
	queryAccess         *queryaccess.Service
	masking             *maskingRuntime
	notifRepo           *repository.NotificationRepo
	broker              *realtime.Broker
	lark                *notification.Dispatcher
	notifications       *NotificationRouter
	downloadRateLimiter requestRateLimiter
	appBaseURL          string
	jwtSecret           []byte
}

func NewExportHandler(
	exports *repository.ExportRepo,
	tickets *repository.TicketRepo,
	dbConns *repository.DBConnectionRepo,
	users *repository.UserRepo,
	audit *repository.AuditRepo,
	settings *repository.SettingsRepo,
	queryAccessRepo *repository.QueryAccessRepo,
	maskingRules *repository.MaskingRuleRepo,
	whitelist *repository.MaskingWhitelistRepo,
	engine *masking.Engine,
	notifRepo *repository.NotificationRepo,
	broker *realtime.Broker,
	lark *notification.Dispatcher,
	appBaseURL string,
	jwtSecret []byte,
) *ExportHandler {
	return &ExportHandler{
		exports:             exports,
		tickets:             tickets,
		dbConns:             dbConns,
		users:               users,
		audit:               audit,
		settings:            settings,
		queryAccess:         queryaccess.NewService(queryAccessRepo, users),
		masking:             newMaskingRuntime(users, maskingRules, whitelist, tickets, engine),
		notifRepo:           notifRepo,
		broker:              broker,
		lark:                lark,
		notifications:       NewNotificationRouter(notifRepo, audit, broker, lark),
		downloadRateLimiter: newRequestRateLimiter(3, time.Minute),
		appBaseURL:          strings.TrimRight(appBaseURL, "/"),
		jwtSecret:           append([]byte(nil), jwtSecret...),
	}
}

func buildTicketNotificationBody(ticket *model.Ticket, connName *string, submitterName string, currentStatus, link string) string {
	parts := []string{
		fmt.Sprintf("工單類型：%s", exportTicketTypeLabel(ticket.TicketType)),
		fmt.Sprintf("目前狀態：%s", currentStatus),
	}
	if strings.TrimSpace(submitterName) != "" {
		parts = append(parts, fmt.Sprintf("提交者：%s", strings.TrimSpace(submitterName)))
	}
	if ticket.TicketType == model.TicketTypeSQLExport && ticket.ContainsSensitive != nil {
		parts = append(parts, fmt.Sprintf("導出類型：%s", exportSensitivityLabel(*ticket.ContainsSensitive)))
	}
	if connName != nil && strings.TrimSpace(*connName) != "" {
		parts = append(parts, fmt.Sprintf("數據庫實例：%s", strings.TrimSpace(*connName)))
	}
	if ticket.DatabaseName != nil && strings.TrimSpace(*ticket.DatabaseName) != "" {
		parts = append(parts, fmt.Sprintf("數據庫：%s", strings.TrimSpace(*ticket.DatabaseName)))
	}
	if ticket.SchemaName != nil && strings.TrimSpace(*ticket.SchemaName) != "" {
		parts = append(parts, fmt.Sprintf("Schema：%s", strings.TrimSpace(*ticket.SchemaName)))
	}
	if strings.TrimSpace(link) != "" {
		parts = append(parts, fmt.Sprintf("工單連結：%s", strings.TrimSpace(link)))
	}
	return strings.Join(parts, "\n")
}

func exportSensitivityLabel(containsSensitive bool) string {
	if containsSensitive {
		return "敏感數據導出"
	}
	return "普通數據導出"
}

func exportTicketTypeLabel(ticketType model.TicketType) string {
	switch ticketType {
	case model.TicketTypeDDL:
		return "DDL"
	case model.TicketTypeDML:
		return "DML"
	case model.TicketTypeRedisCommand:
		return "REDIS_COMMAND"
	case model.TicketTypeQueryAccess:
		return "QUERY_ACCESS"
	case model.TicketTypeSQLExport:
		return "SQL_EXPORT"
	case model.TicketTypeSensitiveQueryAccess:
		return "SENSITIVE_QUERY_ACCESS"
	default:
		return strings.ToUpper(string(ticketType))
	}
}

func exportTicketStateLabel(status model.TicketStatus) string {
	switch status {
	case model.TicketStatusPendingReview:
		return "待審核"
	case model.TicketStatusApproved:
		return "已核准"
	case model.TicketStatusRejected:
		return "已駁回"
	case model.TicketStatusPendingExecution:
		return "待執行"
	case model.TicketStatusExecuting:
		return "執行中"
	case model.TicketStatusCompleted:
		return "已完成"
	case model.TicketStatusFailed:
		return "執行失敗"
	case model.TicketStatusStopped:
		return "已停止"
	case model.TicketStatusInterrupted:
		return "已中斷"
	case model.TicketStatusNeedsAdminAttention:
		return "需要管理員處理"
	default:
		return string(status)
	}
}

func (h *ExportHandler) ticketLink(ticketNo string) string {
	path := fmt.Sprintf("/tickets/%s", ticketNo)
	if h.appBaseURL == "" {
		return path
	}
	return h.appBaseURL + path
}

func exportPendingReviewTitle() string {
	return "工單待審核"
}

func (h *ExportHandler) loadTicketNotificationContext(ctx context.Context, ticket *model.Ticket) *string {
	if ticket == nil || ticket.DBConnectionID == nil || h.dbConns == nil {
		return nil
	}
	conn, err := h.dbConns.GetByID(ctx, *ticket.DBConnectionID)
	if err != nil || conn == nil {
		return nil
	}
	return &conn.Name
}

func (h *ExportHandler) ticketSubmitterName(ctx context.Context, ticket *model.Ticket) string {
	if ticket == nil || ticket.SubmitterID == 0 {
		return ""
	}
	if h.users == nil {
		return strconv.FormatUint(ticket.SubmitterID, 10)
	}
	user, err := h.users.GetByID(ctx, ticket.SubmitterID)
	if err != nil || user == nil || strings.TrimSpace(user.Username) == "" {
		return strconv.FormatUint(ticket.SubmitterID, 10)
	}
	return user.Username
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
		QueryContext   string `json:"query_context_token"`
		Reason         string `json:"reason"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.SQLContent == "" || req.DBConnectionID == 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "sql_content and db_connection_id are required")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "export reason is required")
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
	if err := h.queryAccess.CheckSQL(r.Context(), userID, conn, req.SQLContent, queryaccess.CheckContext{
		DatabaseName: strings.TrimSpace(req.DatabaseName),
		SchemaName:   queryContextSchemaName(conn, req.SchemaName),
	}); err != nil {
		if missingErr, ok := err.(*queryaccess.MissingAccessError); ok {
			jsonErr(w, http.StatusForbidden, missingErr.Error())
			return
		}
		slog.Error("query access check failed for export", "user_id", userID, "connection_id", conn.ID, "err", err)
		jsonErr(w, http.StatusUnprocessableEntity, "Query access is temporarily unavailable. Please try again later.")
		return
	}

	analysis, err := validateQueryContextToken(h.jwtSecret, req.QueryContext, userID, conn.ID, req.SQLContent, req.DatabaseName, queryContextSchemaName(conn, req.SchemaName))
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	containsSensitive := analysis.ContainsSensitive
	title := fmt.Sprintf("SQL Export / %s", conn.Name)
	description := fmt.Sprintf("導出原因：%s\nSensitive=%t", reason, containsSensitive)
	ticket, err := h.tickets.CreateWithScopes(r.Context(), &model.Ticket{
		Title:             title,
		Description:       &description,
		SQLContent:        req.SQLContent,
		TicketType:        model.TicketTypeSQLExport,
		ContainsSensitive: &containsSensitive,
		DBConnectionID:    &req.DBConnectionID,
		DatabaseName:      nullableTrimmedString(req.DatabaseName),
		SchemaName:        nullableTrimmedString(queryContextSchemaName(conn, req.SchemaName)),
		SubmitterID:       userID,
	}, analysis.Scopes)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create export ticket failed")
		return
	}
	resolution, err := resolveTicketWorkflow(r.Context(), h.settings, h.users, ticket)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "resolve export workflow failed")
		return
	}
	if err := h.tickets.SaveWorkflowSnapshot(r.Context(), ticket.ID, resolution); err != nil {
		jsonErr(w, http.StatusInternalServerError, "save export workflow snapshot failed")
		return
	}
	if resolution == nil || resolution.ErrorCode != "" {
		comment := "Workflow resolution failed."
		if resolution != nil && resolution.ErrorMessage != "" {
			comment = resolution.ErrorMessage
		}
		if _, err := h.tickets.UpdateStatus(r.Context(), ticket.ID, model.TicketStatusPendingReview, model.TicketStatusNeedsAdminAttention, nil, &comment, nil); err != nil {
			jsonErr(w, http.StatusInternalServerError, "mark export workflow attention failed")
			return
		}
		if updated, err := h.tickets.GetByID(r.Context(), ticket.ID); err == nil && updated != nil {
			ticket = updated
		}
		h.audit.Log(r.Context(), repository.AuditEntry{
			ActorID:      &userID,
			ActorName:    middleware.UsernameFromCtx(r.Context()),
			ActionType:   "workflow_resolution_failed",
			ResourceType: "ticket",
			ResourceID:   &ticket.ID,
			Details:      workflowAuditDetails(ticket, resolution),
			IPAddress:    clientIP(r),
		})
		connName := h.loadTicketNotificationContext(r.Context(), ticket)
		submitterName := h.ticketSubmitterName(r.Context(), ticket)
		body := buildTicketNotificationBody(ticket, connName, submitterName, exportTicketStateLabel(ticket.Status), h.ticketLink(ticket.TicketNo))
		h.notifications.SendTicket(r.Context(), ticket, NotificationRoute{
			RecipientIDs: resolution.AdminUserIDs,
			ActorID:      &userID,
			NotifType:    "ticket_needs_admin_attention",
			Title:        "工單需要管理員處理",
			Body:         body,
		})
		publishTicketRealtimeEvent(r.Context(), h.broker, ticket, resolution, &userID)
		jsonCreated(w, map[string]any{
			"ticket_id":          ticket.ID,
			"ticket_no":          ticket.TicketNo,
			"status":             string(ticket.Status),
			"contains_sensitive": containsSensitive,
			"scope_count":        len(analysis.Scopes),
		})
		return
	}
	if !resolution.ApprovalEnabled {
		comment := "Auto-approved because workflow rule approval is disabled."
		ok, err := h.tickets.UpdateStatus(r.Context(), ticket.ID, model.TicketStatusPendingReview, model.TicketStatusApproved, nil, &comment, nil)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "auto-approve export ticket failed")
			return
		}
		if !ok {
			jsonErr(w, http.StatusConflict, "ticket status changed concurrently")
			return
		}
		updated, err := h.tickets.GetByID(r.Context(), ticket.ID)
		if err != nil || updated == nil {
			jsonErr(w, http.StatusInternalServerError, "load auto-approved export ticket failed")
			return
		}
		ticket = updated
		if _, err := h.ensureReadyExportRequest(r.Context(), ticket); err != nil {
			jsonErr(w, http.StatusInternalServerError, "create ready export failed")
			return
		}
		h.audit.Log(r.Context(), repository.AuditEntry{
			ActorID:      &userID,
			ActorName:    middleware.UsernameFromCtx(r.Context()),
			ActionType:   "ticket_auto_approve",
			ResourceType: "ticket",
			ResourceID:   &ticket.ID,
			Details: map[string]any{
				"ticket_type":        ticket.TicketType,
				"contains_sensitive": containsSensitive,
				"workflow_rule_id":   *resolution.RuleID,
			},
			IPAddress: clientIP(r),
		})
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "ticket_submit",
		ResourceType: "ticket",
		ResourceID:   &ticket.ID,
		Details: map[string]any{
			"ticket_type":        ticket.TicketType,
			"contains_sensitive": containsSensitive,
			"export_reason":      reason,
		},
		IPAddress: clientIP(r),
	})
	connName := h.loadTicketNotificationContext(r.Context(), ticket)
	submitterName := h.ticketSubmitterName(r.Context(), ticket)
	if resolution.ApprovalEnabled {
		body := buildTicketNotificationBody(
			ticket,
			connName,
			submitterName,
			exportTicketStateLabel(ticket.Status),
			h.ticketLink(ticket.TicketNo),
		)
		h.notifications.SendTicket(r.Context(), ticket, NotificationRoute{
			RecipientIDs: []uint64{userID},
			ActorID:      &userID,
			NotifyActor:  true,
			NotifType:    "ticket_submitted",
			Title:        fmt.Sprintf("匯出工單已建立：%s", ticket.TicketNo),
			Body:         body,
		})
		h.notifications.SendTicket(r.Context(), ticket, NotificationRoute{
			RecipientIDs: resolution.ApprovalUserIDs,
			ActorID:      &userID,
			NotifType:    "ticket_pending_review",
			Title:        exportPendingReviewTitle(),
			Body:         body,
		})
	} else {
		body := buildTicketNotificationBody(
			ticket,
			connName,
			submitterName,
			exportTicketStateLabel(ticket.Status),
			h.ticketLink(ticket.TicketNo),
		)
		h.notifications.SendTicket(r.Context(), ticket, NotificationRoute{
			RecipientIDs: []uint64{userID},
			ActorID:      &userID,
			NotifyActor:  true,
			NotifType:    "ticket_auto_approved",
			Title:        fmt.Sprintf("普通匯出已建立：%s", ticket.TicketNo),
			Body:         body,
		})
	}
	publishTicketRealtimeEvent(r.Context(), h.broker, ticket, resolution, &userID)

	jsonCreated(w, map[string]any{
		"ticket_id":          ticket.ID,
		"ticket_no":          ticket.TicketNo,
		"status":             string(ticket.Status),
		"contains_sensitive": containsSensitive,
		"scope_count":        len(analysis.Scopes),
	})
}

func (h *ExportHandler) ensureReadyExportRequest(ctx context.Context, ticket *model.Ticket) (*model.ExportRequest, error) {
	if ticket.DBConnectionID == nil {
		return nil, fmt.Errorf("export ticket has no db connection")
	}
	existing, err := h.exports.GetByTicketID(ctx, ticket.ID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	exportTicketID := ticket.ID
	id, token, err := h.exports.Create(ctx, &model.ExportRequest{
		TicketID:       &exportTicketID,
		RequesterID:    ticket.SubmitterID,
		SQLContent:     ticket.SQLContent,
		DBConnectionID: *ticket.DBConnectionID,
	}, model.ExportStatusReady)
	if err != nil {
		return nil, err
	}
	req, err := h.exports.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if req != nil && req.DownloadToken == "" {
		req.DownloadToken = token
	}
	return req, nil
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

	approveBody := fmt.Sprintf("Export request #%d was approved. Please download it before it expires.", id)
	h.notifications.Send(r.Context(), NotificationRoute{
		RecipientIDs: []uint64{req.RequesterID},
		NotifType:    "export_approved",
		Title:        "導出申請已通過",
		Body:         approveBody,
		ResourceType: "export",
		ResourceID:   id,
		NotifyActor:  true,
	})

	jsonOK(w, map[string]any{
		"id":           id,
		"status":       string(model.ExportStatusReady),
		"download_url": fmt.Sprintf("/api/exports/%d/download", id),
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

	rejectBody := fmt.Sprintf("Export request #%d was rejected.", id)
	h.notifications.Send(r.Context(), NotificationRoute{
		RecipientIDs: []uint64{req.RequesterID},
		NotifType:    "export_rejected",
		Title:        "導出申請已拒絕",
		Body:         rejectBody,
		ResourceType: "export",
		ResourceID:   id,
		NotifyActor:  true,
	})

	w.WriteHeader(http.StatusNoContent)
}

// GET /exports/download/{token} — legacy authenticated download route.
// Token-not-found, expired, or unauthorized all avoid token enumeration.
func (h *ExportHandler) Download(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	userID := middleware.UserIDFromCtx(r.Context())
	if userID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if len(token) != 64 {
		h.auditExportDownloadFailure(r, userID, nil, "invalid_token")
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	req, err := h.exports.GetByToken(r.Context(), token)
	if err != nil {
		h.auditExportDownloadFailure(r, userID, nil, "lookup_failed")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if req == nil {
		h.auditExportDownloadFailure(r, userID, nil, "not_found")
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	h.downloadExportRequest(w, r, req, userID)
}

// GET /exports/{id}/download — authenticated repeated download within export TTL.
func (h *ExportHandler) DownloadByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	userID := middleware.UserIDFromCtx(r.Context())
	if userID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	req, err := h.exports.GetByID(r.Context(), id)
	if err != nil {
		h.auditExportDownloadFailure(r, userID, nil, "lookup_failed")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if req == nil {
		h.auditExportDownloadFailure(r, userID, nil, "not_found")
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	h.downloadExportRequest(w, r, req, userID)
}

func (h *ExportHandler) downloadExportRequest(w http.ResponseWriter, r *http.Request, req *model.ExportRequest, userID uint64) {
	if time.Now().After(req.ExpiresAt) {
		h.auditExportDownloadFailure(r, userID, req, "expired")
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if req.Status != model.ExportStatusReady {
		h.auditExportDownloadFailure(r, userID, req, "not_ready")
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if req.RequesterID != userID && (req.ApproverID == nil || *req.ApproverID != userID) && !middleware.HasPermission(r.Context(), permissionSQLEditorExportReview) {
		h.auditExportDownloadFailure(r, userID, req, "unauthorized")
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	rateLimitKey := fmt.Sprintf("export-download:%d", req.ID)
	if !h.downloadRateLimiter.Allow(rateLimitKey, time.Now()) {
		h.auditExportDownloadFailure(r, userID, req, "rate_limited")
		http.Error(w, "At most three downloads are allowed per minute. Please try again later.", http.StatusTooManyRequests)
		return
	}
	if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil {
		h.auditExportDownloadFailure(r, userID, req, "streaming_unsupported")
		http.Error(w, "download streaming unsupported", http.StatusInternalServerError)
		return
	}

	var ticket *model.Ticket
	var err error
	if req.TicketID != nil && h.tickets != nil {
		ticket, err = h.tickets.GetByID(r.Context(), *req.TicketID)
		if err != nil {
			h.auditExportDownloadFailure(r, userID, req, "ticket_lookup_failed")
			http.Error(w, "ticket lookup failed", http.StatusInternalServerError)
			return
		}
		if ticket == nil || ticket.TicketType != model.TicketTypeSQLExport || ticket.Status != model.TicketStatusApproved {
			h.auditExportDownloadFailure(r, userID, req, "ticket_not_approved")
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	conn, err := h.dbConns.GetByID(r.Context(), req.DBConnectionID)
	if err != nil || conn == nil {
		h.auditExportDownloadFailure(r, userID, req, "connection_not_found")
		http.Error(w, "connection not found", http.StatusInternalServerError)
		return
	}

	resolvedConn, password, err := h.dbConns.ResolveCredential(conn, model.DBCredentialRoleReadonly)
	if err != nil {
		h.auditExportDownloadFailure(r, userID, req, "credential_resolution_failed")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	driver, dsn := pool.BuildDSN(resolvedConn, password)
	pools, err := pool.Global().GetOrCreate(conn.ID, driver, dsn)
	if err != nil {
		h.auditExportDownloadFailure(r, userID, req, "pool_failed")
		http.Error(w, "pool error", http.StatusInternalServerError)
		return
	}

	timeoutSettings := h.loadSQLExportTimeoutSettings(r.Context())
	ctx, cancel := context.WithTimeout(r.Context(), timeoutSettings.AppTimeout)
	defer cancel()

	queryCtx, err := h.exportQueryExecutionContext(r.Context(), req, resolvedConn)
	if err != nil {
		h.auditExportDownloadFailure(r, userID, req, "query_context_failed")
		http.Error(w, "query context failed", http.StatusInternalServerError)
		return
	}

	result, err := executeQueryForConnection(ctx, resolvedConn, password, pools.QueryPool, req.SQLContent, queryCtx, timeoutSettings)
	if err != nil {
		h.auditExportDownloadFailure(r, userID, req, "query_failed")
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}

	shouldApplyMasking := true
	if ticket != nil && ticket.TicketType == model.TicketTypeSQLExport && ticket.Status == model.TicketStatusApproved {
		shouldApplyMasking = false
	}
	if shouldApplyMasking {
		if _, _, err := h.masking.applyResult(r.Context(), resolvedConn, req.RequesterID, result); err != nil {
			h.auditExportDownloadFailure(r, userID, req, "masking_failed")
			http.Error(w, "masking failed", http.StatusUnprocessableEntity)
			return
		}
	}

	if err := h.exports.MarkDownloaded(r.Context(), req.ID); err != nil {
		h.auditExportDownloadFailure(r, userID, req, "mark_downloaded_failed")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="export-%d.csv"`, req.ID))

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

	resourceType := "export"
	reqID := req.ID
	resourceID := &reqID
	if req.TicketID != nil {
		resourceType = "ticket"
		resourceID = req.TicketID
	}
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "export_download",
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Details: map[string]any{
			"export_id": req.ID,
			"ticket_id": req.TicketID,
			"rows":      len(result.Rows),
		},
		IPAddress: clientIP(r),
	})
}

func (h *ExportHandler) auditExportDownloadFailure(r *http.Request, userID uint64, req *model.ExportRequest, reason string) {
	if h.audit == nil {
		return
	}
	resourceType := "export"
	var resourceID *uint64
	details := map[string]any{
		"reason": reason,
	}
	if req != nil {
		resourceID = &req.ID
		if req.TicketID != nil {
			resourceType = "ticket"
			resourceID = req.TicketID
		}
		details["export_id"] = req.ID
		details["ticket_id"] = req.TicketID
		details["status"] = req.Status
		details["expires_at"] = req.ExpiresAt
	}
	_ = h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "export_download_failed",
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Details:      details,
		IPAddress:    clientIP(r),
	})
}

func (h *ExportHandler) loadSQLExportTimeoutSettings(ctx context.Context) sqlEditorTimeoutSettings {
	settings := defaultSQLEditorTimeoutSettings()
	if h.settings == nil {
		return settings
	}

	platformSettings, err := h.settings.Get(ctx)
	if err != nil || platformSettings == nil {
		return settings
	}
	if platformSettings.SQLExportAppTimeoutSeconds > 0 {
		settings.AppTimeout = time.Duration(platformSettings.SQLExportAppTimeoutSeconds) * time.Second
	}
	if platformSettings.SQLExportMySQLMaxExecutionTimeMs > 0 {
		settings.MySQLMaxExecutionTimeMs = platformSettings.SQLExportMySQLMaxExecutionTimeMs
	}
	if platformSettings.SQLExportPostgresStatementTimeoutMs > 0 {
		settings.PostgresStatementTimeoutMs = platformSettings.SQLExportPostgresStatementTimeoutMs
	}
	return settings
}

func nullableTrimmedString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func (h *ExportHandler) exportQueryExecutionContext(ctx context.Context, req *model.ExportRequest, conn *model.DBConnection) (queryExecutionContext, error) {
	queryCtx := exportQueryExecutionContextFromContext(conn, "", "", nil)

	if req == nil || req.TicketID == nil || h.tickets == nil {
		return queryCtx, nil
	}

	ticket, err := h.tickets.GetByID(ctx, *req.TicketID)
	if err != nil {
		return queryCtx, err
	}
	ticketDatabaseName := ""
	ticketSchemaName := ""
	if ticket != nil && ticket.DatabaseName != nil {
		ticketDatabaseName = *ticket.DatabaseName
	}
	if ticket != nil && ticket.SchemaName != nil {
		ticketSchemaName = *ticket.SchemaName
	}
	scopes, err := h.tickets.ListScopes(ctx, *req.TicketID)
	if err != nil {
		return queryCtx, err
	}
	return exportQueryExecutionContextFromContext(conn, ticketDatabaseName, ticketSchemaName, scopes), nil
}

func exportQueryExecutionContextFromContext(conn *model.DBConnection, ticketDatabaseName string, ticketSchemaName string, scopes []model.TicketScope) queryExecutionContext {
	queryCtx := queryExecutionContext{}

	if queryCtx.DatabaseName == "" {
		queryCtx.DatabaseName = strings.TrimSpace(ticketDatabaseName)
	}
	if queryCtx.SchemaName == "" {
		queryCtx.SchemaName = strings.TrimSpace(ticketSchemaName)
	}
	for _, scope := range scopes {
		if queryCtx.DatabaseName == "" && scope.DatabaseName != nil {
			queryCtx.DatabaseName = strings.TrimSpace(*scope.DatabaseName)
		}
		if queryCtx.SchemaName == "" && scope.SchemaName != nil {
			queryCtx.SchemaName = strings.TrimSpace(*scope.SchemaName)
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
