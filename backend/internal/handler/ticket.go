package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/masking"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/notification"
	"github.com/dbre-maestro/maestro/internal/pool"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/dbre-maestro/maestro/internal/sqlreview"
	ticketsm "github.com/dbre-maestro/maestro/internal/ticket"
	"github.com/go-chi/chi/v5"
)

type TicketHandler struct {
	tickets        *repository.TicketRepo
	settings       *repository.SettingsRepo
	exports        *repository.ExportRepo
	audit          *repository.AuditRepo
	dbConns        *repository.DBConnectionRepo
	users          *repository.UserRepo
	masking        *maskingRuntime
	sqlReviewRules *repository.SQLReviewRuleRepo
	notifRepo      *repository.NotificationRepo
	lark           *notification.Client // nil = notifications disabled
}

func NewTicketHandler(
	tickets *repository.TicketRepo,
	settings *repository.SettingsRepo,
	exports *repository.ExportRepo,
	audit *repository.AuditRepo,
	dbConns *repository.DBConnectionRepo,
	users *repository.UserRepo,
	maskingRules *repository.MaskingRuleRepo,
	whitelist *repository.MaskingWhitelistRepo,
	engine *masking.Engine,
	sqlReviewRules *repository.SQLReviewRuleRepo,
	lark *notification.Client,
	notifRepo *repository.NotificationRepo,
) *TicketHandler {
	return &TicketHandler{
		tickets:        tickets,
		settings:       settings,
		exports:        exports,
		audit:          audit,
		dbConns:        dbConns,
		users:          users,
		masking:        newMaskingRuntime(users, maskingRules, whitelist, tickets, engine),
		sqlReviewRules: sqlReviewRules,
		notifRepo:      notifRepo,
		lark:           lark,
	}
}

