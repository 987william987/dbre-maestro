package handler

import (
	"context"
	"database/sql"
	"encoding/json"
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
	queryAccess        *repository.QueryAccessRepo
	exports            *repository.ExportRepo
	audit              *repository.AuditRepo
	settings           *repository.SettingsRepo
	dbConns            *repository.DBConnectionRepo
	users              *repository.UserRepo
	authGroups         *repository.AuthGroupRepo
	masking            *maskingRuntime
	sqlReviewRules     *repository.SQLReviewRuleRepo
	shadowValidationDB *sqlx.DB
	notifRepo          *repository.NotificationRepo
	broker             *realtime.Broker
	lark               *notification.Dispatcher
	notifications      *NotificationRouter
	forbiddenLimiter   requestRateLimiter
	appBaseURL         string
	appEnv             string
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

type workflowTraceUser struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
}

type workflowTraceAuthGroup struct {
	GroupKey string `json:"group_key"`
	Name     string `json:"name"`
}

type ticketWorkflowTrace struct {
	RuleID                *uint64                  `json:"workflow_rule_id,omitempty"`
	RuleName              string                   `json:"workflow_rule_name"`
	ApprovalEnabled       bool                     `json:"approval_enabled"`
	ExecutionMode         string                   `json:"execution_mode"`
	ApprovalUserIDs       []uint64                 `json:"approval_user_ids"`
	ExecutorUserIDs       []uint64                 `json:"executor_user_ids"`
	AdminUserIDs          []uint64                 `json:"admin_user_ids"`
	ApprovalUsers         []workflowTraceUser      `json:"approval_users"`
	ExecutorUsers         []workflowTraceUser      `json:"executor_users"`
	AdminUsers            []workflowTraceUser      `json:"admin_users"`
	MissingApprovalGroups []workflowTraceAuthGroup `json:"missing_approval_groups"`
	MissingExecutorGroups []workflowTraceAuthGroup `json:"missing_executor_groups"`
	ErrorCode             string                   `json:"error_code,omitempty"`
	ErrorMessage          string                   `json:"error_message,omitempty"`
	ResolvedAt            time.Time                `json:"resolved_at"`
	ResolutionTrace       json.RawMessage          `json:"resolution_trace,omitempty"`
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
	ticketEventNeedsAdmin       ticketNotificationEvent = "needs_admin_attention"
)

type ticketExecutionRunOptions struct {
	Automated        bool
	ReviewerID       uint64
	WorkflowRuleID   *uint64
	WorkflowRuleName string
}

type ticketRecipientRole string

const (
	ticketRoleSubmitter        ticketRecipientRole = "submitter"
	ticketRoleReviewer         ticketRecipientRole = "reviewer"
	ticketRoleExecutorPool     ticketRecipientRole = "executor_pool"
	ticketRoleAssignedExecutor ticketRecipientRole = "assigned_executor"
	ticketRoleAdmin            ticketRecipientRole = "admin"
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
		Roles:       []ticketRecipientRole{ticketRoleSubmitter, ticketRoleAssignedExecutor, ticketRoleExecutorPool, ticketRoleAdmin},
		NotifyActor: true,
		Status:      model.TicketStatusFailed,
		NextAction:  "請查看錯誤並重新處理",
	},
	ticketEventRevoked: {
		Title:       "工單已撤銷",
		NotifType:   "ticket_revoked",
		Roles:       []ticketRecipientRole{ticketRoleSubmitter},
		NotifyActor: false,
		Status:      model.TicketStatusStopped,
		NextAction:  "請查看工單詳情",
	},
	ticketEventNeedsAdmin: {
		Title:       "工單需要管理員處理",
		NotifType:   "ticket_needs_admin_attention",
		Roles:       []ticketRecipientRole{ticketRoleAdmin},
		NotifyActor: false,
		Status:      model.TicketStatusNeedsAdminAttention,
		NextAction:  "請修正 Workflow Rules 後重試路由",
	},
}

type TicketHandlerOption func(*TicketHandler)

func WithTicketHandlerAppEnv(appEnv string) TicketHandlerOption {
	return func(h *TicketHandler) {
		h.appEnv = strings.TrimSpace(appEnv)
	}
}

