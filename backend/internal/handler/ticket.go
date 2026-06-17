package handler

import (
	"context"
	"database/sql"
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
	"github.com/dbre-maestro/maestro/internal/realtime"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/dbre-maestro/maestro/internal/sqlparse"
	"github.com/dbre-maestro/maestro/internal/sqlpolicy"
	ticketsm "github.com/dbre-maestro/maestro/internal/ticket"
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

type TicketHandler struct {
	tickets            *repository.TicketRepo
	exports            *repository.ExportRepo
	audit              *repository.AuditRepo
	dbConns            *repository.DBConnectionRepo
	users              *repository.UserRepo
	masking            *maskingRuntime
	sqlReviewRules     *repository.SQLReviewRuleRepo
	shadowValidationDB *sqlx.DB
	notifRepo          *repository.NotificationRepo
	broker             *realtime.Broker
	lark               *notification.Dispatcher
	appBaseURL         string
}

type ticketResponse struct {
	model.Ticket
	DBConnectionName *string `json:"db_connection_name,omitempty"`
	SubmitterName    string  `json:"submitter_name"`
	ReviewerName     *string `json:"reviewer_name,omitempty"`
	ExecutorName     *string `json:"executor_name,omitempty"`
	RevokedByName    *string `json:"revoked_by_name,omitempty"`
}

type ticketWorkflowParticipants struct {
	Reviewers []string `json:"reviewers"`
	Executors []string `json:"executors"`
}

type ticketReviewItem struct {
	Seq              int     `json:"seq"`
	SQLStmt          string  `json:"sql_stmt"`
	Phase            string  `json:"phase"`
	ValidationStage  *string `json:"validation_stage,omitempty"`
	StatementKind    *string `json:"statement_kind,omitempty"`
	ObjectType       *string `json:"object_type,omitempty"`
	ValidationMethod *string `json:"validation_method,omitempty"`
	ScanRows         int64   `json:"scan_rows"`
	Status           string  `json:"status"`
	Message          *string `json:"message,omitempty"`
}

type ticketDatabaseOption struct {
	Name string `json:"name"`
}

type ticketNotificationEvent string

const (
	ticketEventPendingReview    ticketNotificationEvent = "pending_review"
	ticketEventApproved         ticketNotificationEvent = "approved"
	ticketEventRejected         ticketNotificationEvent = "rejected"
	ticketEventWithdrawn        ticketNotificationEvent = "withdrawn"
	ticketEventPendingExecution ticketNotificationEvent = "pending_execution"
	ticketEventCompleted        ticketNotificationEvent = "completed"
	ticketEventExecutionFailed  ticketNotificationEvent = "execution_failed"
	ticketEventRevoked          ticketNotificationEvent = "revoked"
)

type ticketRecipientRole string

const (
	ticketRoleSubmitter        ticketRecipientRole = "submitter"
	ticketRoleReviewer         ticketRecipientRole = "reviewer"
	ticketRoleExecutorPool     ticketRecipientRole = "executor_pool"
	ticketRoleAssignedExecutor ticketRecipientRole = "assigned_executor"
)

type ticketNotificationPolicy struct {
	Title       string
	NotifType   string
	Roles       []ticketRecipientRole
	NotifyActor bool
	Status      model.TicketStatus
	NextAction  string
}

var ticketNotificationPolicies = map[ticketNotificationEvent]ticketNotificationPolicy{
	ticketEventPendingReview: {
		Title:       "工單待審核",
		NotifType:   "ticket_pending_review",
		Roles:       []ticketRecipientRole{ticketRoleReviewer},
		NotifyActor: false,
		Status:      model.TicketStatusPendingReview,
		NextAction:  "請審核是否通過此工單",
	},
	ticketEventApproved: {
		Title:       "工單已核准",
		NotifType:   "ticket_approved",
		Roles:       []ticketRecipientRole{ticketRoleSubmitter},
		NotifyActor: false,
		Status:      model.TicketStatusApproved,
		NextAction:  "請查看工單詳情",
	},
	ticketEventRejected: {
		Title:       "工單已駁回",
		NotifType:   "ticket_rejected",
		Roles:       []ticketRecipientRole{ticketRoleSubmitter},
		NotifyActor: false,
		Status:      model.TicketStatusRejected,
		NextAction:  "請依駁回原因修正後重新提交",
	},
	ticketEventWithdrawn: {
		Title:       "工單已收回",
		NotifType:   "ticket_withdrawn",
		Roles:       []ticketRecipientRole{ticketRoleReviewer},
		NotifyActor: false,
		Status:      model.TicketStatusWithdrawn,
		NextAction:  "無需再處理此工單",
	},
	ticketEventPendingExecution: {
		Title:       "工單待執行",
		NotifType:   "ticket_pending_execution",
		Roles:       []ticketRecipientRole{ticketRoleExecutorPool},
		NotifyActor: false,
		Status:      model.TicketStatusPendingExecution,
		NextAction:  "請執行此工單",
	},
	ticketEventCompleted: {
		Title:       "工單已完成",
		NotifType:   "ticket_executed",
		Roles:       []ticketRecipientRole{ticketRoleSubmitter},
		NotifyActor: true,
		Status:      model.TicketStatusCompleted,
		NextAction:  "請查看執行結果",
	},
	ticketEventExecutionFailed: {
		Title:       "工單執行失敗",
		NotifType:   "ticket_executed",
		Roles:       []ticketRecipientRole{ticketRoleSubmitter, ticketRoleAssignedExecutor},
		NotifyActor: true,
		Status:      model.TicketStatusFailed,
		NextAction:  "請查看錯誤並重新處理",
	},
	ticketEventRevoked: {
		Title:       "工單已撤銷",
		NotifType:   "ticket_revoked",
		Roles:       []ticketRecipientRole{ticketRoleSubmitter},
		NotifyActor: false,
		Status:      model.TicketStatusApproved,
		NextAction:  "請查看工單詳情",
	},
}

func NewTicketHandler(
	tickets *repository.TicketRepo,
	exports *repository.ExportRepo,
	audit *repository.AuditRepo,
	dbConns *repository.DBConnectionRepo,
	users *repository.UserRepo,
	maskingRules *repository.MaskingRuleRepo,
	whitelist *repository.MaskingWhitelistRepo,
	engine *masking.Engine,
	sqlReviewRules *repository.SQLReviewRuleRepo,
	shadowValidationDB *sqlx.DB,
	lark *notification.Dispatcher,
	notifRepo *repository.NotificationRepo,
	broker *realtime.Broker,
	appBaseURL string,
) *TicketHandler {
	return &TicketHandler{
		tickets:            tickets,
		exports:            exports,
		audit:              audit,
		dbConns:            dbConns,
		users:              users,
		masking:            newMaskingRuntime(users, maskingRules, whitelist, tickets, engine),
		sqlReviewRules:     sqlReviewRules,
		shadowValidationDB: shadowValidationDB,
		notifRepo:          notifRepo,
		broker:             broker,
		lark:               lark,
		appBaseURL:         strings.TrimRight(appBaseURL, "/"),
	}
}

func (h *TicketHandler) notifyLarkUsers(ctx context.Context, userIDs []uint64, title, body, ticketNo string) {
	if h.lark == nil || len(userIDs) == 0 {
		return
	}
	result := h.lark.NotifyUsers(ctx, userIDs, notification.Message{Title: title, Body: body, TicketNo: ticketNo})
	if result.Err != nil {
		h.audit.Log(ctx, repository.AuditEntry{
			ActionType: "notification_failure",
			Details: map[string]any{
				"err":      result.Err.Error(),
				"attempts": result.Attempts,
			},
		})
	}
}

func (h *TicketHandler) ticketLink(ticketID uint64) string {
	path := fmt.Sprintf("/tickets/%d", ticketID)
	if h.appBaseURL == "" {
		return path
	}
	return h.appBaseURL + path
}

func (h *TicketHandler) ticketStateLabel(status model.TicketStatus) string {
	switch status {
	case model.TicketStatusPendingReview:
		return "待審核"
	case model.TicketStatusApproved:
		return "已核准"
	case model.TicketStatusRejected:
		return "已駁回"
	case model.TicketStatusWithdrawn:
		return "已收回"
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
	default:
		return string(status)
	}
}

func (h *TicketHandler) ticketTypeLabel(ticketType model.TicketType) string {
	switch ticketType {
	case model.TicketTypeDDL:
		return "DDL"
	case model.TicketTypeDML:
		return "DML"
	case model.TicketTypeRedisCommand:
		return "REDIS_COMMAND"
	case model.TicketTypeSQLExport:
		return "SQL_EXPORT"
	case model.TicketTypeSensitiveQueryAccess:
		return "SENSITIVE_QUERY_ACCESS"
	default:
		return strings.ToUpper(string(ticketType))
	}
}

func (h *TicketHandler) buildTicketNotificationBody(ticket *model.Ticket, currentStatus model.TicketStatus, nextAction string, detail string) string {
	parts := []string{
		fmt.Sprintf("工單類型：%s", h.ticketTypeLabel(ticket.TicketType)),
		fmt.Sprintf("目前狀態：%s", h.ticketStateLabel(currentStatus)),
	}
	if nextAction != "" {
		parts = append(parts, fmt.Sprintf("待執行操作：%s", nextAction))
	}
	if ticket.DBConnectionID != nil && h.dbConns != nil {
		conn, err := h.dbConns.GetByID(context.Background(), *ticket.DBConnectionID)
		if err == nil && conn != nil {
			parts = append(parts, fmt.Sprintf("數據庫實例：%s", conn.Name))
		}
	}
	if ticket.DatabaseName != nil && strings.TrimSpace(*ticket.DatabaseName) != "" {
		parts = append(parts, fmt.Sprintf("數據庫：%s", strings.TrimSpace(*ticket.DatabaseName)))
	}
	if strings.TrimSpace(detail) != "" {
		parts = append(parts, fmt.Sprintf("說明：%s", strings.TrimSpace(detail)))
	}
	parts = append(parts, fmt.Sprintf("工單連結：%s", h.ticketLink(ticket.ID)))
	return strings.Join(parts, "\n")
}

func (h *TicketHandler) dispatchTicketNotification(
	ctx context.Context,
	ticket *model.Ticket,
	event ticketNotificationEvent,
	actorID *uint64,
	detail string,
) {
	policy, ok := ticketNotificationPolicies[event]
	if !ok {
		return
	}
	recipientIDs, err := h.resolveTicketNotificationRecipients(ctx, ticket, policy, actorID)
	if err != nil || len(recipientIDs) == 0 {
		return
	}
	body := h.buildTicketNotificationBody(ticket, policy.Status, policy.NextAction, detail)
	for _, recipientID := range recipientIDs {
		h.sendInApp(ctx, recipientID, policy.NotifType, policy.Title, body, "ticket", ticket.ID)
	}
	h.notifyLarkUsers(ctx, recipientIDs, policy.Title, body, ticket.TicketNo)
}

func (h *TicketHandler) resolveTicketNotificationRecipients(
	ctx context.Context,
	ticket *model.Ticket,
	policy ticketNotificationPolicy,
	actorID *uint64,
) ([]uint64, error) {
	seen := make(map[uint64]struct{})
	recipients := make([]uint64, 0, 4)
	addRecipient := func(userID uint64) {
		if userID == 0 {
			return
		}
		if actorID != nil && !policy.NotifyActor && *actorID == userID {
			return
		}
		if _, ok := seen[userID]; ok {
			return
		}
		seen[userID] = struct{}{}
		recipients = append(recipients, userID)
	}

	for _, role := range policy.Roles {
		switch role {
		case ticketRoleSubmitter:
			addRecipient(ticket.SubmitterID)
		case ticketRoleReviewer:
			reviewerIDs, err := listActiveUserIDsByPermissions(ctx, h.users, reviewPermissionsForTicket(ticket.TicketType))
			if err != nil {
				return nil, err
			}
			for _, reviewerID := range reviewerIDs {
				addRecipient(reviewerID)
			}
		case ticketRoleExecutorPool:
			executorIDs, err := listActiveUserIDsByPermissions(ctx, h.users, []string{permissionTicketExecute})
			if err != nil {
				return nil, err
			}
			for _, executorID := range executorIDs {
				addRecipient(executorID)
			}
		case ticketRoleAssignedExecutor:
			if ticket.ExecutorID != nil {
				addRecipient(*ticket.ExecutorID)
			}
		}
	}

	return recipients, nil
}

func (h *TicketHandler) sendInApp(ctx context.Context, userID uint64, notifType, title, body, resType string, resID uint64) {
	if h.notifRepo == nil {
		return
	}
	notificationID, err := h.notifRepo.Create(ctx, userID, notifType, title, body, &resType, &resID)
	if err != nil {
		return
	}
	publishNotificationCreated(ctx, h.broker, h.notifRepo, userID, notificationID)
}

func (h *TicketHandler) publishTicketUpdate(ctx context.Context, ticket *model.Ticket, actorID *uint64) {
	if h.broker == nil || ticket == nil || h.users == nil {
		return
	}

	recipients := make([]uint64, 0, 8)
	recipients = append(recipients, ticket.SubmitterID)
	if actorID != nil && *actorID != 0 {
		recipients = append(recipients, *actorID)
	}
	if workspaceReaders, err := listActiveUserIDsByPermissions(ctx, h.users, ticketWorkspaceRealtimePermissions()); err == nil {
		recipients = append(recipients, workspaceReaders...)
	}
	if reviewerIDs, err := listActiveUserIDsByPermissions(ctx, h.users, reviewPermissionsForTicket(ticket.TicketType)); err == nil {
		recipients = append(recipients, reviewerIDs...)
	}
	if ticket.ExecutorID != nil {
		recipients = append(recipients, *ticket.ExecutorID)
	}

	h.broker.PublishToUsers(recipients, "ticket.updated", ticketUpdatedEvent{
		TicketID: ticket.ID,
		Status:   string(ticket.Status),
	})
}

// GET /tickets/connections
func (h *TicketHandler) ListConnections(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())
	connections, err := listAccessibleConnections(r.Context(), h.dbConns, h.users, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list ticket connections failed")
		return
	}
	jsonOK(w, map[string]any{"connections": connections})
}