// notifyLark sends a Lark notification and logs failures to audit_logs.
// No-op if lark client is not configured.
func (h *TicketHandler) notifyLark(ctx context.Context, title, body string) {
	if h.lark == nil {
		return
	}
	result := h.lark.Send(ctx, notification.Message{Title: title, Body: body})
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

func (h *TicketHandler) sendInApp(ctx context.Context, userID uint64, notifType, title, body, resType string, resID uint64) {
	if h.notifRepo == nil {
		return
	}
	_ = h.notifRepo.Create(ctx, userID, notifType, title, body, &resType, &resID)
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
	case model.TicketTypeDDL, model.TicketTypeDML, model.TicketTypeSQLExport, model.TicketTypeSensitiveQueryAccess:
	default:
		jsonErr(w, http.StatusUnprocessableEntity, "invalid ticket_type")
		return
	}

	// SQL Review: run static + EXPLAIN-based checks if a target DB is specified
	if req.DBConnectionID != nil && (req.TicketType == model.TicketTypeDDL || req.TicketType == model.TicketTypeDML) {
		if issues := h.runTicketSQLReview(r.Context(), *req.DBConnectionID, req.SQLContent); len(issues) > 0 {
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
		SubmitterID:             userID,
		ApprovedDurationMinutes: req.ApprovedDurationMinutes,
	}

	created, err := h.tickets.CreateWithScopes(r.Context(), t, req.Scopes)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create ticket failed")
		return
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "ticket_submit",
		ResourceType: "ticket",
		ResourceID:   &created.ID,
		IPAddress:    clientIP(r),
	})

	h.notifyTicketReviewers(r.Context(), created, "新的工單待審核", fmt.Sprintf("工單 %s 已提交，等待審核", created.TicketNo))
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

	var statusFilter *model.TicketStatus
	if s := r.URL.Query().Get("status"); s != "" {
		ts := model.TicketStatus(s)
		statusFilter = &ts
	}

	// T5: IDOR — only reviewers/executors can see the full queue.
	var submitterFilter *uint64
	canViewAll, err := h.canViewAllTickets(r.Context(), userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket access check failed")
		return
	}
	if !canViewAll {
		submitterFilter = &userID
	}

	tickets, err := h.tickets.List(r.Context(), submitterFilter, statusFilter, limit, offset)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list tickets failed")
		return
	}
	if tickets == nil {
		tickets = []model.Ticket{}
	}

	jsonOK(w, map[string]any{"tickets": tickets, "limit": limit, "offset": offset})
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
	canReview, err := h.canReviewTicket(r.Context(), ticket, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket capability check failed")
		return
	}
	canRevoke, err := h.canRevokeSensitiveTicket(r.Context(), ticket, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket capability check failed")
		return
	}

	jsonOK(w, map[string]any{
		"ticket":     ticket,
		"executions": executions,
		"scopes":     scopes,
		"capabilities": map[string]any{
			"can_review":            canReview,
			"can_revoke":            canRevoke,
			"can_request_execution": middleware.HasPermission(r.Context(), "tickets.execute") && ticket.TicketType != model.TicketTypeSQLExport && ticket.TicketType != model.TicketTypeSensitiveQueryAccess,
			"can_execute":           middleware.HasPermission(r.Context(), "tickets.execute") && ticket.TicketType != model.TicketTypeSQLExport && ticket.TicketType != model.TicketTypeSensitiveQueryAccess,
			"can_download_export":   ticket.TicketType == model.TicketTypeSQLExport && ticket.Status == model.TicketStatusApproved && ticket.SubmitterID == userID,
		},
	})
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
	allowed, err := h.canReviewTicket(r.Context(), ticket, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "ticket review check failed")
		return
	}
	if !allowed {
		jsonErr(w, http.StatusForbidden, "forbidden")
		return
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

	body := fmt.Sprintf("工單 %s 已審核通過", ticket.TicketNo)
	if req.Comment != nil && *req.Comment != "" {
		body += " — " + *req.Comment
	}
	if ticket.TicketType == model.TicketTypeSQLExport {
		if _, err := h.ensureReadyExportRequest(r.Context(), ticket); err != nil {
			jsonErr(w, http.StatusInternalServerError, "create ready export failed")
			return
		}
	}
	h.notifyLark(r.Context(), "工單審核通過", body)
	h.sendInApp(r.Context(), ticket.SubmitterID, "ticket_approved", "工單審核通過", body, "ticket", id)

	updated, _ := h.tickets.GetByID(r.Context(), id)
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
	allowed, err := h.canReviewTicket(r.Context(), ticket, userID)
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

	rejectBody := fmt.Sprintf("工單 %s 已拒絕：%s", ticket.TicketNo, req.Reason)
	h.notifyLark(r.Context(), "工單已拒絕", rejectBody)
	h.sendInApp(r.Context(), ticket.SubmitterID, "ticket_rejected", "工單已拒絕", rejectBody, "ticket", id)

	updated, _ := h.tickets.GetByID(r.Context(), id)
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
	if ticket.TicketType != model.TicketTypeDDL && ticket.TicketType != model.TicketTypeDML {
		jsonErr(w, http.StatusUnprocessableEntity, "only ddl/dml tickets can request execution")
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

	_ = userID
	updated, _ := h.tickets.GetByID(r.Context(), id)
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
	if ticket.TicketType != model.TicketTypeDDL && ticket.TicketType != model.TicketTypeDML {
		jsonErr(w, http.StatusUnprocessableEntity, "only ddl/dml tickets can execute")
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
	go h.runTicketSQL(ticket, userID)

	updated, _ := h.tickets.GetByID(r.Context(), id)
	jsonOK(w, updated)
}

// runTicketSQL splits the ticket SQL into statements and executes each one serially
// against the target DB, recording results in ticket_executions.
func (h *TicketHandler) runTicketSQL(ticket *model.Ticket, executorID uint64) {
	ctx := context.Background()

	conn, err := h.dbConns.GetByID(ctx, *ticket.DBConnectionID)
	if err != nil || conn == nil {
		h.finishTicket(ctx, ticket.ID, model.TicketStatusFailed, "db connection not found")
		return
	}
	password, err := h.dbConns.DecryptPassword(conn)
	if err != nil {
		h.finishTicket(ctx, ticket.ID, model.TicketStatusFailed, "decrypt password failed")
		return
	}

	driver, dsn := pool.BuildDSN(conn, password)
	pools, err := pool.Global().GetOrCreate(conn.ID, driver, dsn)
	if err != nil {
		h.finishTicket(ctx, ticket.ID, model.TicketStatusFailed, "cannot connect: "+err.Error())
		return
	}

	stmts := splitSQLStatements(ticket.SQLContent)
	finalStatus := model.TicketStatusCompleted

	for seq, stmt := range stmts {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}

		// Check if ticket was stopped between statements
		current, err := h.tickets.GetByID(ctx, ticket.ID)
		if err == nil && current != nil && current.Status == model.TicketStatusStopped {
			return
		}

		execRow := &model.TicketExecution{
			TicketID: ticket.ID,
			Seq:      seq + 1,
			SQLStmt:  stmt,
		}
		execID, err := h.tickets.CreateExecution(ctx, execRow)
		if err != nil {
			h.finishTicket(ctx, ticket.ID, model.TicketStatusFailed, "record execution failed")
			return
		}
		_ = h.tickets.MarkExecutionRunning(ctx, execID)

		res, execErr := pools.ExecPool.ExecContext(ctx, stmt)
		var rowsAffected int64
		var errMsg *string
		if execErr != nil {
			msg := execErr.Error()
			errMsg = &msg
			finalStatus = model.TicketStatusFailed
		} else {
			rowsAffected, _ = res.RowsAffected()
		}
		_ = h.tickets.MarkExecutionDone(ctx, execID, rowsAffected, errMsg)

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
		ActorName:    "",
		ActionType:   actionType,
		ResourceType: "ticket",
		ResourceID:   &ticket.ID,
	})

	if finalStatus == model.TicketStatusCompleted {
		body := fmt.Sprintf("工單 %s 已成功執行", ticket.TicketNo)
		h.notifyLark(ctx, "工單執行完成", body)
		h.sendInApp(ctx, ticket.SubmitterID, "ticket_executed", "工單執行完成", body, "ticket", ticket.ID)
	} else {
		body := fmt.Sprintf("工單 %s 執行失敗", ticket.TicketNo)
		h.sendInApp(ctx, ticket.SubmitterID, "ticket_executed", "工單執行失敗", body, "ticket", ticket.ID)
	}
}