func NewTicketHandler(
	tickets *repository.TicketRepo,
	queryAccess *repository.QueryAccessRepo,
	exports *repository.ExportRepo,
	audit *repository.AuditRepo,
	settings *repository.SettingsRepo,
	dbConns *repository.DBConnectionRepo,
	users *repository.UserRepo,
	authGroups *repository.AuthGroupRepo,
	maskingRules *repository.MaskingRuleRepo,
	whitelist *repository.MaskingWhitelistRepo,
	engine *masking.Engine,
	sqlReviewRules *repository.SQLReviewRuleRepo,
	shadowValidationDB *sqlx.DB,
	lark *notification.Dispatcher,
	notifRepo *repository.NotificationRepo,
	broker *realtime.Broker,
	appBaseURL string,
	opts ...TicketHandlerOption,
) *TicketHandler {
	h := &TicketHandler{
		tickets:            tickets,
		queryAccess:        queryAccess,
		exports:            exports,
		audit:              audit,
		settings:           settings,
		dbConns:            dbConns,
		users:              users,
		authGroups:         authGroups,
		masking:            newMaskingRuntime(users, maskingRules, whitelist, tickets, engine),
		sqlReviewRules:     sqlReviewRules,
		shadowValidationDB: shadowValidationDB,
		notifRepo:          notifRepo,
		broker:             broker,
		lark:               lark,
		notifications:      NewNotificationRouter(notifRepo, audit, broker, lark),
		forbiddenLimiter:   newRequestRateLimiter(20, time.Minute),
		appBaseURL:         strings.TrimRight(appBaseURL, "/"),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func (h *TicketHandler) ticketLink(ticketNo string) string {
	path := fmt.Sprintf("/tickets/%s", ticketNo)
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
	case model.TicketStatusNeedsAdminAttention:
		return "需要管理員處理"
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

func approvalWorkflowForTicket(ticket *model.Ticket) model.ApprovalWorkflowType {
	if ticket == nil {
		return ""
	}
	switch ticket.TicketType {
	case model.TicketTypeDDL:
		return model.ApprovalWorkflowDDL
	case model.TicketTypeDML:
		return model.ApprovalWorkflowDML
	case model.TicketTypeRedisCommand:
		return model.ApprovalWorkflowRedisCommand
	case model.TicketTypeQueryAccess:
		return model.ApprovalWorkflowQueryAccess
	case model.TicketTypeSQLExport:
		if ticket.ContainsSensitive != nil && *ticket.ContainsSensitive {
			return model.ApprovalWorkflowSQLExportSensitive
		}
		return model.ApprovalWorkflowSQLExportNormal
	case model.TicketTypeSensitiveQueryAccess:
		return model.ApprovalWorkflowSensitiveQueryAccess
	default:
		return ""
	}
}

func (h *TicketHandler) workflowReviewerIDs(ctx context.Context, ticket *model.Ticket) ([]uint64, error) {
	resolution, err := h.ticketWorkflowResolution(ctx, ticket)
	if err != nil {
		return nil, err
	}
	if resolution == nil {
		return []uint64{}, nil
	}
	return resolution.ApprovalUserIDs, nil
}

func (h *TicketHandler) ticketWorkflowResolution(ctx context.Context, ticket *model.Ticket) (*model.WorkflowResolution, error) {
	if ticket == nil {
		return nil, nil
	}
	if h.tickets != nil {
		snapshot, err := h.tickets.GetWorkflowSnapshot(ctx, ticket.ID)
		if err != nil {
			return nil, err
		}
		if snapshot != nil {
			return workflowResolutionFromSnapshot(ticket, snapshot), nil
		}
	}
	return resolveTicketWorkflow(ctx, h.settings, h.users, ticket)
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
	parts = append(parts, fmt.Sprintf("工單連結：%s", h.ticketLink(ticket.TicketNo)))
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
	h.notifications.SendTicket(ctx, ticket, NotificationRoute{
		RecipientIDs: recipientIDs,
		ActorID:      actorID,
		NotifyActor:  policy.NotifyActor,
		NotifType:    policy.NotifType,
		Title:        policy.Title,
		Body:         body,
	})
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
			reviewerIDs, err := h.workflowReviewerIDs(ctx, ticket)
			if err != nil {
				return nil, err
			}
			for _, reviewerID := range reviewerIDs {
				addRecipient(reviewerID)
			}
		case ticketRoleExecutorPool:
			resolution, err := h.ticketWorkflowResolution(ctx, ticket)
			if err != nil {
				return nil, err
			}
			for _, executorID := range resolution.ExecutorUserIDs {
				addRecipient(executorID)
			}
		case ticketRoleAssignedExecutor:
			if ticket.ExecutorID != nil {
				addRecipient(*ticket.ExecutorID)
			}
		case ticketRoleAdmin:
			resolution, err := h.ticketWorkflowResolution(ctx, ticket)
			if err != nil {
				return nil, err
			}
			adminIDs := []uint64{}
			if resolution != nil {
				adminIDs = resolution.AdminUserIDs
			}
			if len(adminIDs) == 0 {
				adminIDs, err = workflowAdminUserIDs(ctx, h.users)
				if err != nil {
					return nil, err
				}
			}
			for _, adminID := range adminIDs {
				addRecipient(adminID)
			}
		}
	}

	return recipients, nil
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
	if resolution, err := h.ticketWorkflowResolution(ctx, ticket); err == nil && resolution != nil {
		recipients = append(recipients, resolution.ApprovalUserIDs...)
		recipients = append(recipients, resolution.ExecutorUserIDs...)
		if ticket.Status == model.TicketStatusNeedsAdminAttention {
			recipients = append(recipients, resolution.AdminUserIDs...)
		}
	}
	if ticket.ExecutorID != nil {
		recipients = append(recipients, *ticket.ExecutorID)
	}

	h.broker.PublishToUsers(recipients, "ticket.updated", ticketUpdatedEvent{
		TicketID: ticket.ID,
		Status:   string(ticket.Status),
	})
}

func (h *TicketHandler) publishTicketUpdateByID(ctx context.Context, ticketID uint64, fallback *model.Ticket, actorID *uint64) {
	var current *model.Ticket
	if h.tickets != nil {
		updated, err := h.tickets.GetByID(ctx, ticketID)
		if err != nil {
			slog.Warn("load ticket for realtime update failed", "ticket_id", ticketID, "err", err)
		} else if updated != nil {
			current = updated
		}
	}
	if current == nil {
		current = fallback
	}
	h.publishTicketUpdate(ctx, current, actorID)
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
		Title                   string                      `json:"title"`
		Description             *string                     `json:"description"`
		SQLContent              string                      `json:"sql_content"`
		TicketType              model.TicketType            `json:"ticket_type"`
		DBConnectionID          *uint64                     `json:"db_connection_id"`
		DatabaseName            *string                     `json:"database_name"`
		ApprovedDurationMinutes *int                        `json:"approved_duration_minutes"`
		Scopes                  []model.TicketScope         `json:"scopes"`
		ScopeMode               *model.QueryAccessScopeMode `json:"scope_mode"`
		Items                   []struct {
			DatabaseName string  `json:"database_name"`
			TableName    *string `json:"table_name"`
		} `json:"items"`
		Rules []struct {
			Effect          model.QueryAccessEffect `json:"effect"`
			ConnectionID    uint64                  `json:"connection_id"`
			DatabasePattern string                  `json:"database_pattern"`
			TablePattern    string                  `json:"table_pattern"`
		} `json:"rules"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TicketType != model.TicketTypeQueryAccess && (req.Title == "" || req.SQLContent == "") {
		jsonErr(w, http.StatusUnprocessableEntity, "title and sql_content are required")
		return
	}
	switch req.TicketType {
	case model.TicketTypeSQLExport, model.TicketTypeSensitiveQueryAccess:
		jsonErr(w, http.StatusUnprocessableEntity, "sql_export and sensitive_query_access tickets must be created from SQL Editor")
		return
	}
	if !isGeneralTicketApplyType(req.TicketType) {
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
		if req.ApprovedDurationMinutes == nil {
			jsonErr(w, http.StatusUnprocessableEntity, "approved_duration_minutes is required")
			return
		}
		approvedDurationMinutes, err := normalizeSensitiveAccessDurationMinutes(*req.ApprovedDurationMinutes)
		if err != nil {
			jsonErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		req.ApprovedDurationMinutes = &approvedDurationMinutes
	}
	if req.TicketType == model.TicketTypeQueryAccess {
		if req.ApprovedDurationMinutes == nil || *req.ApprovedDurationMinutes <= 0 {
			jsonErr(w, http.StatusUnprocessableEntity, "query_access requires approved_duration_minutes")
			return
		}
		approvedDurationMinutes, err := normalizeQueryAccessDurationMinutes(*req.ApprovedDurationMinutes)
		if err != nil {
			jsonErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		req.ApprovedDurationMinutes = &approvedDurationMinutes

		queryAccessConnectionIDs := make([]uint64, 0)
		if len(req.Rules) > 0 {
			for _, rule := range req.Rules {
				if rule.ConnectionID == 0 {
					jsonErr(w, http.StatusUnprocessableEntity, "query_access rule requires connection_id")
					return
				}
				if strings.TrimSpace(rule.DatabasePattern) == "" || strings.TrimSpace(rule.TablePattern) == "" {
					jsonErr(w, http.StatusUnprocessableEntity, "query_access rule requires database_pattern and table_pattern")
					return
				}
				if rule.Effect != model.QueryAccessEffectAllow && rule.Effect != model.QueryAccessEffectDeny {
					jsonErr(w, http.StatusUnprocessableEntity, "query_access rule effect must be allow or deny")
					return
				}
				queryAccessConnectionIDs = append(queryAccessConnectionIDs, rule.ConnectionID)
			}
		} else {
			if req.DBConnectionID == nil {
				jsonErr(w, http.StatusUnprocessableEntity, "query_access requires db_connection_id")
				return
			}
			if req.ScopeMode == nil || (*req.ScopeMode != model.QueryAccessScopeModeDatabase && *req.ScopeMode != model.QueryAccessScopeModeTable) {
				jsonErr(w, http.StatusUnprocessableEntity, "query_access requires scope_mode=database or table")
				return
			}
			if len(req.Items) == 0 {
				jsonErr(w, http.StatusUnprocessableEntity, "query_access requires items")
				return
			}
			for _, item := range req.Items {
				if strings.TrimSpace(item.DatabaseName) == "" {
					jsonErr(w, http.StatusUnprocessableEntity, "query_access item database_name is required")
					return
				}
				if *req.ScopeMode == model.QueryAccessScopeModeTable && strings.TrimSpace(nullableStringValue(item.TableName)) == "" {
					jsonErr(w, http.StatusUnprocessableEntity, "query_access table scope requires table_name")
					return
				}
				queryAccessConnectionIDs = append(queryAccessConnectionIDs, *req.DBConnectionID)
			}
		}
		for _, connectionID := range dedupeUint64(queryAccessConnectionIDs) {
			hasAccess, err := userCanAccessConnection(r.Context(), h.users, userID, connectionID)
			if err != nil {
				jsonErr(w, http.StatusInternalServerError, "db scope check failed")
				return
			}
			if !hasAccess {
				jsonErr(w, http.StatusForbidden, "access to this connection is not allowed")
				return
			}
		}
		if req.DBConnectionID == nil && len(queryAccessConnectionIDs) > 0 {
			firstConnectionID := queryAccessConnectionIDs[0]
			req.DBConnectionID = &firstConnectionID
		}
		if strings.TrimSpace(req.Title) == "" {
			req.Title = "Query Access Request"
		}
		if strings.TrimSpace(req.SQLContent) == "" {
			req.SQLContent = "QUERY ACCESS REQUEST"
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
	if req.TicketType == model.TicketTypeQueryAccess && h.queryAccess != nil {
		items := make([]model.QueryAccessTicketItem, 0, len(req.Rules)+len(req.Items))
		if len(req.Rules) > 0 {
			for _, rule := range req.Rules {
				items = append(items, model.QueryAccessTicketItem{
					TicketID:        created.ID,
					ConnectionID:    rule.ConnectionID,
					Effect:          rule.Effect,
					DatabasePattern: strings.TrimSpace(rule.DatabasePattern),
					TablePattern:    strings.TrimSpace(rule.TablePattern),
				})
			}
		} else {
			for _, item := range req.Items {
				tablePattern := "*"
				tableName := optionalTrimmedString(nullableStringValue(item.TableName))
				if tableName != nil {
					tablePattern = *tableName
				}
				items = append(items, model.QueryAccessTicketItem{
					TicketID:        created.ID,
					ConnectionID:    *req.DBConnectionID,
					ScopeMode:       *req.ScopeMode,
					DatabaseName:    strings.TrimSpace(item.DatabaseName),
					TableName:       tableName,
					Effect:          model.QueryAccessEffectAllow,
					DatabasePattern: strings.TrimSpace(item.DatabaseName),
					TablePattern:    tablePattern,
				})
			}
		}
		if err := h.queryAccess.CreateTicketItems(r.Context(), created.ID, items); err != nil {
			_ = h.tickets.Delete(r.Context(), created.ID)
			slog.Error("persist query access ticket items failed", "ticket_id", created.ID, "err", err)
			jsonErr(w, http.StatusInternalServerError, "persist query access ticket items failed")
			return
		}
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

	created, err = h.applyWorkflowAfterCreate(r.Context(), created, &userID, clientIP(r))
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "resolve ticket workflow failed")
		return
	}
	h.publishTicketUpdateByID(r.Context(), created.ID, created, &userID)
	jsonCreated(w, created)
}

func (h *TicketHandler) applyWorkflowAfterCreate(ctx context.Context, ticket *model.Ticket, actorID *uint64, ipAddress string) (*model.Ticket, error) {
	resolution, err := h.ticketWorkflowResolution(ctx, ticket)
	if err != nil {
		return ticket, err
	}
	if err := h.tickets.SaveWorkflowSnapshot(ctx, ticket.ID, resolution); err != nil {
		return ticket, err
	}
	if resolution == nil || resolution.ErrorCode != "" {
		comment := "Workflow resolution failed."
		if resolution != nil && resolution.ErrorMessage != "" {
			comment = resolution.ErrorMessage
		}
		ok, err := h.tickets.UpdateStatus(ctx, ticket.ID, model.TicketStatusPendingReview, model.TicketStatusNeedsAdminAttention, nil, &comment, nil)
		if err != nil {
			return ticket, err
		}
		if ok {
			updated, loadErr := h.tickets.GetByID(ctx, ticket.ID)
			if loadErr != nil {
				return ticket, loadErr
			}
			if updated != nil {
				ticket = updated
			}
		}
		h.audit.Log(ctx, repository.AuditEntry{
			ActorID:      actorID,
			ActionType:   "workflow_resolution_failed",
			ResourceType: "ticket",
			ResourceID:   &ticket.ID,
			Details:      workflowAuditDetails(ticket, resolution),
			IPAddress:    ipAddress,
		})
		h.dispatchTicketNotification(ctx, ticket, ticketEventNeedsAdmin, actorID, comment)
		return ticket, nil
	}
	if !resolution.ApprovalEnabled {
		comment := "Auto-approved because workflow rule approval is disabled."
		ok, err := h.tickets.UpdateStatus(ctx, ticket.ID, model.TicketStatusPendingReview, model.TicketStatusApproved, nil, &comment, nil)
		if err != nil {
			return ticket, err
		}
		if ok {
			updated, loadErr := h.tickets.GetByID(ctx, ticket.ID)
			if loadErr != nil {
				return ticket, loadErr
			}
			if updated != nil {
				ticket = updated
			}
		}
		h.audit.Log(ctx, repository.AuditEntry{
			ActorID:      actorID,
			ActionType:   "ticket_auto_approve",
			ResourceType: "ticket",
			ResourceID:   &ticket.ID,
			Details:      workflowAuditDetails(ticket, resolution),
			IPAddress:    ipAddress,
		})
		if h.shouldAutoExecuteAfterApproval(ticket, resolution) {
			comment := "Workflow Rule 設定為免審批並自動執行，系統已開始執行。"
			if err := h.moveApprovedTicketToPendingExecution(ctx, ticket, nil, &comment); err != nil {
				return ticket, err
			}
			updated, loadErr := h.tickets.GetByID(ctx, ticket.ID)
			if loadErr != nil {
				return ticket, loadErr
			}
			if updated != nil {
				ticket = updated
			}
			if err := h.startWorkflowAutoExecution(ctx, ticket, actorID, resolution, comment); err != nil {
				return ticket, err
			}
			return ticket, nil
		}
		h.dispatchTicketNotification(ctx, ticket, ticketEventApproved, actorID, "Workflow Rule 設定為免審批，工單已自動核准。")
		return ticket, nil
	}
	h.dispatchTicketNotification(ctx, ticket, ticketEventPendingReview, actorID, "提交人已送出工單，等待 reviewer 處理。")
	return ticket, nil
}

func workflowAuditDetails(ticket *model.Ticket, resolution *model.WorkflowResolution) map[string]any {
	details := map[string]any{
		"ticket_type": string(ticket.TicketType),
		"status":      string(ticket.Status),
	}
	if ticket.DBConnectionID != nil {
		details["db_connection_id"] = *ticket.DBConnectionID
	}
	if ticket.ContainsSensitive != nil {
		details["contains_sensitive"] = *ticket.ContainsSensitive
	}
	if resolution != nil {
		if resolution.RuleID != nil {
			details["workflow_rule_id"] = *resolution.RuleID
		}
		details["workflow_rule_name"] = resolution.RuleName
		details["approval_enabled"] = resolution.ApprovalEnabled
		details["execution_mode"] = resolution.ExecutionMode
		details["approval_user_ids"] = resolution.ApprovalUserIDs
		details["executor_user_ids"] = resolution.ExecutorUserIDs
		details["admin_user_ids"] = resolution.AdminUserIDs
		details["missing_approval_groups"] = resolution.MissingApprovalGroups
		details["missing_executor_groups"] = resolution.MissingExecutorGroups
		details["excluded_approval_users"] = resolution.ExcludedApprovalUsers
		details["excluded_executor_users"] = resolution.ExcludedExecutorUsers
		details["error_code"] = resolution.ErrorCode
		details["error_message"] = resolution.ErrorMessage
	}
	return details
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
	if s := strings.TrimSpace(r.URL.Query().Get("ticket_no")); s != "" {
		filter.TicketNo = &s
	}
	if s := strings.TrimSpace(r.URL.Query().Get("title")); s != "" {
		filter.Title = &s
	}
	if s := strings.TrimSpace(r.URL.Query().Get("submitter")); s != "" {
		filter.Submitter = &s
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

	// T5: IDOR — ticket workspace permissions open the page, while workflow
	// rules narrow review/execute visibility to assigned participants.
	fullQueueVisible, err := h.canViewFullTicketQueue(r.Context(), userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket access check failed")
		return
	}
	if fullQueueVisible {
		filter.VisibleToAllTickets = true
	} else {
		filter.VisibleToUserID = &userID
	}

	tickets, _, err := h.tickets.List(r.Context(), filter, 100, 0)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list tickets failed")
		return
	}
	if tickets == nil {
		tickets = []model.Ticket{}
	}

	visibleTickets := make([]model.Ticket, 0, len(tickets))
	for _, ticket := range tickets {
		if fullQueueVisible {
			visibleTickets = append(visibleTickets, ticket)
			continue
		}
		t := ticket
		canView, err := h.canViewTicket(r.Context(), &t, userID)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "ticket access check failed")
			return
		}
		if canView {
			visibleTickets = append(visibleTickets, ticket)
		}
	}
	total := int64(len(visibleTickets))
	if offset > len(visibleTickets) {
		visibleTickets = []model.Ticket{}
	} else {
		end := offset + limit
		if end > len(visibleTickets) {
			end = len(visibleTickets)
		}
		visibleTickets = visibleTickets[offset:end]
	}

	responseTickets := make([]ticketResponse, 0, len(tickets))
	for _, ticket := range visibleTickets {
		enriched, enrichErr := h.buildTicketResponse(r.Context(), &ticket)
		if enrichErr != nil {
			jsonErr(w, http.StatusInternalServerError, "list tickets failed")
			return
		}
		responseTickets = append(responseTickets, enriched)
	}

	jsonOK(w, map[string]any{"tickets": responseTickets, "total": total, "limit": limit, "offset": offset})
}

func (h *TicketHandler) WorkflowDashboardSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := h.tickets.WorkflowDashboardSummary(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load workflow dashboard summary failed")
		return
	}
	jsonOK(w, map[string]any{"summary": summary})
}

// GET /tickets/{id}
func (h *TicketHandler) Get(w http.ResponseWriter, r *http.Request) {
	ticket, resolved := h.resolveTicketRef(w, r)
	if !resolved {
		return
	}
	if ticket == nil {
		jsonErr(w, http.StatusNotFound, "ticket not found")
		return
	}
	id := ticket.ID

	// T5: IDOR — only submitters, ticket-wide roles, or policy reviewers for this
	// workflow can view a ticket.
	userID := middleware.UserIDFromCtx(r.Context())
	canView, err := h.canViewTicket(r.Context(), ticket, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket access check failed")
		return
	}
	if !canView {
		h.forbidTicketAccess(w, r, ticket, "view", "not_visible")
		return
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
	canRevoke, err := h.canRevokeTicket(r.Context(), ticket, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket capability check failed")
		return
	}
	canExecute, err := h.canExecuteTicket(r.Context(), ticket, userID)
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
	var workflowTrace *ticketWorkflowTrace
	if ticket.Status == model.TicketStatusNeedsAdminAttention {
		if allowed, err := h.canViewWorkflowTrace(r.Context(), userID); err != nil {
			jsonErr(w, http.StatusInternalServerError, "ticket workflow trace check failed")
			return
		} else if allowed {
			trace, err := h.loadWorkflowTrace(r.Context(), ticket)
			if err != nil {
				jsonErr(w, http.StatusInternalServerError, "get ticket workflow trace failed")
				return
			}
			workflowTrace = trace
		}
	}

	jsonOK(w, map[string]any{
		"ticket":                    enrichedTicket,
		"executions":                executions,
		"review_results":            reviewResults,
		"activity_logs":             auditLogs,
		"scopes":                    scopes,
		"query_access_items":        h.mustListQueryAccessItems(r.Context(), id),
		"export_request":            exportDetail,
		"workflow_participants":     workflowParticipants,
		"workflow_resolution_trace": workflowTrace,
		"capabilities": map[string]any{
			"can_review":   canReview,
			"can_reject":   canReject,
			"can_withdraw": canWithdraw,
			"can_revoke":   canRevoke,
			"can_execute":  canExecute,
			"can_retry_workflow_resolution": middleware.HasPermission(r.Context(), "settings.write") &&
				ticket.Status == model.TicketStatusNeedsAdminAttention,
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

	resolution, err := h.ticketWorkflowResolution(ctx, ticket)
	if err != nil {
		return participants, err
	}
	if resolution == nil {
		return participants, nil
	}
	participants.Reviewers, err = h.lookupUsernamesByIDs(ctx, resolution.ApprovalUserIDs)
	if err != nil {
		return participants, err
	}
	if isExecutableTicketType(ticket.TicketType) {
		participants.Executors, err = h.lookupUsernamesByIDs(ctx, resolution.ExecutorUserIDs)
		if err != nil {
			return participants, err
		}
	}

	return participants, nil
}

func (h *TicketHandler) canViewWorkflowTrace(ctx context.Context, userID uint64) (bool, error) {
	if h.users == nil || userID == 0 {
		return false, nil
	}
	user, err := h.users.GetByID(ctx, userID)
	if err != nil {
		return false, err
	}
	if user != nil && (user.Username == "admin" || user.IsProtected) {
		return true, nil
	}
	groups, err := h.users.GetAuthGroups(ctx, userID)
	if err != nil {
		return false, err
	}
	for _, group := range groups {
		if group == model.AuthGroupAdmin {
			return true, nil
		}
	}
	return false, nil
}

func (h *TicketHandler) loadWorkflowTrace(ctx context.Context, ticket *model.Ticket) (*ticketWorkflowTrace, error) {
	if h.tickets == nil || ticket == nil {
		return nil, nil
	}
	snapshot, err := h.tickets.GetWorkflowSnapshot(ctx, ticket.ID)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, nil
	}
	trace := &ticketWorkflowTrace{
		RuleID:          snapshot.RuleID,
		RuleName:        snapshot.RuleName,
		ApprovalEnabled: snapshot.ApprovalEnabled,
		ExecutionMode:   normalizeWorkflowExecutionMode(snapshot.ExecutionMode),
		ApprovalUserIDs: append([]uint64{}, snapshot.ApprovalUserIDs...),
		ExecutorUserIDs: append([]uint64{}, snapshot.ExecutorUserIDs...),
		AdminUserIDs:    append([]uint64{}, snapshot.AdminUserIDs...),
		ErrorCode:       snapshot.ErrorCode,
		ErrorMessage:    snapshot.ErrorMessage,
		ResolvedAt:      snapshot.ResolvedAt,
	}
	if strings.TrimSpace(snapshot.ResolutionTrace) != "" {
		trace.ResolutionTrace = json.RawMessage(snapshot.ResolutionTrace)
	}
	trace.ApprovalUsers, err = h.lookupWorkflowTraceUsers(ctx, trace.ApprovalUserIDs)
	if err != nil {
		return nil, err
	}
	trace.ExecutorUsers, err = h.lookupWorkflowTraceUsers(ctx, trace.ExecutorUserIDs)
	if err != nil {
		return nil, err
	}
	trace.AdminUsers, err = h.lookupWorkflowTraceUsers(ctx, trace.AdminUserIDs)
	if err != nil {
		return nil, err
	}
	missingApprovalGroups, missingExecutorGroups := extractMissingWorkflowGroups(snapshot.ResolutionTrace)
	trace.MissingApprovalGroups, err = h.lookupWorkflowTraceAuthGroups(ctx, missingApprovalGroups)
	if err != nil {
		return nil, err
	}
	trace.MissingExecutorGroups, err = h.lookupWorkflowTraceAuthGroups(ctx, missingExecutorGroups)
	if err != nil {
		return nil, err
	}
	return trace, nil
}

func extractMissingWorkflowGroups(raw string) ([]string, []string) {
	if strings.TrimSpace(raw) == "" {
		return []string{}, []string{}
	}
	var payload struct {
		MissingApprovalGroups []string `json:"missing_approval_groups"`
		MissingExecutorGroups []string `json:"missing_executor_groups"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return []string{}, []string{}
	}
	return payload.MissingApprovalGroups, payload.MissingExecutorGroups
}