// GET /tickets/connections/{id}/databases
func (h *TicketHandler) ListDatabases(w http.ResponseWriter, r *http.Request) {
	connID, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil || connID == 0 {
		jsonErr(w, http.StatusBadRequest, "invalid connection id")
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	hasAccess, err := userCanAccessConnection(r.Context(), h.users, userID, connID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "db scope check failed")
		return
	}
	if !hasAccess {
		jsonErr(w, http.StatusForbidden, "access to this connection is not allowed")
		return
	}

	databases, err := h.listTicketDatabases(r.Context(), connID)
	if err != nil {
		slog.Warn("list ticket databases failed", "connection_id", connID, "err", err)
		jsonErr(w, http.StatusInternalServerError, "list ticket databases failed")
		return
	}

	jsonOK(w, map[string]any{"databases": databases})
}

// POST /tickets/review
func (h *TicketHandler) ReviewSQL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SQLContent     string           `json:"sql_content"`
		TicketType     model.TicketType `json:"ticket_type"`
		DBConnectionID *uint64          `json:"db_connection_id"`
		DatabaseName   *string          `json:"database_name"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.SQLContent) == "" || req.DBConnectionID == nil {
		jsonErr(w, http.StatusUnprocessableEntity, "db_connection_id and sql_content are required")
		return
	}
	if strings.TrimSpace(nullableStringValue(req.DatabaseName)) == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "database_name is required")
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	hasAccess, err := userCanAccessConnection(r.Context(), h.users, userID, *req.DBConnectionID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "db scope check failed")
		return
	}
	if !hasAccess {
		jsonErr(w, http.StatusForbidden, "access to this connection is not allowed")
		return
	}
	if err := h.validateTicketConnectionType(r.Context(), req.TicketType, *req.DBConnectionID); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	results := h.runTicketSQLReviewWithType(r.Context(), *req.DBConnectionID, req.TicketType, req.SQLContent, req.DatabaseName)
	blocked := false
	for _, result := range results {
		if result.Status == "error" {
			blocked = true
			break
		}
	}

	jsonOK(w, map[string]any{
		"passed":  !blocked,
		"results": results,
	})
}

// POST /tickets
func (h *TicketHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())

	var req struct {
		Title                   string              `json:"title"`
		Description             *string             `json:"description"`
		SQLContent              string              `json:"sql_content"`
		TicketType              model.TicketType    `json:"ticket_type"`
		DBConnectionID          *uint64             `json:"db_connection_id"`
		DatabaseName            *string             `json:"database_name"`
		ApprovedDurationMinutes *int                `json:"approved_duration_minutes"`
		Scopes                  []model.TicketScope `json:"scopes"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" || req.SQLContent == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "title and sql_content are required")
		return
	}
	switch req.TicketType {
	case model.TicketTypeDDL, model.TicketTypeDML, model.TicketTypeRedisCommand, model.TicketTypeSQLExport, model.TicketTypeSensitiveQueryAccess:
	default:
		jsonErr(w, http.StatusUnprocessableEntity, "invalid ticket_type")
		return
	}
	if req.DBConnectionID != nil {
		hasAccess, err := userCanAccessConnection(r.Context(), h.users, userID, *req.DBConnectionID)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "db scope check failed")
			return
		}
		if !hasAccess {
			jsonErr(w, http.StatusForbidden, "access to this connection is not allowed")
			return
		}
		if err := h.validateTicketConnectionType(r.Context(), req.TicketType, *req.DBConnectionID); err != nil {
			jsonErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	if (req.TicketType == model.TicketTypeDDL || req.TicketType == model.TicketTypeDML || req.TicketType == model.TicketTypeRedisCommand) && strings.TrimSpace(nullableStringValue(req.DatabaseName)) == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "database_name is required")
		return
	}

	// SQL/Command Review
	var reviewResults []ticketReviewItem
	if req.DBConnectionID != nil && (req.TicketType == model.TicketTypeDDL || req.TicketType == model.TicketTypeDML || req.TicketType == model.TicketTypeRedisCommand) {
		reviewResults = h.runTicketSQLReviewWithType(r.Context(), *req.DBConnectionID, req.TicketType, req.SQLContent, req.DatabaseName)
		issues := make([]string, 0)
		for _, result := range reviewResults {
			if result.Status == "error" && result.Message != nil && strings.TrimSpace(*result.Message) != "" {
				issues = append(issues, fmt.Sprintf("statement %d (%s): %s", result.Seq, result.Phase, *result.Message))
			}
		}
		if len(issues) > 0 {
			jsonErr(w, http.StatusUnprocessableEntity, "SQL review failed: "+strings.Join(issues, "; "))
			return
		}
	}
	if req.TicketType == model.TicketTypeSensitiveQueryAccess {
		if req.DBConnectionID == nil || len(req.Scopes) == 0 {
			jsonErr(w, http.StatusUnprocessableEntity, "sensitive_query_access requires db_connection_id and scopes")
			return
		}
		if req.ApprovedDurationMinutes == nil || (*req.ApprovedDurationMinutes != 10 && *req.ApprovedDurationMinutes != 30 && *req.ApprovedDurationMinutes != 60) {
			jsonErr(w, http.StatusUnprocessableEntity, "approved_duration_minutes must be 10, 30, or 60")
			return
		}
	}

	t := &model.Ticket{
		Title:                   req.Title,
		Description:             req.Description,
		SQLContent:              req.SQLContent,
		TicketType:              req.TicketType,
		DBConnectionID:          req.DBConnectionID,
		DatabaseName:            optionalTrimmedString(nullableStringValue(req.DatabaseName)),
		SubmitterID:             userID,
		ApprovedDurationMinutes: req.ApprovedDurationMinutes,
	}

	created, err := h.tickets.CreateWithScopes(r.Context(), t, req.Scopes)
	if err != nil {
		slog.Error("create ticket failed",
			"err", err,
			"title", req.Title,
			"ticket_type", req.TicketType,
			"db_connection_id", req.DBConnectionID,
			"submitter_id", userID,
		)
		jsonErr(w, http.StatusInternalServerError, "create ticket failed")
		return
	}
	if len(reviewResults) > 0 {
		persistedResults := make([]model.TicketReviewResult, 0, len(reviewResults))
		for _, result := range reviewResults {
			persistedResults = append(persistedResults, model.TicketReviewResult{
				TicketID:         created.ID,
				Seq:              result.Seq,
				SQLStmt:          result.SQLStmt,
				Phase:            result.Phase,
				ValidationStage:  result.ValidationStage,
				StatementKind:    result.StatementKind,
				ObjectType:       result.ObjectType,
				ValidationMethod: result.ValidationMethod,
				ScanRows:         result.ScanRows,
				Status:           result.Status,
				Message:          result.Message,
			})
		}
		if err := h.tickets.ReplaceReviewResults(r.Context(), created.ID, persistedResults); err != nil {
			slog.Error("persist ticket review results failed", "ticket_id", created.ID, "err", err)
			jsonErr(w, http.StatusInternalServerError, "persist ticket review results failed")
			return
		}
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "ticket_submit",
		ResourceType: "ticket",
		ResourceID:   &created.ID,
		IPAddress:    clientIP(r),
	})

	h.dispatchTicketNotification(r.Context(), created, ticketEventPendingReview, &userID, "提交人已送出工單，等待 reviewer 處理。")
	h.publishTicketUpdate(r.Context(), created, &userID)
	jsonCreated(w, created)
}