func (h *TicketHandler) finishTicket(ctx context.Context, id uint64, status model.TicketStatus, _ string) {
	_ = h.tickets.MarkCompleted(ctx, id, status)
}

// RunScheduledTicket is the public entry point for the background scheduler.
func (h *TicketHandler) RunScheduledTicket(ticket *model.Ticket, executorID uint64) {
	h.runTicketSQL(ticket, executorID)
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

	body := fmt.Sprintf("工單 %s 已提前撤銷，下一次查詢起立即失效", ticket.TicketNo)
	h.sendInApp(r.Context(), ticket.SubmitterID, "ticket_revoked", "工單已撤銷", body, "ticket", id)
	updated, _ := h.tickets.GetByID(r.Context(), id)
	jsonOK(w, updated)
}

func (h *TicketHandler) canViewAllTickets(ctx context.Context, userID uint64) (bool, error) {
	if middleware.HasPermission(ctx, "tickets.review", "tickets.execute") {
		return true, nil
	}
	if h.settings == nil {
		return false, nil
	}
	if ok, err := h.settings.IsSensitiveExportReviewer(ctx, userID); err != nil {
		return false, err
	} else if ok {
		return true, nil
	}
	if ok, err := h.settings.IsSensitiveQueryAccessReviewer(ctx, userID); err != nil {
		return false, err
	} else if ok {
		return true, nil
	}
	return false, nil
}

func (h *TicketHandler) canReviewTicket(ctx context.Context, ticket *model.Ticket, userID uint64) (bool, error) {
	switch ticket.TicketType {
	case model.TicketTypeSQLExport:
		if middleware.HasPermission(ctx, "settings.write") {
			return true, nil
		}
		if h.settings == nil {
			return false, nil
		}
		return h.settings.IsSensitiveExportReviewer(ctx, userID)
	case model.TicketTypeSensitiveQueryAccess:
		if middleware.HasPermission(ctx, "settings.write") {
			return true, nil
		}
		if h.settings == nil {
			return false, nil
		}
		return h.settings.IsSensitiveQueryAccessReviewer(ctx, userID)
	default:
		return middleware.HasPermission(ctx, "tickets.review"), nil
	}
}