func (h *TicketHandler) lookupWorkflowTraceUsers(ctx context.Context, userIDs []uint64) ([]workflowTraceUser, error) {
	if len(userIDs) == 0 {
		return []workflowTraceUser{}, nil
	}
	if h.users == nil {
		users := make([]workflowTraceUser, 0, len(userIDs))
		for _, id := range userIDs {
			users = append(users, workflowTraceUser{ID: id, Username: strconv.FormatUint(id, 10)})
		}
		return users, nil
	}
	records, err := h.users.ListByIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	byID := make(map[uint64]string, len(records))
	for _, user := range records {
		byID[user.ID] = user.Username
	}
	users := make([]workflowTraceUser, 0, len(userIDs))
	for _, id := range userIDs {
		username := byID[id]
		if strings.TrimSpace(username) == "" {
			username = strconv.FormatUint(id, 10)
		}
		users = append(users, workflowTraceUser{ID: id, Username: username})
	}
	return users, nil
}

func (h *TicketHandler) lookupWorkflowTraceAuthGroups(ctx context.Context, groupKeys []string) ([]workflowTraceAuthGroup, error) {
	if len(groupKeys) == 0 {
		return []workflowTraceAuthGroup{}, nil
	}
	uniqueKeys := make([]string, 0, len(groupKeys))
	seen := make(map[string]struct{}, len(groupKeys))
	for _, groupKey := range groupKeys {
		normalized := strings.TrimSpace(groupKey)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		uniqueKeys = append(uniqueKeys, normalized)
	}
	if h.authGroups == nil {
		groups := make([]workflowTraceAuthGroup, 0, len(uniqueKeys))
		for _, groupKey := range uniqueKeys {
			groups = append(groups, workflowTraceAuthGroup{GroupKey: groupKey, Name: groupKey})
		}
		return groups, nil
	}
	records, err := h.authGroups.ListByKeys(ctx, uniqueKeys)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]string, len(records))
	for _, group := range records {
		byKey[group.GroupKey] = group.Name
	}
	groups := make([]workflowTraceAuthGroup, 0, len(uniqueKeys))
	for _, groupKey := range uniqueKeys {
		name := byKey[groupKey]
		if strings.TrimSpace(name) == "" {
			name = groupKey
		}
		groups = append(groups, workflowTraceAuthGroup{GroupKey: groupKey, Name: name})
	}
	return groups, nil
}