// GET /tickets
func (h *TicketHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())

	limit := 20
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	filter := repository.TicketListFilter{}
	if s := r.URL.Query().Get("status"); s != "" {
		ts := model.TicketStatus(s)
		filter.Status = &ts
	}
	if s := strings.TrimSpace(r.URL.Query().Get("type")); s != "" {
		tt := model.TicketType(s)
		filter.Type = &tt
	}
	if s := strings.TrimSpace(r.URL.Query().Get("q")); s != "" {
		filter.Keyword = &s
	}
	if s := r.URL.Query().Get("from"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			filter.From = &t
		}
	}
	if s := r.URL.Query().Get("to"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			filter.To = &t
		}
	}

	// T5: IDOR — only reviewers/executors can see the full queue.
	canViewAll, err := h.canViewAllTickets(r.Context(), userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket access check failed")
		return
	}
	if !canViewAll {
		filter.SubmitterID = &userID
	}

	tickets, total, err := h.tickets.List(r.Context(), filter, limit, offset)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list tickets failed")
		return
	}
	if tickets == nil {
		tickets = []model.Ticket{}
	}

	responseTickets := make([]ticketResponse, 0, len(tickets))
	for _, ticket := range tickets {
		enriched, enrichErr := h.buildTicketResponse(r.Context(), &ticket)
		if enrichErr != nil {
			jsonErr(w, http.StatusInternalServerError, "list tickets failed")
			return
		}
		responseTickets = append(responseTickets, enriched)
	}

	jsonOK(w, map[string]any{"tickets": responseTickets, "total": total, "limit": limit, "offset": offset})
}