func (h *TicketHandler) canRevokeSensitiveTicket(ctx context.Context, ticket *model.Ticket, userID uint64) (bool, error) {
	if ticket.TicketType != model.TicketTypeSensitiveQueryAccess {
		return false, nil
	}
	if middleware.HasPermission(ctx, "settings.write") {
		return true, nil
	}
	if h.settings == nil {
		return false, nil
	}
	return h.settings.IsSensitiveQueryAccessReviewer(ctx, userID)
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

func (h *TicketHandler) notifyTicketReviewers(ctx context.Context, ticket *model.Ticket, title, body string) {
	if h.settings == nil || h.notifRepo == nil {
		return
	}
	var reviewerIDs []uint64
	switch ticket.TicketType {
	case model.TicketTypeSQLExport:
		settings, err := h.settings.Get(ctx)
		if err != nil {
			return
		}
		reviewerIDs = settings.SensitiveExportReviewerUserIDs
	case model.TicketTypeSensitiveQueryAccess:
		settings, err := h.settings.Get(ctx)
		if err != nil {
			return
		}
		reviewerIDs = settings.SensitiveQueryAccessReviewerUserIDs
	default:
		return
	}
	for _, reviewerID := range reviewerIDs {
		h.sendInApp(ctx, reviewerID, "ticket_pending_review", title, body, "ticket", ticket.ID)
	}
}

// runTicketSQLReview runs static + EXPLAIN-based checks against each SQL statement.
// Returns a list of blocking issue messages.
func (h *TicketHandler) runTicketSQLReview(ctx context.Context, dbConnID uint64, sqlContent string) []string {
	rules, err := h.sqlReviewRules.List(ctx)
	if err != nil {
		return nil // fail open on meta DB error
	}

	ruleMap := make(map[string]bool, len(rules))
	var rowThreshold int64 = sqlreview.DefaultRowThreshold
	for _, r := range rules {
		if r.Enabled {
			ruleMap[r.RuleName] = true
			if r.RuleName == "high_row_count" && r.Threshold != nil {
				rowThreshold = *r.Threshold
			}
		}
	}

	var allIssues []string

	// Static checks (no DB connection required)
	staticNames := []string{"dml_no_where", "ddl_no_comment", "require_utf8mb4"}
	hasStatic := false
	for _, name := range staticNames {
		if ruleMap[name] {
			hasStatic = true
			break
		}
	}
	if hasStatic {
		for _, stmt := range splitSQLStatements(sqlContent) {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			allIssues = append(allIssues, sqlreview.RunStaticChecks(stmt, ruleMap)...)
		}
	}

	// EXPLAIN-based checks (need DB connection)
	if !ruleMap["full_table_scan"] && !ruleMap["high_row_count"] {
		return allIssues
	}

	conn, err := h.dbConns.GetByID(ctx, dbConnID)
	if err != nil || conn == nil {
		return allIssues // fail open: can't connect, let static issues through
	}
	password, err := h.dbConns.DecryptPassword(conn)
	if err != nil {
		return allIssues
	}
	driver, dsn := pool.BuildDSN(conn, password)
	pools, err := pool.Global().GetOrCreate(conn.ID, driver, dsn)
	if err != nil {
		return allIssues // fail open
	}

	for _, stmt := range splitSQLStatements(sqlContent) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		issues, err := sqlreview.CheckExplain(ctx, pools.QueryPool, stmt, rowThreshold)
		if err != nil {
			continue // EXPLAIN not supported for this statement type (DDL etc.), skip
		}
		for _, issue := range issues {
			if ruleMap[issue.Kind] {
				allIssues = append(allIssues, issue.Msg)
			}
		}
	}
	return allIssues
}

// splitSQLStatements splits a multi-statement SQL string by semicolons.
func splitSQLStatements(sql string) []string {
	var stmts []string
	var cur strings.Builder
	depth := 0
	for _, ch := range sql {
		switch ch {
		case '(':
			depth++
			cur.WriteRune(ch)
		case ')':
			depth--
			cur.WriteRune(ch)
		case ';':
			if depth == 0 {
				if s := strings.TrimSpace(cur.String()); s != "" {
					stmts = append(stmts, s)
				}
				cur.Reset()
			} else {
				cur.WriteRune(ch)
			}
		default:
			cur.WriteRune(ch)
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		stmts = append(stmts, s)
	}
	return stmts
}

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