// POST /tickets/{id}/retry-workflow-resolution
func (h *TicketHandler) RetryWorkflowResolution(w http.ResponseWriter, r *http.Request) {
	ticket, resolved := h.resolveTicketRef(w, r)
	if !resolved {
		return
	}
	if ticket == nil {
		jsonErr(w, http.StatusNotFound, "ticket not found")
		return
	}
	id := ticket.ID
	if !middleware.HasPermission(r.Context(), "settings.write") {
		h.forbidTicketAccess(w, r, ticket, "retry_workflow_resolution", "missing_settings_write")
		return
	}
	if ticket.Status != model.TicketStatusNeedsAdminAttention {
		jsonErr(w, http.StatusUnprocessableEntity, "ticket is not waiting for admin attention")
		return
	}
	resolution, err := resolveTicketWorkflow(r.Context(), h.settings, h.users, ticket)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "resolve workflow failed")
		return
	}
	if err := h.tickets.SaveWorkflowSnapshot(r.Context(), ticket.ID, resolution); err != nil {
		jsonErr(w, http.StatusInternalServerError, "save workflow snapshot failed")
		return
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	if resolution == nil || resolution.ErrorCode != "" {
		h.audit.Log(r.Context(), repository.AuditEntry{
			ActorID:      &actorID,
			ActorName:    middleware.UsernameFromCtx(r.Context()),
			ActionType:   "workflow_resolution_retry_failed",
			ResourceType: "ticket",
			ResourceID:   &id,
			Details:      workflowAuditDetails(ticket, resolution),
			IPAddress:    clientIP(r),
		})
		h.dispatchTicketNotification(r.Context(), ticket, ticketEventNeedsAdmin, &actorID, "Workflow Rule 仍無法解析，工單維持需管理員處理狀態。")
		jsonOK(w, map[string]any{"ticket": ticket, "workflow_resolution": resolution})
		return
	}
	target := model.TicketStatusPendingReview
	if !resolution.ApprovalEnabled {
		target = model.TicketStatusApproved
	}
	comment := "Workflow Rule 已重新解析。"
	ok, err := h.tickets.UpdateStatus(r.Context(), id, model.TicketStatusNeedsAdminAttention, target, nil, &comment, nil)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "update ticket status failed")
		return
	}
	if !ok {
		jsonErr(w, http.StatusConflict, "ticket status changed concurrently")
		return
	}
	updated, _ := h.tickets.GetByID(r.Context(), id)
	if updated == nil {
		updated = ticket
		updated.Status = target
	}
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "workflow_resolution_retry",
		ResourceType: "ticket",
		ResourceID:   &id,
		Details:      workflowAuditDetails(updated, resolution),
		IPAddress:    clientIP(r),
	})
	if target == model.TicketStatusApproved {
		h.dispatchTicketNotification(r.Context(), updated, ticketEventApproved, &actorID, "Workflow Rule 設定為免審批，工單已自動核准。")
	} else {
		h.dispatchTicketNotification(r.Context(), updated, ticketEventPendingReview, &actorID, "Workflow Rule 已重新解析，工單等待 reviewer 處理。")
	}
	h.publishTicketUpdate(r.Context(), updated, &actorID)
	jsonOK(w, map[string]any{"ticket": updated, "workflow_resolution": resolution})
}