// GET /tickets/{id}
func (h *TicketHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := parseTicketID(w, r)
	if id == 0 {
		return
	}

	ticket, err := h.tickets.GetByID(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "get ticket failed")
		return
	}
	if ticket == nil {
		jsonErr(w, http.StatusNotFound, "ticket not found")
		return
	}

	// T5: IDOR — only reviewers/executors can view arbitrary tickets.
	userID := middleware.UserIDFromCtx(r.Context())
	canViewAll, err := h.canViewAllTickets(r.Context(), userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket access check failed")
		return
	}
	if !canViewAll {
		if ticket.SubmitterID != userID {
			jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
	}

	executions, _ := h.tickets.ListExecutions(r.Context(), id)
	if executions == nil {
		executions = []model.TicketExecution{}
	}
	scopes, _ := h.tickets.ListScopes(r.Context(), id)
	if scopes == nil {
		scopes = []model.TicketScope{}
	}
	reviewResults, _ := h.tickets.ListReviewResults(r.Context(), id)
	if reviewResults == nil {
		reviewResults = []model.TicketReviewResult{}
	}
	canReview, err := h.canReviewTicket(r.Context(), ticket, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket capability check failed")
		return
	}
	canReject, err := h.canRejectTicket(r.Context(), ticket, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket capability check failed")
		return
	}
	canWithdraw, err := h.canWithdrawTicket(r.Context(), ticket, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket capability check failed")
		return
	}
	canRevoke, err := h.canRevokeSensitiveTicket(r.Context(), ticket, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket capability check failed")
		return
	}

	var exportDetail map[string]any
	if ticket.TicketType == model.TicketTypeSQLExport && h.exports != nil {
		exportReq, err := h.exports.GetByTicketID(r.Context(), id)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "get export failed")
			return
		}
		if exportReq != nil {
			exportDetail = map[string]any{
				"status":     exportReq.Status,
				"expires_at": exportReq.ExpiresAt,
			}
			if ticket.SubmitterID == userID && exportReq.Status == model.ExportStatusReady {
				exportDetail["download_url"] = fmt.Sprintf("/api/exports/download/%s", exportReq.DownloadToken)
			}
		}
	}

	enrichedTicket, err := h.buildTicketResponse(r.Context(), ticket)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "get ticket failed")
		return
	}
	workflowParticipants, err := h.loadWorkflowParticipants(r.Context(), ticket)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "get ticket workflow failed")
		return
	}
	auditResourceType := "ticket"
	auditLogs, _, err := h.audit.List(r.Context(), repository.AuditListFilter{
		ResourceType: &auditResourceType,
		ResourceID:   &id,
	}, 200, 0)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "get ticket activity logs failed")
		return
	}
	if auditLogs == nil {
		auditLogs = []model.AuditLog{}
	}

	jsonOK(w, map[string]any{
		"ticket":                enrichedTicket,
		"executions":            executions,
		"review_results":        reviewResults,
		"activity_logs":         auditLogs,
		"scopes":                scopes,
		"export_request":        exportDetail,
		"workflow_participants": workflowParticipants,
		"capabilities": map[string]any{
			"can_review":   canReview,
			"can_reject":   canReject,
			"can_withdraw": canWithdraw,
			"can_revoke":   canRevoke,
			"can_request_execution": middleware.HasPermission(r.Context(), "tickets.execute") &&
				ticket.TicketType != model.TicketTypeSQLExport &&
				ticket.TicketType != model.TicketTypeSensitiveQueryAccess &&
				ticket.Status == model.TicketStatusApproved,
			"can_execute": middleware.HasPermission(r.Context(), "tickets.execute") &&
				ticket.TicketType != model.TicketTypeSQLExport &&
				ticket.TicketType != model.TicketTypeSensitiveQueryAccess &&
				ticket.Status == model.TicketStatusPendingExecution,
			"can_download_export": ticket.TicketType == model.TicketTypeSQLExport && ticket.Status == model.TicketStatusApproved && ticket.SubmitterID == userID,
		},
	})
}

func (h *TicketHandler) loadWorkflowParticipants(ctx context.Context, ticket *model.Ticket) (ticketWorkflowParticipants, error) {
	participants := ticketWorkflowParticipants{
		Reviewers: []string{},
		Executors: []string{},
	}
	if ticket == nil || h.users == nil {
		return participants, nil
	}

	reviewerIDs, err := listActiveUserIDsByPermissions(ctx, h.users, reviewPermissionsForTicket(ticket.TicketType))
	if err != nil {
		return participants, err
	}
	participants.Reviewers, err = h.lookupUsernamesByIDs(ctx, reviewerIDs)
	if err != nil {
		return participants, err
	}

	if ticket.TicketType == model.TicketTypeDDL || ticket.TicketType == model.TicketTypeDML || ticket.TicketType == model.TicketTypeRedisCommand {
		executorIDs, err := listActiveUserIDsByPermissions(ctx, h.users, []string{permissionTicketExecute})
		if err != nil {
			return participants, err
		}
		participants.Executors, err = h.lookupUsernamesByIDs(ctx, executorIDs)
		if err != nil {
			return participants, err
		}
	}

	return participants, nil
}