func (h *TicketHandler) RetryWorkflowResolutionBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TicketIDs []uint64 `json:"ticket_ids"`
	}
	_ = bindJSON(r, &req)

	tickets := []model.Ticket{}
	if len(req.TicketIDs) > 0 {
		for _, id := range req.TicketIDs {
			ticket, err := h.tickets.GetByID(r.Context(), id)
			if err != nil {
				jsonErr(w, http.StatusInternalServerError, "load ticket failed")
				return
			}
			if ticket != nil && ticket.Status == model.TicketStatusNeedsAdminAttention {
				tickets = append(tickets, *ticket)
			}
		}
	} else {
		status := model.TicketStatusNeedsAdminAttention
		list, _, err := h.tickets.List(r.Context(), repository.TicketListFilter{Status: &status}, 500, 0)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "list needs-admin tickets failed")
			return
		}
		tickets = list
	}

	actorID := middleware.UserIDFromCtx(r.Context())
	results := make([]map[string]any, 0, len(tickets))
	for _, item := range tickets {
		ticket := item
		resolution, err := resolveTicketWorkflow(r.Context(), h.settings, h.users, &ticket)
		if err != nil {
			results = append(results, map[string]any{"ticket_id": ticket.ID, "status": "failed", "error": err.Error()})
			continue
		}
		if err := h.tickets.SaveWorkflowSnapshot(r.Context(), ticket.ID, resolution); err != nil {
			results = append(results, map[string]any{"ticket_id": ticket.ID, "status": "failed", "error": err.Error()})
			continue
		}
		if resolution == nil || resolution.ErrorCode != "" {
			h.dispatchTicketNotification(r.Context(), &ticket, ticketEventNeedsAdmin, &actorID, "Workflow Rule 仍無法解析，工單維持需管理員處理狀態。")
			results = append(results, map[string]any{"ticket_id": ticket.ID, "status": "needs_admin_attention", "workflow_resolution": resolution})
			continue
		}
		target := model.TicketStatusPendingReview
		if !resolution.ApprovalEnabled {
			target = model.TicketStatusApproved
		}
		comment := "Workflow Rule 已批次重新解析。"
		ok, err := h.tickets.UpdateStatus(r.Context(), ticket.ID, model.TicketStatusNeedsAdminAttention, target, nil, &comment, nil)
		if err != nil || !ok {
			errorText := "ticket status changed concurrently"
			if err != nil {
				errorText = err.Error()
			}
			results = append(results, map[string]any{"ticket_id": ticket.ID, "status": "failed", "error": errorText})
			continue
		}
		updated, _ := h.tickets.GetByID(r.Context(), ticket.ID)
		if updated == nil {
			updated = &ticket
			updated.Status = target
		}
		if target == model.TicketStatusApproved {
			h.dispatchTicketNotification(r.Context(), updated, ticketEventApproved, &actorID, "Workflow Rule 設定為免審批，工單已自動核准。")
		} else {
			h.dispatchTicketNotification(r.Context(), updated, ticketEventPendingReview, &actorID, "Workflow Rule 已批次重新解析，工單等待 reviewer 處理。")
		}
		h.publishTicketUpdate(r.Context(), updated, &actorID)
		results = append(results, map[string]any{"ticket_id": ticket.ID, "status": string(target), "workflow_resolution": resolution})
	}
	jsonOK(w, map[string]any{"results": results})
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
	ticket, resolved := h.resolveTicketRef(w, r)
	if !resolved {
		return
	}
	if ticket == nil {
		jsonErr(w, http.StatusNotFound, "ticket not found")
		return
	}
	id := ticket.ID

	var req struct {
		Comment *string `json:"comment"`
	}
	bindJSON(r, &req)

	userID := middleware.UserIDFromCtx(r.Context())
	allowed, err := h.canRejectTicket(r.Context(), ticket, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket review check failed")
		return
	}
	if !allowed {
		h.forbidTicketAccess(w, r, ticket, "approve", "not_reviewer")
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
	} else if ticket.TicketType == model.TicketTypeQueryAccess {
		if h.queryAccess == nil {
			jsonErr(w, http.StatusInternalServerError, "query access repository is not configured")
			return
		}
		var expiresAt *time.Time
		if ticket.ApprovedDurationMinutes != nil && *ticket.ApprovedDurationMinutes > 0 {
			value := time.Now().UTC().Add(time.Duration(*ticket.ApprovedDurationMinutes) * time.Minute)
			expiresAt = &value
		}
		ok, err = h.queryAccess.ApproveTicket(r.Context(), id, ticket.Status, userID, req.Comment, ticket.SubmitterID, expiresAt)
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
		resolution, resolveErr := h.ticketWorkflowResolution(r.Context(), ticket)
		if resolveErr != nil {
			jsonErr(w, http.StatusInternalServerError, "load workflow resolution failed")
			return
		}
		if h.shouldAutoExecuteAfterApproval(ticket, resolution) {
			if err := h.startWorkflowAutoExecution(r.Context(), ticket, &userID, resolution, "Workflow Rule 設定為審批後自動執行，系統已開始執行。"); err != nil {
				jsonErr(w, http.StatusInternalServerError, "workflow auto execution start failed")
				return
			}
		} else {
			h.dispatchTicketNotification(r.Context(), ticket, ticketEventPendingExecution, &userID, "reviewer 已通過審核，工單已進入待執行隊列。")
		}
	} else {
		h.dispatchTicketNotification(r.Context(), ticket, ticketEventApproved, &userID, body)
	}

	updated, err := h.tickets.GetByID(r.Context(), id)
	if err != nil {
		slog.Warn("load ticket after approve failed", "ticket_id", id, "err", err)
		updated = ticket
	} else if updated == nil {
		updated = ticket
	}
	h.publishTicketUpdate(r.Context(), updated, &userID)
	jsonOK(w, updated)
}

func (h *TicketHandler) shouldAutoExecuteAfterApproval(ticket *model.Ticket, resolution *model.WorkflowResolution) bool {
	if ticket == nil || resolution == nil {
		return false
	}
	if h.appEnv == "production" {
		return false
	}
	if resolution.ExecutionMode != workflowExecutionModeAutoApproval {
		return false
	}
	return ticket.TicketType == model.TicketTypeDDL || ticket.TicketType == model.TicketTypeDML
}

func (h *TicketHandler) moveApprovedTicketToPendingExecution(ctx context.Context, ticket *model.Ticket, actorID *uint64, comment *string) error {
	if ticket == nil || h.tickets == nil {
		return nil
	}
	ok, err := h.tickets.UpdateStatus(ctx, ticket.ID, model.TicketStatusApproved, model.TicketStatusPendingExecution, actorID, comment, nil)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("ticket status changed concurrently")
	}
	return nil
}

func (h *TicketHandler) startWorkflowAutoExecution(ctx context.Context, ticket *model.Ticket, actorID *uint64, resolution *model.WorkflowResolution, notification string) error {
	if ticket == nil || h.tickets == nil {
		return nil
	}
	ok, err := h.tickets.StartExecution(ctx, ticket.ID, 0)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("ticket already taken by another executor")
	}
	details := map[string]any{
		"automated":          true,
		"workflow_rule_name": resolution.RuleName,
		"execution_mode":     resolution.ExecutionMode,
	}
	if actorID != nil {
		details["actor_id"] = *actorID
	}
	if resolution.RuleID != nil {
		details["workflow_rule_id"] = *resolution.RuleID
	}
	h.audit.Log(ctx, repository.AuditEntry{
		ActorID:      actorID,
		ActorName:    middleware.UsernameFromCtx(ctx),
		ActionType:   "workflow_auto_execute_start",
		ResourceType: "ticket",
		ResourceID:   &ticket.ID,
		Details:      details,
	})
	h.dispatchTicketNotification(ctx, ticket, ticketEventPendingExecution, actorID, notification)
	go h.runTicketExecutionWithOptions(ticket, 0, ticketExecutionRunOptions{
		Automated:        true,
		ReviewerID:       optionalUint64Value(actorID),
		WorkflowRuleID:   resolution.RuleID,
		WorkflowRuleName: resolution.RuleName,
	})
	return nil
}

func optionalUint64Value(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}

// POST /tickets/{id}/reject
func (h *TicketHandler) Reject(w http.ResponseWriter, r *http.Request) {
	ticket, resolved := h.resolveTicketRef(w, r)
	if !resolved {
		return
	}
	if ticket == nil {
		jsonErr(w, http.StatusNotFound, "ticket not found")
		return
	}
	id := ticket.ID

	var req struct {
		Reason string `json:"reason"`
	}
	if err := bindJSON(r, &req); err != nil || req.Reason == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "rejection reason is required")
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	allowed, err := h.canRejectTicket(r.Context(), ticket, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket review check failed")
		return
	}
	if !allowed {
		h.forbidTicketAccess(w, r, ticket, "reject", "not_reviewer_or_executor")
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

	h.publishTicketUpdateByID(r.Context(), id, ticket, &userID)
	updated, _ := h.tickets.GetByID(r.Context(), id)
	jsonOK(w, updated)
}