func (h *TicketHandler) buildTicketResponse(ctx context.Context, ticket *model.Ticket) (ticketResponse, error) {
	response := ticketResponse{Ticket: *ticket}

	if ticket.DBConnectionID != nil {
		conn, err := h.dbConns.GetByID(ctx, *ticket.DBConnectionID)
		if err != nil {
			return ticketResponse{}, fmt.Errorf("load db connection %d: %w", *ticket.DBConnectionID, err)
		}
		if conn != nil {
			response.DBConnectionName = &conn.Name
		}
	}

	submitterName, err := h.lookupUsername(ctx, ticket.SubmitterID)
	if err != nil {
		return ticketResponse{}, err
	}
	response.SubmitterName = submitterName

	if ticket.ReviewerID != nil {
		reviewerName, err := h.lookupUsername(ctx, *ticket.ReviewerID)
		if err != nil {
			return ticketResponse{}, err
		}
		response.ReviewerName = &reviewerName
	}

	if ticket.ExecutorID != nil {
		executorName, err := h.lookupUsername(ctx, *ticket.ExecutorID)
		if err != nil {
			return ticketResponse{}, err
		}
		response.ExecutorName = &executorName
	}

	if ticket.RevokedBy != nil {
		revokedByName, err := h.lookupUsername(ctx, *ticket.RevokedBy)
		if err != nil {
			return ticketResponse{}, err
		}
		response.RevokedByName = &revokedByName
	}

	return response, nil
}

func (h *TicketHandler) lookupUsername(ctx context.Context, userID uint64) (string, error) {
	user, err := h.users.GetByID(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("load user %d: %w", userID, err)
	}
	if user == nil {
		return strconv.FormatUint(userID, 10), nil
	}
	return user.Username, nil
}

func (h *TicketHandler) lookupUsernamesByIDs(ctx context.Context, userIDs []uint64) ([]string, error) {
	if len(userIDs) == 0 || h.users == nil {
		return []string{}, nil
	}
	users, err := h.users.ListByIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	usernames := make([]string, 0, len(users))
	for _, user := range users {
		if user.Username == "" {
			continue
		}
		usernames = append(usernames, user.Username)
	}
	return usernames, nil
}

// POST /tickets/{id}/approve
func (h *TicketHandler) Approve(w http.ResponseWriter, r *http.Request) {
	id := parseTicketID(w, r)
	if id == 0 {
		return
	}

	var req struct {
		Comment *string `json:"comment"`
	}
	bindJSON(r, &req)

	ticket, err := h.tickets.GetByID(r.Context(), id)
	if err != nil || ticket == nil {
		jsonErr(w, http.StatusNotFound, "ticket not found")
		return
	}
	userID := middleware.UserIDFromCtx(r.Context())
	allowed, err := h.canRejectTicket(r.Context(), ticket, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket review check failed")
		return
	}
	if !allowed {
		jsonErr(w, http.StatusForbidden, "forbidden")
		return
	}

	targetStatus := model.TicketStatusApproved
	if ticket.TicketType == model.TicketTypeDDL || ticket.TicketType == model.TicketTypeDML || ticket.TicketType == model.TicketTypeRedisCommand {
		targetStatus = model.TicketStatusPendingExecution
	}

	if err := ticketsm.ValidateTransition(ticket.Status, model.TicketStatusApproved); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	var ok bool
	if ticket.TicketType == model.TicketTypeSensitiveQueryAccess && ticket.ApprovedDurationMinutes != nil {
		approvedUntil := time.Now().Add(time.Duration(*ticket.ApprovedDurationMinutes) * time.Minute)
		ok, err = h.tickets.ApproveSensitiveAccess(r.Context(), id, ticket.Status, userID, approvedUntil)
	} else {
		ok, err = h.tickets.UpdateStatus(r.Context(), id,
			ticket.Status, model.TicketStatusApproved,
			&userID, req.Comment, nil,
		)
	}
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	if !ok {
		jsonErr(w, http.StatusConflict, "ticket status changed concurrently")
		return
	}

	var auditDetails any
	if req.Comment != nil && *req.Comment != "" {
		auditDetails = map[string]string{"comment": *req.Comment}
	}
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "ticket_approve",
		ResourceType: "ticket",
		ResourceID:   &id,
		Details:      auditDetails,
		IPAddress:    clientIP(r),
	})

	body := fmt.Sprintf("Ticket %s has been approved", ticket.TicketNo)
	if req.Comment != nil && *req.Comment != "" {
		body += " — " + *req.Comment
	}
	if ticket.TicketType == model.TicketTypeSQLExport {
		if _, err := h.ensureReadyExportRequest(r.Context(), ticket); err != nil {
			jsonErr(w, http.StatusInternalServerError, "create ready export failed")
			return
		}
	}
	if targetStatus == model.TicketStatusPendingExecution {
		ok, err = h.tickets.UpdateStatus(r.Context(), id,
			model.TicketStatusApproved, model.TicketStatusPendingExecution,
			&userID, req.Comment, nil,
		)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "move to pending execution failed")
			return
		}
		if !ok {
			jsonErr(w, http.StatusConflict, "ticket status changed concurrently")
			return
		}
		updated, _ := h.tickets.GetByID(r.Context(), id)
		if updated != nil {
			ticket = updated
		}
		h.dispatchTicketNotification(r.Context(), ticket, ticketEventPendingExecution, &userID, "reviewer 已通過審核，工單已進入待執行隊列。")
	} else {
		h.dispatchTicketNotification(r.Context(), ticket, ticketEventApproved, &userID, body)
	}

	updated, _ := h.tickets.GetByID(r.Context(), id)
	h.publishTicketUpdate(r.Context(), updated, &userID)
	jsonOK(w, updated)
}

// POST /tickets/{id}/reject
func (h *TicketHandler) Reject(w http.ResponseWriter, r *http.Request) {
	id := parseTicketID(w, r)
	if id == 0 {
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	if err := bindJSON(r, &req); err != nil || req.Reason == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "rejection reason is required")
		return
	}

	ticket, err := h.tickets.GetByID(r.Context(), id)
	if err != nil || ticket == nil {
		jsonErr(w, http.StatusNotFound, "ticket not found")
		return
	}
	userID := middleware.UserIDFromCtx(r.Context())
	allowed, err := h.canRejectTicket(r.Context(), ticket, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket review check failed")
		return
	}
	if !allowed {
		jsonErr(w, http.StatusForbidden, "forbidden")
		return
	}

	if err := ticketsm.ValidateTransition(ticket.Status, model.TicketStatusRejected); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	ok, err := h.tickets.UpdateStatus(r.Context(), id,
		ticket.Status, model.TicketStatusRejected,
		&userID, nil, &req.Reason,
	)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	if !ok {
		jsonErr(w, http.StatusConflict, "ticket status changed concurrently")
		return
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "ticket_reject",
		ResourceType: "ticket",
		ResourceID:   &id,
		Details:      map[string]string{"reason": req.Reason},
		IPAddress:    clientIP(r),
	})

	rejectDetail := req.Reason
	if ticket.Status == model.TicketStatusApproved || ticket.Status == model.TicketStatusPendingExecution {
		rejectDetail = "執行階段駁回：" + req.Reason
	}
	h.dispatchTicketNotification(r.Context(), ticket, ticketEventRejected, &userID, rejectDetail)

	updated, _ := h.tickets.GetByID(r.Context(), id)
	h.publishTicketUpdate(r.Context(), updated, &userID)
	jsonOK(w, updated)
}