// POST /tickets/{id}/withdraw
func (h *TicketHandler) Withdraw(w http.ResponseWriter, r *http.Request) {
	ticket, resolved := h.resolveTicketRef(w, r)
	if !resolved {
		return
	}
	if ticket == nil {
		jsonErr(w, http.StatusNotFound, "ticket not found")
		return
	}
	id := ticket.ID

	var req struct {
		Reason *string `json:"reason"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := bindJSON(r, &req); err != nil {
			jsonErr(w, http.StatusBadRequest, "invalid withdraw request")
			return
		}
	}
	var reason *string
	if req.Reason != nil {
		trimmed := strings.TrimSpace(*req.Reason)
		if trimmed != "" {
			reason = &trimmed
		}
	}

	userID := middleware.UserIDFromCtx(r.Context())
	allowed, err := h.canWithdrawTicket(r.Context(), ticket, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket withdraw check failed")
		return
	}
	if !allowed {
		h.forbidTicketAccess(w, r, ticket, "withdraw", "not_submitter")
		return
	}

	if err := ticketsm.ValidateTransition(ticket.Status, model.TicketStatusWithdrawn); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	ok, err := h.tickets.UpdateStatus(r.Context(), id, ticket.Status, model.TicketStatusWithdrawn, nil, nil, reason)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "withdraw failed")
		return
	}
	if !ok {
		jsonErr(w, http.StatusConflict, "ticket status changed concurrently")
		return
	}

	auditDetails := map[string]string{"status": string(model.TicketStatusWithdrawn)}
	notificationDetail := "工單撤銷。"
	if reason != nil {
		auditDetails["reason"] = *reason
		notificationDetail = *reason
	}
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "ticket_withdraw",
		ResourceType: "ticket",
		ResourceID:   &id,
		Details:      auditDetails,
		IPAddress:    clientIP(r),
	})

	h.dispatchTicketNotification(r.Context(), ticket, ticketEventWithdrawn, &userID, notificationDetail)

	h.publishTicketUpdateByID(r.Context(), id, ticket, &userID)
	updated, _ := h.tickets.GetByID(r.Context(), id)
	jsonOK(w, updated)
}

// POST /tickets/{id}/stop — DBA/Admin only; stops an executing ticket
func (h *TicketHandler) Stop(w http.ResponseWriter, r *http.Request) {
	ticket, resolved := h.resolveTicketRef(w, r)
	if !resolved {
		return
	}
	if ticket == nil {
		jsonErr(w, http.StatusNotFound, "ticket not found")
		return
	}
	id := ticket.ID
	userID := middleware.UserIDFromCtx(r.Context())
	allowed, err := h.canStopTicket(r.Context(), ticket, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket stop check failed")
		return
	}
	if !allowed {
		h.forbidTicketAccess(w, r, ticket, "stop", "not_executor_or_admin")
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

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "ticket_stop",
		ResourceType: "ticket",
		ResourceID:   &id,
		IPAddress:    clientIP(r),
	})

	h.publishTicketUpdateByID(r.Context(), id, nil, &userID)
	w.WriteHeader(http.StatusNoContent)
}

// POST /tickets/{id}/execute — T9: OCC protected; runs SQL on target DB
// Body (optional): { "scheduled_at": "2026-06-11T10:00:00Z" }
func (h *TicketHandler) Execute(w http.ResponseWriter, r *http.Request) {
	ticket, resolved := h.resolveTicketRef(w, r)
	if !resolved {
		return
	}
	if ticket == nil {
		jsonErr(w, http.StatusNotFound, "ticket not found")
		return
	}
	id := ticket.ID

	var req struct {
		ScheduledAt *time.Time `json:"scheduled_at"`
	}
	bindJSON(r, &req) // optional body; ignore parse errors

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
	allowed, err := h.canExecuteTicket(r.Context(), ticket, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket execution check failed")
		return
	}
	if !allowed {
		h.forbidTicketAccess(w, r, ticket, "execute", "not_executor")
		return
	}

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
	go h.runTicketExecutionWithOptions(ticket, userID, ticketExecutionRunOptions{})

	h.publishTicketUpdateByID(r.Context(), id, ticket, &userID)
	updated, _ := h.tickets.GetByID(r.Context(), id)
	jsonOK(w, updated)
}

// runTicketSQL splits the ticket SQL into statements and executes each one serially
// against the target DB, recording results in ticket_executions.
func (h *TicketHandler) runTicketExecution(ticket *model.Ticket, executorID uint64) {
	h.runTicketExecutionWithOptions(ticket, executorID, ticketExecutionRunOptions{})
}

func (h *TicketHandler) runTicketExecutionWithOptions(ticket *model.Ticket, executorID uint64, opts ticketExecutionRunOptions) {
	if ticket.TicketType == model.TicketTypeRedisCommand {
		h.runTicketRedisCommands(ticket, executorID)
		return
	}
	h.runTicketSQL(ticket, executorID, opts)
}

func (h *TicketHandler) runTicketSQL(ticket *model.Ticket, executorID uint64, opts ticketExecutionRunOptions) {
	ctx := context.Background()
	executorName, err := h.lookupUsername(ctx, executorID)
	if err != nil {
		executorName = ""
	}
	if opts.Automated {
		executorName = "workflow automation"
	}

	execDB, cleanup, err := h.openTicketSQLDB(ctx, *ticket.DBConnectionID, model.DBCredentialRoleReadwrite, ticket.DatabaseName)
	if err != nil {
		h.finishTicketExecutionStartFailure(ctx, ticket, executorID, opts, "cannot connect: "+err.Error())
		return
	}
	defer cleanup()

	finalStatus := model.TicketStatusCompleted
	executedStatements := []map[string]any{}
	parsedStatements, _, err := h.parseTicketStatements(ctx, *ticket.DBConnectionID, ticket.SQLContent)
	if err != nil {
		h.finishTicketExecutionStartFailure(ctx, ticket, executorID, opts, "parse SQL failed: "+err.Error())
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
		statementAudit := map[string]any{
			"seq":         parsedStatement.Seq,
			"sql":         stmt,
			"duration_ms": durationMs,
		}
		if rowsAffected != nil {
			statementAudit["rows_affected"] = *rowsAffected
		}
		if errMsg != nil {
			statementAudit["error"] = *errMsg
		}
		executedStatements = append(executedStatements, statementAudit)
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
	if opts.Automated {
		actionType = "workflow_auto_execute_complete"
		if finalStatus != model.TicketStatusCompleted {
			actionType = "workflow_auto_execute_failed"
		}
	}
	details := map[string]any{
		"status":     string(finalStatus),
		"statements": executedStatements,
	}
	if opts.Automated {
		details["automated"] = true
		details["reviewer_id"] = opts.ReviewerID
		details["workflow_rule_name"] = opts.WorkflowRuleName
		if opts.WorkflowRuleID != nil {
			details["workflow_rule_id"] = *opts.WorkflowRuleID
		}
	}
	auditActorID := &executorID
	notificationActorID := &executorID
	if opts.Automated {
		auditActorID = &opts.ReviewerID
		notificationActorID = &opts.ReviewerID
	}
	h.audit.Log(ctx, repository.AuditEntry{
		ActorID:      auditActorID,
		ActorName:    executorName,
		ActionType:   actionType,
		ResourceType: "ticket",
		ResourceID:   &ticket.ID,
		Details:      details,
	})

	if finalStatus == model.TicketStatusCompleted {
		detail := "工單已執行完成。"
		if opts.Automated {
			detail = "Workflow Rule 已自動執行完成。"
		}
		h.dispatchTicketNotification(ctx, ticket, ticketEventCompleted, notificationActorID, detail)
	} else if opts.Automated {
		h.dispatchTicketNotification(ctx, ticket, ticketEventExecutionFailed, notificationActorID, "Workflow Rule 自動執行失敗，請 DBA/Admin 查看 execution log 並重新處理。")
	} else {
		h.dispatchTicketNotification(ctx, ticket, ticketEventExecutionFailed, &executorID, "工單執行失敗，請查看 execution log。")
	}
	h.publishTicketUpdateByID(ctx, ticket.ID, ticket, notificationActorID)
}

func (h *TicketHandler) finishTicket(ctx context.Context, id uint64, status model.TicketStatus, _ string) {
	_ = h.tickets.MarkCompleted(ctx, id, status)
}

func (h *TicketHandler) finishTicketExecutionStartFailure(ctx context.Context, ticket *model.Ticket, executorID uint64, opts ticketExecutionRunOptions, message string) {
	if ticket == nil {
		return
	}
	status := model.TicketStatusFailed
	actionType := "ticket_execute_failed"
	details := map[string]any{
		"status": string(status),
		"error":  message,
	}
	actorID := &executorID
	actorName, _ := h.lookupUsername(ctx, executorID)
	notificationActorID := &executorID
	if opts.Automated {
		actionType = "workflow_auto_execute_failed"
		details["status"] = string(status)
		details["automated"] = true
		details["reviewer_id"] = opts.ReviewerID
		details["workflow_rule_name"] = opts.WorkflowRuleName
		if opts.WorkflowRuleID != nil {
			details["workflow_rule_id"] = *opts.WorkflowRuleID
		}
		actorID = &opts.ReviewerID
		actorName = "workflow automation"
		notificationActorID = &opts.ReviewerID
	}
	h.finishTicket(ctx, ticket.ID, status, message)
	h.audit.Log(ctx, repository.AuditEntry{
		ActorID:      actorID,
		ActorName:    actorName,
		ActionType:   actionType,
		ResourceType: "ticket",
		ResourceID:   &ticket.ID,
		Details:      details,
	})
	if opts.Automated {
		h.dispatchTicketNotification(ctx, ticket, ticketEventExecutionFailed, notificationActorID, "Workflow Rule 自動執行失敗，請 DBA/Admin 查看 execution log 並重新處理。")
	} else {
		h.dispatchTicketNotification(ctx, ticket, ticketEventExecutionFailed, notificationActorID, "工單執行失敗，請查看 execution log。")
	}
	h.publishTicketUpdateByID(ctx, ticket.ID, ticket, notificationActorID)
}

// RunScheduledTicket is the public entry point for the background scheduler.
func (h *TicketHandler) RunScheduledTicket(ticket *model.Ticket, executorID uint64) {
	h.runTicketExecution(ticket, executorID)
}

func (h *TicketHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	ticket, resolved := h.resolveTicketRef(w, r)
	if !resolved {
		return
	}
	if ticket == nil {
		jsonErr(w, http.StatusNotFound, "ticket not found")
		return
	}
	id := ticket.ID
	if ticket.TicketType != model.TicketTypeSensitiveQueryAccess && ticket.TicketType != model.TicketTypeQueryAccess {
		jsonErr(w, http.StatusUnprocessableEntity, "only sensitive_query_access and query_access tickets can be revoked")
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	allowed, err := h.canRevokeTicket(r.Context(), ticket, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket revoke check failed")
		return
	}
	if !allowed {
		h.forbidTicketAccess(w, r, ticket, "revoke", "not_revoker")
		return
	}

	var ok bool
	switch ticket.TicketType {
	case model.TicketTypeSensitiveQueryAccess:
		ok, err = h.tickets.RevokeSensitiveAccess(r.Context(), id, userID)
	case model.TicketTypeQueryAccess:
		if h.queryAccess == nil {
			jsonErr(w, http.StatusInternalServerError, "query access repository is not configured")
			return
		}
		ok, err = h.queryAccess.RevokeTicket(r.Context(), id, userID)
	}
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

	detail := "敏感權限已提前撤銷，後續查詢起即失效。"
	if ticket.TicketType == model.TicketTypeQueryAccess {
		detail = "查詢權限已提前回收，後續查詢起即失效。"
	}
	h.dispatchTicketNotification(r.Context(), ticket, ticketEventRevoked, &userID, detail)
	h.publishTicketUpdateByID(r.Context(), id, ticket, &userID)
	updated, _ := h.tickets.GetByID(r.Context(), id)
	jsonOK(w, updated)
}

func (h *TicketHandler) canViewFullTicketQueue(ctx context.Context, userID uint64) (bool, error) {
	if h.users == nil || userID == 0 {
		return false, nil
	}
	hasAllPermissions, err := h.users.HasAllPermissions(ctx, userID)
	if err != nil {
		return false, err
	}
	if hasAllPermissions {
		return true, nil
	}
	groups, err := h.users.GetAuthGroups(ctx, userID)
	if err != nil {
		return false, err
	}
	for _, group := range groups {
		if group == model.AuthGroupAdmin || group == model.AuthGroupDBA {
			return true, nil
		}
	}
	return false, nil
}

func (h *TicketHandler) canViewTicket(ctx context.Context, ticket *model.Ticket, userID uint64) (bool, error) {
	if ticket == nil {
		return false, nil
	}
	if allowed, err := h.canViewFullTicketQueue(ctx, userID); err != nil || allowed {
		return allowed, err
	}
	if ticket.SubmitterID == userID {
		return true, nil
	}
	if allowed, err := h.canReviewTicket(ctx, ticket, userID); err != nil || allowed {
		return allowed, err
	}
	return h.canExecuteTicket(ctx, ticket, userID)
}

func (h *TicketHandler) canReviewTicket(ctx context.Context, ticket *model.Ticket, userID uint64) (bool, error) {
	if ticket == nil || ticket.SubmitterID == userID {
		return false, nil
	}
	if !h.canReviewWorkflowByPermission(ctx, approvalWorkflowForTicket(ticket)) {
		return false, nil
	}
	resolution, err := h.ticketWorkflowResolution(ctx, ticket)
	if err != nil {
		return false, err
	}
	if resolution == nil || resolution.ErrorCode != "" || !resolution.ApprovalEnabled {
		return false, nil
	}
	if allowed, err := h.canAdminOverrideTicketReview(ctx, userID); err != nil || allowed {
		return allowed, err
	}
	return uint64InSlice(userID, resolution.ApprovalUserIDs), nil
}

func (h *TicketHandler) canAdminOverrideTicketReview(ctx context.Context, userID uint64) (bool, error) {
	if h.users == nil || userID == 0 {
		return false, nil
	}
	hasAllPermissions, err := h.users.HasAllPermissions(ctx, userID)
	if err != nil {
		return false, err
	}
	if hasAllPermissions {
		return true, nil
	}
	groups, err := h.users.GetAuthGroups(ctx, userID)
	if err != nil {
		return false, err
	}
	for _, group := range groups {
		if group == model.AuthGroupAdmin {
			return true, nil
		}
	}
	return false, nil
}

func (h *TicketHandler) canExecuteTicket(ctx context.Context, ticket *model.Ticket, userID uint64) (bool, error) {
	if ticket == nil || !isExecutableTicketType(ticket.TicketType) || ticket.Status != model.TicketStatusPendingExecution {
		return false, nil
	}
	if ticket.SubmitterID == userID {
		return false, nil
	}
	if ticket.ReviewerID != nil && *ticket.ReviewerID == userID {
		return false, nil
	}
	if !middleware.HasPermission(ctx, permissionTicketExecute) {
		return false, nil
	}
	resolution, err := h.ticketWorkflowResolution(ctx, ticket)
	if err != nil {
		return false, err
	}
	if resolution == nil || resolution.ErrorCode != "" {
		return false, nil
	}
	return uint64InSlice(userID, resolution.ExecutorUserIDs), nil
}

func (h *TicketHandler) canStopTicket(ctx context.Context, ticket *model.Ticket, userID uint64) (bool, error) {
	if ticket == nil {
		return false, nil
	}
	if !middleware.HasPermission(ctx, permissionTicketExecute) {
		return false, nil
	}
	if ticket.ExecutorID != nil && *ticket.ExecutorID == userID {
		return true, nil
	}
	if ticket.Status == model.TicketStatusPendingExecution {
		if allowed, err := h.canExecuteTicket(ctx, ticket, userID); err != nil || allowed {
			return allowed, err
		}
	}
	return h.canViewFullTicketQueue(ctx, userID)
}

func uint64InSlice(value uint64, values []uint64) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func (h *TicketHandler) canReviewWorkflowByPermission(ctx context.Context, workflowType model.ApprovalWorkflowType) bool {
	for _, permissionKey := range reviewPermissionsForWorkflow(workflowType) {
		if middleware.HasPermission(ctx, permissionKey) {
			return true
		}
	}
	return false
}

func isExecutableTicketType(ticketType model.TicketType) bool {
	return ticketType == model.TicketTypeDDL || ticketType == model.TicketTypeDML || ticketType == model.TicketTypeRedisCommand
}

func isGeneralTicketApplyType(ticketType model.TicketType) bool {
	return ticketType == model.TicketTypeDDL ||
		ticketType == model.TicketTypeDML ||
		ticketType == model.TicketTypeRedisCommand ||
		ticketType == model.TicketTypeQueryAccess
}

func (h *TicketHandler) canRejectTicket(ctx context.Context, ticket *model.Ticket, userID uint64) (bool, error) {
	allowed, err := h.canReviewTicket(ctx, ticket, userID)
	if err != nil || allowed {
		return allowed, err
	}
	if ticket.TicketType == model.TicketTypeDDL || ticket.TicketType == model.TicketTypeDML || ticket.TicketType == model.TicketTypeRedisCommand {
		if ticket.Status == model.TicketStatusApproved || ticket.Status == model.TicketStatusPendingExecution {
			return h.canExecuteTicket(ctx, ticket, userID)
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

func (h *TicketHandler) canRevokeTicket(ctx context.Context, ticket *model.Ticket, userID uint64) (bool, error) {
	if ticket.TicketType != model.TicketTypeSensitiveQueryAccess && ticket.TicketType != model.TicketTypeQueryAccess {
		return false, nil
	}
	if ticket.Status != model.TicketStatusApproved {
		return false, nil
	}
	_ = userID
	if ticket.TicketType == model.TicketTypeSensitiveQueryAccess {
		return middleware.HasPermission(ctx, permissionSQLEditorSensitiveRev), nil
	}
	return middleware.HasPermission(ctx, permissionTicketReview), nil
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
	case model.TicketTypeDDL, model.TicketTypeDML, model.TicketTypeSQLExport, model.TicketTypeSensitiveQueryAccess, model.TicketTypeQueryAccess:
		if conn.DBType == "redis" {
			return fmt.Errorf("%s tickets do not support redis connections", ticketType)
		}
	}
	return nil
}

func (h *TicketHandler) mustListQueryAccessItems(ctx context.Context, ticketID uint64) []model.QueryAccessTicketItem {
	if h.queryAccess == nil {
		return []model.QueryAccessTicketItem{}
	}
	items, err := h.queryAccess.ListTicketItems(ctx, ticketID)
	if err != nil || items == nil {
		return []model.QueryAccessTicketItem{}
	}
	connectionNames := make(map[uint64]*string)
	for i := range items {
		connectionID := items[i].ConnectionID
		if _, ok := connectionNames[connectionID]; !ok {
			connectionNames[connectionID] = nil
			conn, err := h.dbConns.GetByID(ctx, connectionID)
			if err == nil && conn != nil {
				name := conn.Name
				connectionNames[connectionID] = &name
			}
		}
		items[i].DBConnectionName = connectionNames[connectionID]
	}
	return items
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
	conn, err := h.dbConns.GetByID(ctx, connID)
	if err != nil {
		return nil, err
	}
	if conn == nil {
		return nil, fmt.Errorf("db connection not found")
	}
	if conn.DBType == "redis" {
		return buildRedisTicketDatabaseOptions(), nil
	}

	queryDB, cleanup, conn, err := h.openTicketSQLDBWithConnection(ctx, connID, model.DBCredentialRoleReadonly, nil)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	switch conn.DBType {
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

func buildRedisTicketDatabaseOptions() []ticketDatabaseOption {
	items := make([]ticketDatabaseOption, 0, 16)
	for index := 0; index < 16; index++ {
		items = append(items, ticketDatabaseOption{Name: strconv.Itoa(index)})
	}
	return items
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
		items = append(items, buildValidationReviewItem(stmt.Seq, stmt.RawSQL, validationMethodStaticRule, nil, string(stmt.Kind), inferReviewObjectType(stmt), 0, nil))
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
			items = append(items, buildValidationReviewItem(stmt.Seq, stmt.RawSQL, validationMethodTicketPolicy, nil, string(stmt.Kind), inferReviewObjectType(stmt), 0, []string{message}))
		}
	}
	return items
}

func (h *TicketHandler) resolveTicketRef(w http.ResponseWriter, r *http.Request) (*model.Ticket, bool) {
	ref := strings.TrimSpace(chi.URLParam(r, "id"))
	if ref == "" {
		jsonErr(w, http.StatusBadRequest, "invalid ticket reference")
		return nil, false
	}

	if id, err := strconv.ParseUint(ref, 10, 64); err == nil {
		if id == 0 {
			jsonErr(w, http.StatusBadRequest, "invalid ticket reference")
			return nil, false
		}
		ticket, err := h.tickets.GetByID(r.Context(), id)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "get ticket failed")
			return nil, false
		}
		return ticket, true
	}

	ticket, err := h.tickets.GetByTicketNo(r.Context(), ref)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "get ticket failed")
		return nil, false
	}
	return ticket, true
}

func (h *TicketHandler) forbidTicketAccess(w http.ResponseWriter, r *http.Request, ticket *model.Ticket, action string, reason string) {
	userID := middleware.UserIDFromCtx(r.Context())
	key := fmt.Sprintf("%d:%s", userID, clientIP(r))
	if h.forbiddenLimiter != nil && !h.forbiddenLimiter.Allow(key, time.Now()) {
		h.logForbiddenTicketAccess(r, ticket, action, "rate_limited")
		jsonErr(w, http.StatusTooManyRequests, "too many forbidden ticket access attempts")
		return
	}
	h.logForbiddenTicketAccess(r, ticket, action, reason)
	jsonErr(w, http.StatusForbidden, "forbidden")
}

func (h *TicketHandler) logForbiddenTicketAccess(r *http.Request, ticket *model.Ticket, action string, reason string) {
	if h.audit == nil {
		return
	}
	userID := middleware.UserIDFromCtx(r.Context())
	var resourceID *uint64
	var ticketNo string
	if ticket != nil {
		id := ticket.ID
		resourceID = &id
		ticketNo = ticket.TicketNo
	}
	if err := h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "ticket_forbidden_access",
		ResourceType: "ticket",
		ResourceID:   resourceID,
		Details: map[string]any{
			"action":     action,
			"reason":     reason,
			"ticket_ref": chi.URLParam(r, "id"),
			"ticket_no":  ticketNo,
		},
		IPAddress: clientIP(r),
	}); err != nil {
		slog.Warn("write forbidden ticket access audit failed", "err", err)
	}
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

func dedupeUint64(values []uint64) []uint64 {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[uint64]struct{}, len(values))
	result := make([]uint64, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