// POST /tickets/{id}/withdraw
func (h *TicketHandler) Withdraw(w http.ResponseWriter, r *http.Request) {
	id := parseTicketID(w, r)
	if id == 0 {
		return
	}

	ticket, err := h.tickets.GetByID(r.Context(), id)
	if err != nil || ticket == nil {
		jsonErr(w, http.StatusNotFound, "ticket not found")
		return
	}
	userID := middleware.UserIDFromCtx(r.Context())
	allowed, err := h.canWithdrawTicket(r.Context(), ticket, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket withdraw check failed")
		return
	}
	if !allowed {
		jsonErr(w, http.StatusForbidden, "forbidden")
		return
	}

	if err := ticketsm.ValidateTransition(ticket.Status, model.TicketStatusWithdrawn); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	ok, err := h.tickets.UpdateStatus(r.Context(), id, ticket.Status, model.TicketStatusWithdrawn, nil, nil, nil)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "withdraw failed")
		return
	}
	if !ok {
		jsonErr(w, http.StatusConflict, "ticket status changed concurrently")
		return
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "ticket_withdraw",
		ResourceType: "ticket",
		ResourceID:   &id,
		Details:      map[string]string{"status": string(model.TicketStatusWithdrawn)},
		IPAddress:    clientIP(r),
	})

	h.dispatchTicketNotification(r.Context(), ticket, ticketEventWithdrawn, &userID, "submitter 已收回此工單。")

	updated, _ := h.tickets.GetByID(r.Context(), id)
	h.publishTicketUpdate(r.Context(), updated, &userID)
	jsonOK(w, updated)
}

// POST /tickets/{id}/request-execution
func (h *TicketHandler) RequestExecution(w http.ResponseWriter, r *http.Request) {
	id := parseTicketID(w, r)
	if id == 0 {
		return
	}

	ticket, err := h.tickets.GetByID(r.Context(), id)
	if err != nil || ticket == nil {
		jsonErr(w, http.StatusNotFound, "ticket not found")
		return
	}

	if err := ticketsm.ValidateTransition(ticket.Status, model.TicketStatusPendingExecution); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if ticket.TicketType != model.TicketTypeDDL && ticket.TicketType != model.TicketTypeDML && ticket.TicketType != model.TicketTypeRedisCommand {
		jsonErr(w, http.StatusUnprocessableEntity, "only ddl/dml/redis tickets can request execution")
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	ok, err := h.tickets.UpdateStatus(r.Context(), id,
		ticket.Status, model.TicketStatusPendingExecution,
		nil, nil, nil,
	)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	if !ok {
		jsonErr(w, http.StatusConflict, "ticket status changed concurrently")
		return
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "ticket_request_execution",
		ResourceType: "ticket",
		ResourceID:   &id,
		Details:      map[string]string{"status": string(model.TicketStatusPendingExecution)},
		IPAddress:    clientIP(r),
	})

	h.dispatchTicketNotification(r.Context(), ticket, ticketEventPendingExecution, &userID, "工單已進入待執行隊列。")
	updated, _ := h.tickets.GetByID(r.Context(), id)
	h.publishTicketUpdate(r.Context(), updated, &userID)
	jsonOK(w, updated)
}

// POST /tickets/{id}/stop — DBA/Admin only; stops an executing ticket
func (h *TicketHandler) Stop(w http.ResponseWriter, r *http.Request) {
	id := parseTicketID(w, r)
	if id == 0 {
		return
	}

	ok, err := h.tickets.MarkStopped(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "stop failed")
		return
	}
	if !ok {
		jsonErr(w, http.StatusConflict, "ticket is not currently executing")
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "ticket_stop",
		ResourceType: "ticket",
		ResourceID:   &id,
		IPAddress:    clientIP(r),
	})

	if updated, _ := h.tickets.GetByID(r.Context(), id); updated != nil {
		h.publishTicketUpdate(r.Context(), updated, &userID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /tickets/{id}/execute — T9: OCC protected; runs SQL on target DB
// Body (optional): { "scheduled_at": "2026-06-11T10:00:00Z" }
func (h *TicketHandler) Execute(w http.ResponseWriter, r *http.Request) {
	id := parseTicketID(w, r)
	if id == 0 {
		return
	}

	var req struct {
		ScheduledAt *time.Time `json:"scheduled_at"`
	}
	bindJSON(r, &req) // optional body; ignore parse errors

	ticket, err := h.tickets.GetByID(r.Context(), id)
	if err != nil || ticket == nil {
		jsonErr(w, http.StatusNotFound, "ticket not found")
		return
	}

	if ticket.Status != model.TicketStatusPendingExecution {
		jsonErr(w, http.StatusUnprocessableEntity, "ticket is not pending execution")
		return
	}
	if ticket.TicketType != model.TicketTypeDDL && ticket.TicketType != model.TicketTypeDML && ticket.TicketType != model.TicketTypeRedisCommand {
		jsonErr(w, http.StatusUnprocessableEntity, "only ddl/dml/redis tickets can execute")
		return
	}

	if ticket.DBConnectionID == nil {
		jsonErr(w, http.StatusUnprocessableEntity, "ticket has no target db_connection")
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())

	// Scheduled execution: store scheduled_at and return without running
	if req.ScheduledAt != nil && req.ScheduledAt.After(time.Now()) {
		if err := h.tickets.SetScheduled(r.Context(), id, userID, *req.ScheduledAt); err != nil {
			jsonErr(w, http.StatusInternalServerError, "set scheduled time failed")
			return
		}
		h.audit.Log(r.Context(), repository.AuditEntry{
			ActorID:      &userID,
			ActorName:    middleware.UsernameFromCtx(r.Context()),
			ActionType:   "ticket_schedule",
			ResourceType: "ticket",
			ResourceID:   &id,
			Details:      map[string]any{"scheduled_at": req.ScheduledAt},
			IPAddress:    clientIP(r),
		})
		updated, _ := h.tickets.GetByID(r.Context(), id)
		jsonOK(w, updated)
		return
	}

	// T9: OCC — WHERE status='pending_execution', returns 409 if 0 rows affected
	ok, err := h.tickets.StartExecution(r.Context(), id, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "execution start failed")
		return
	}
	if !ok {
		jsonErr(w, http.StatusConflict, "ticket already taken by another executor")
		return
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "ticket_execute_start",
		ResourceType: "ticket",
		ResourceID:   &id,
		IPAddress:    clientIP(r),
	})

	// Run SQL asynchronously so the HTTP response returns immediately.
	// Status is persisted to DB; the client polls GET /tickets/{id} for progress.
	ticket.ExecutorID = &userID
	go h.runTicketExecution(ticket, userID)

	updated, _ := h.tickets.GetByID(r.Context(), id)
	h.publishTicketUpdate(r.Context(), updated, &userID)
	jsonOK(w, updated)
}

// runTicketSQL splits the ticket SQL into statements and executes each one serially
// against the target DB, recording results in ticket_executions.
func (h *TicketHandler) runTicketExecution(ticket *model.Ticket, executorID uint64) {
	if ticket.TicketType == model.TicketTypeRedisCommand {
		h.runTicketRedisCommands(ticket, executorID)
		return
	}
	h.runTicketSQL(ticket, executorID)
}

func (h *TicketHandler) runTicketSQL(ticket *model.Ticket, executorID uint64) {
	ctx := context.Background()
	executorName, err := h.lookupUsername(ctx, executorID)
	if err != nil {
		executorName = ""
	}

	execDB, cleanup, err := h.openTicketSQLDB(ctx, *ticket.DBConnectionID, model.DBCredentialRoleReadwrite, ticket.DatabaseName)
	if err != nil {
		h.finishTicket(ctx, ticket.ID, model.TicketStatusFailed, "cannot connect: "+err.Error())
		return
	}
	defer cleanup()

	finalStatus := model.TicketStatusCompleted
	parsedStatements, _, err := h.parseTicketStatements(ctx, *ticket.DBConnectionID, ticket.SQLContent)
	if err != nil {
		h.finishTicket(ctx, ticket.ID, model.TicketStatusFailed, "parse SQL failed: "+err.Error())
		return
	}

	for _, parsedStatement := range parsedStatements {
		stmt := strings.TrimSpace(parsedStatement.RawSQL)

		// Check if ticket was stopped between statements
		current, err := h.tickets.GetByID(ctx, ticket.ID)
		if err == nil && current != nil && current.Status == model.TicketStatusStopped {
			return
		}

		execRow := &model.TicketExecution{
			TicketID: ticket.ID,
			Seq:      parsedStatement.Seq,
			SQLStmt:  stmt,
		}
		execID, err := h.tickets.CreateExecution(ctx, execRow)
		if err != nil {
			h.finishTicket(ctx, ticket.ID, model.TicketStatusFailed, "record execution failed")
			return
		}
		_ = h.tickets.MarkExecutionRunning(ctx, execID)

		startedAt := time.Now()
		res, execErr := execDB.ExecContext(ctx, stmt)
		durationMs := time.Since(startedAt).Milliseconds()
		var rowsAffected *int64
		var errMsg *string
		if execErr != nil {
			msg := execErr.Error()
			errMsg = &msg
			finalStatus = model.TicketStatusFailed
		} else {
			if value, err := res.RowsAffected(); err == nil {
				rowsAffected = &value
			}
		}
		_ = h.tickets.MarkExecutionDone(ctx, execID, rowsAffected, durationMs, errMsg)

		if execErr != nil {
			break
		}
	}

	h.finishTicket(ctx, ticket.ID, finalStatus, "")

	actionType := "ticket_execute_complete"
	if finalStatus == model.TicketStatusFailed {
		actionType = "ticket_execute_failed"
	}
	h.audit.Log(ctx, repository.AuditEntry{
		ActorID:      &executorID,
		ActorName:    executorName,
		ActionType:   actionType,
		ResourceType: "ticket",
		ResourceID:   &ticket.ID,
		Details:      map[string]string{"status": string(finalStatus)},
	})

	if finalStatus == model.TicketStatusCompleted {
		h.dispatchTicketNotification(ctx, ticket, ticketEventCompleted, &executorID, "工單已執行完成。")
	} else {
		h.dispatchTicketNotification(ctx, ticket, ticketEventExecutionFailed, &executorID, "工單執行失敗，請查看 execution log。")
	}
	if updated, _ := h.tickets.GetByID(ctx, ticket.ID); updated != nil {
		h.publishTicketUpdate(ctx, updated, &executorID)
	}
}

func (h *TicketHandler) finishTicket(ctx context.Context, id uint64, status model.TicketStatus, _ string) {
	_ = h.tickets.MarkCompleted(ctx, id, status)
}

// RunScheduledTicket is the public entry point for the background scheduler.
func (h *TicketHandler) RunScheduledTicket(ticket *model.Ticket, executorID uint64) {
	h.runTicketExecution(ticket, executorID)
}

func (h *TicketHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	id := parseTicketID(w, r)
	if id == 0 {
		return
	}

	ticket, err := h.tickets.GetByID(r.Context(), id)
	if err != nil || ticket == nil {
		jsonErr(w, http.StatusNotFound, "ticket not found")
		return
	}
	if ticket.TicketType != model.TicketTypeSensitiveQueryAccess {
		jsonErr(w, http.StatusUnprocessableEntity, "only sensitive_query_access tickets can be revoked")
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	allowed, err := h.canRevokeSensitiveTicket(r.Context(), ticket, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket revoke check failed")
		return
	}
	if !allowed {
		jsonErr(w, http.StatusForbidden, "forbidden")
		return
	}

	ok, err := h.tickets.RevokeSensitiveAccess(r.Context(), id, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "revoke failed")
		return
	}
	if !ok {
		jsonErr(w, http.StatusConflict, "ticket is not revocable")
		return
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "ticket_revoke",
		ResourceType: "ticket",
		ResourceID:   &id,
		IPAddress:    clientIP(r),
	})

	h.dispatchTicketNotification(r.Context(), ticket, ticketEventRevoked, &userID, "敏感權限已提前撤銷，後續查詢起即失效。")
	updated, _ := h.tickets.GetByID(r.Context(), id)
	h.publishTicketUpdate(r.Context(), updated, &userID)
	jsonOK(w, updated)
}

func (h *TicketHandler) canViewAllTickets(ctx context.Context, userID uint64) (bool, error) {
	if middleware.HasPermission(ctx, permissionTicketReview, permissionTicketExecute, permissionSQLEditorExportReview, permissionSQLEditorSensitiveRev) {
		return true, nil
	}
	return false, nil
}

func (h *TicketHandler) canReviewTicket(ctx context.Context, ticket *model.Ticket, userID uint64) (bool, error) {
	_ = userID
	for _, permissionKey := range reviewPermissionsForTicket(ticket.TicketType) {
		if middleware.HasPermission(ctx, permissionKey) {
			return true, nil
		}
	}
	return false, nil
}

func (h *TicketHandler) canRejectTicket(ctx context.Context, ticket *model.Ticket, userID uint64) (bool, error) {
	allowed, err := h.canReviewTicket(ctx, ticket, userID)
	if err != nil || allowed {
		return allowed, err
	}
	if ticket.TicketType == model.TicketTypeDDL || ticket.TicketType == model.TicketTypeDML || ticket.TicketType == model.TicketTypeRedisCommand {
		if ticket.Status == model.TicketStatusApproved || ticket.Status == model.TicketStatusPendingExecution {
			return middleware.HasPermission(ctx, permissionTicketExecute), nil
		}
	}
	return false, nil
}

func (h *TicketHandler) canWithdrawTicket(ctx context.Context, ticket *model.Ticket, userID uint64) (bool, error) {
	if ticket == nil {
		return false, nil
	}
	if !middleware.HasPermission(ctx, "tickets.apply") {
		return false, nil
	}
	return ticket.SubmitterID == userID && ticket.Status == model.TicketStatusPendingReview, nil
}

func (h *TicketHandler) canRevokeSensitiveTicket(ctx context.Context, ticket *model.Ticket, userID uint64) (bool, error) {
	if ticket.TicketType != model.TicketTypeSensitiveQueryAccess {
		return false, nil
	}
	_ = userID
	return middleware.HasPermission(ctx, permissionSQLEditorSensitiveRev), nil
}

func (h *TicketHandler) validateTicketConnectionType(ctx context.Context, ticketType model.TicketType, connID uint64) error {
	conn, err := h.dbConns.GetByID(ctx, connID)
	if err != nil {
		return fmt.Errorf("load db connection failed")
	}
	if conn == nil {
		return fmt.Errorf("db connection not found")
	}
	switch ticketType {
	case model.TicketTypeRedisCommand:
		if conn.DBType != "redis" {
			return fmt.Errorf("redis_command tickets only support redis connections")
		}
	case model.TicketTypeDDL, model.TicketTypeDML, model.TicketTypeSQLExport, model.TicketTypeSensitiveQueryAccess:
		if conn.DBType == "redis" {
			return fmt.Errorf("%s tickets do not support redis connections", ticketType)
		}
	}
	return nil
}

func (h *TicketHandler) ensureReadyExportRequest(ctx context.Context, ticket *model.Ticket) (*model.ExportRequest, error) {
	if h.exports == nil {
		return nil, fmt.Errorf("export repository is not configured")
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

func (h *TicketHandler) parseTicketStatements(ctx context.Context, dbConnID uint64, sqlContent string) ([]sqlparse.ParsedStatement, sqlparse.Dialect, error) {
	conn, err := h.dbConns.GetByID(ctx, dbConnID)
	if err != nil {
		return nil, sqlparse.DialectGeneric, err
	}
	if conn == nil {
		return nil, sqlparse.DialectGeneric, fmt.Errorf("db connection not found")
	}
	dialect := sqlparse.DialectFromDBType(conn.DBType)
	parsed, err := sqlparse.ParseSQL(dialect, sqlContent)
	if err != nil {
		return nil, dialect, err
	}
	return parsed.Statements, dialect, nil
}

func (h *TicketHandler) listTicketDatabases(ctx context.Context, connID uint64) ([]ticketDatabaseOption, error) {
	queryDB, cleanup, conn, err := h.openTicketSQLDBWithConnection(ctx, connID, model.DBCredentialRoleReadonly, nil)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	switch conn.DBType {
	case "redis":
		items := make([]ticketDatabaseOption, 0, 16)
		for index := 0; index < 16; index++ {
			items = append(items, ticketDatabaseOption{Name: strconv.Itoa(index)})
		}
		return items, nil
	case "postgres", "postgresql":
		rows, err := queryDB.QueryContext(ctx,
			`SELECT datname
			 FROM pg_database
			 WHERE datistemplate = false
			   AND datallowconn = true
			 ORDER BY datname`,
		)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		items := make([]ticketDatabaseOption, 0)
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return nil, err
			}
			if shouldSkipPostgresMetadataDatabase(name) {
				continue
			}
			items = append(items, ticketDatabaseOption{Name: name})
		}
		return items, rows.Err()
	default:
		rows, err := queryDB.QueryContext(ctx,
			`SELECT SCHEMA_NAME
			 FROM information_schema.SCHEMATA
			 WHERE SCHEMA_NAME NOT IN ('information_schema', 'performance_schema', 'mysql', 'sys')
			 ORDER BY SCHEMA_NAME`,
		)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		items := make([]ticketDatabaseOption, 0)
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return nil, err
			}
			items = append(items, ticketDatabaseOption{Name: name})
		}
		return items, rows.Err()
	}
}

func (h *TicketHandler) openTicketSQLDB(
	ctx context.Context,
	connID uint64,
	credentialRole string,
	databaseName *string,
) (*sql.DB, func(), error) {
	db, cleanup, _, err := h.openTicketSQLDBWithConnection(ctx, connID, credentialRole, databaseName)
	return db, cleanup, err
}

func (h *TicketHandler) openTicketSQLDBWithConnection(
	ctx context.Context,
	connID uint64,
	credentialRole string,
	databaseName *string,
) (*sql.DB, func(), *model.DBConnection, error) {
	conn, err := h.dbConns.GetByID(ctx, connID)
	if err != nil {
		return nil, nil, nil, err
	}
	if conn == nil {
		return nil, nil, nil, fmt.Errorf("db connection not found")
	}

	resolvedConn, password, err := h.dbConns.ResolveCredential(conn, credentialRole)
	if err != nil {
		return nil, nil, nil, err
	}
	if databaseName != nil && strings.TrimSpace(*databaseName) != "" {
		targetDatabase := strings.TrimSpace(*databaseName)
		resolvedConn.DatabaseName = &targetDatabase
	}

	driver, dsn := pool.BuildDSN(resolvedConn, password)
	db, err := pool.Open(driver, dsn, pool.ProfileExec)
	if err != nil {
		return nil, nil, nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, nil, nil, err
	}

	return db, func() { _ = db.Close() }, resolvedConn, nil
}

func buildPassThroughReviewItems(statements []sqlparse.ParsedStatement) []ticketReviewItem {
	items := make([]ticketReviewItem, 0)
	for _, stmt := range statements {
		items = append(items, buildValidationReviewItem(stmt.Seq, stmt.RawSQL, validationMethodStaticRule, nil, string(stmt.Kind), inferDDLObjectType(stmt), 0, nil))
	}
	return items
}

func buildSyntaxErrorReviewItems(parseErr error, sqlContent string) []ticketReviewItem {
	message := parseErr.Error()
	if syntaxErr, ok := parseErr.(*sqlparse.SyntaxError); ok {
		seq := syntaxErr.StatementSeq
		if seq <= 0 {
			seq = 1
		}
		return []ticketReviewItem{buildParserErrorReviewItem(seq, strings.TrimSpace(sqlContent), message)}
	}
	return []ticketReviewItem{buildParserErrorReviewItem(1, strings.TrimSpace(sqlContent), message)}
}

func buildTicketKindReviewItems(statements []sqlparse.ParsedStatement, kindErr error) []ticketReviewItem {
	items := make([]ticketReviewItem, 0, len(statements))
	targetSeq := 0
	message := kindErr.Error()
	if typed, ok := kindErr.(*sqlpolicy.ErrTicketStatementKind); ok {
		targetSeq = typed.StatementSeq
	}
	for _, stmt := range statements {
		if stmt.Seq == targetSeq {
			items = append(items, buildValidationReviewItem(stmt.Seq, stmt.RawSQL, validationMethodTicketPolicy, nil, string(stmt.Kind), inferDDLObjectType(stmt), 0, []string{message}))
		}
	}
	return items
}

// splitSQLStatements splits a multi-statement SQL string by semicolons.
func parseTicketID(w http.ResponseWriter, r *http.Request) uint64 {
	s := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil || id == 0 {
		jsonErr(w, http.StatusBadRequest, "invalid ticket id")
		return 0
	}
	return id
}

func hasGroup(groups []model.AuthGroup, targets ...model.AuthGroup) bool {
	for _, g := range groups {
		for _, t := range targets {
			if g == t {
				return true
			}
		}
	}
	return false
}
