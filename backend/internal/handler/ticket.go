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
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/dbre-maestro/maestro/internal/sqlparse"
	"github.com/dbre-maestro/maestro/internal/sqlpolicy"
	"github.com/dbre-maestro/maestro/internal/sqlreview"
	ticketsm "github.com/dbre-maestro/maestro/internal/ticket"
	"github.com/go-chi/chi/v5"
)

type TicketHandler struct {
	tickets        *repository.TicketRepo
	exports        *repository.ExportRepo
	audit          *repository.AuditRepo
	dbConns        *repository.DBConnectionRepo
	users          *repository.UserRepo
	masking        *maskingRuntime
	sqlReviewRules *repository.SQLReviewRuleRepo
	notifRepo      *repository.NotificationRepo
	lark           *notification.Client // nil = notifications disabled
}

type ticketResponse struct {
	model.Ticket
	DBConnectionName *string `json:"db_connection_name,omitempty"`
	SubmitterName    string  `json:"submitter_name"`
	ReviewerName     *string `json:"reviewer_name,omitempty"`
	ExecutorName     *string `json:"executor_name,omitempty"`
	RevokedByName    *string `json:"revoked_by_name,omitempty"`
}

type ticketReviewItem struct {
	Seq      int     `json:"seq"`
	SQLStmt  string  `json:"sql_stmt"`
	ScanRows int64   `json:"scan_rows"`
	Status   string  `json:"status"`
	Message  *string `json:"message,omitempty"`
}

type ticketDatabaseOption struct {
	Name string `json:"name"`
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
	lark *notification.Client,
	notifRepo *repository.NotificationRepo,
) *TicketHandler {
	return &TicketHandler{
		tickets:        tickets,
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
	case model.TicketTypeDDL, model.TicketTypeDML, model.TicketTypeSQLExport, model.TicketTypeSensitiveQueryAccess:
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
	}
	if (req.TicketType == model.TicketTypeDDL || req.TicketType == model.TicketTypeDML) && strings.TrimSpace(nullableStringValue(req.DatabaseName)) == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "database_name is required")
		return
	}

	// SQL Review: run static + EXPLAIN-based checks if a target DB is specified
	var reviewResults []ticketReviewItem
	if req.DBConnectionID != nil && (req.TicketType == model.TicketTypeDDL || req.TicketType == model.TicketTypeDML) {
		reviewResults = h.runTicketSQLReviewWithType(r.Context(), *req.DBConnectionID, req.TicketType, req.SQLContent, req.DatabaseName)
		issues := make([]string, 0)
		for _, result := range reviewResults {
			if result.Status == "error" && result.Message != nil && strings.TrimSpace(*result.Message) != "" {
				issues = append(issues, fmt.Sprintf("statement %d: %s", result.Seq, *result.Message))
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
				TicketID: created.ID,
				Seq:      result.Seq,
				SQLStmt:  result.SQLStmt,
				ScanRows: result.ScanRows,
				Status:   result.Status,
				Message:  result.Message,
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

	h.notifyTicketReviewers(r.Context(), created, "New Ticket Pending Review", fmt.Sprintf("Ticket %s has been submitted and is awaiting review", created.TicketNo))
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

	responseTickets := make([]ticketResponse, 0, len(tickets))
	for _, ticket := range tickets {
		enriched, enrichErr := h.buildTicketResponse(r.Context(), &ticket)
		if enrichErr != nil {
			jsonErr(w, http.StatusInternalServerError, "list tickets failed")
			return
		}
		responseTickets = append(responseTickets, enriched)
	}

	jsonOK(w, map[string]any{"tickets": responseTickets, "limit": limit, "offset": offset})
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

	jsonOK(w, map[string]any{
		"ticket":         enrichedTicket,
		"executions":     executions,
		"review_results": reviewResults,
		"scopes":         scopes,
		"export_request": exportDetail,
		"capabilities": map[string]any{
			"can_review":            canReview,
			"can_revoke":            canRevoke,
			"can_request_execution": middleware.HasPermission(r.Context(), "tickets.execute") && ticket.TicketType != model.TicketTypeSQLExport && ticket.TicketType != model.TicketTypeSensitiveQueryAccess,
			"can_execute":           middleware.HasPermission(r.Context(), "tickets.execute") && ticket.TicketType != model.TicketTypeSQLExport && ticket.TicketType != model.TicketTypeSensitiveQueryAccess,
			"can_download_export":   ticket.TicketType == model.TicketTypeSQLExport && ticket.Status == model.TicketStatusApproved && ticket.SubmitterID == userID,
		},
	})
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
	h.notifyLark(r.Context(), "Ticket Approved", body)
	h.sendInApp(r.Context(), ticket.SubmitterID, "ticket_approved", "Ticket Approved", body, "ticket", id)

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

	rejectBody := fmt.Sprintf("Ticket %s has been rejected: %s", ticket.TicketNo, req.Reason)
	h.notifyLark(r.Context(), "Ticket Rejected", rejectBody)
	h.sendInApp(r.Context(), ticket.SubmitterID, "ticket_rejected", "Ticket Rejected", rejectBody, "ticket", id)

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
	h.notifyExecutors(r.Context(), ticket, "Ticket Pending Execution", fmt.Sprintf("Ticket %s has entered the execution queue", ticket.TicketNo))
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

		res, execErr := execDB.ExecContext(ctx, stmt)
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
		body := fmt.Sprintf("Ticket %s executed successfully", ticket.TicketNo)
		h.notifyLark(ctx, "Ticket Executed", body)
		h.sendInApp(ctx, ticket.SubmitterID, "ticket_executed", "Ticket Executed", body, "ticket", ticket.ID)
	} else {
		body := fmt.Sprintf("Ticket %s execution failed", ticket.TicketNo)
		h.sendInApp(ctx, ticket.SubmitterID, "ticket_executed", "Ticket Execution Failed", body, "ticket", ticket.ID)
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

	body := fmt.Sprintf("Ticket %s has been revoked early and will be invalidated from the next query onwards", ticket.TicketNo)
	h.sendInApp(r.Context(), ticket.SubmitterID, "ticket_revoked", "Ticket Revoked", body, "ticket", id)
	updated, _ := h.tickets.GetByID(r.Context(), id)
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

func (h *TicketHandler) canRevokeSensitiveTicket(ctx context.Context, ticket *model.Ticket, userID uint64) (bool, error) {
	if ticket.TicketType != model.TicketTypeSensitiveQueryAccess {
		return false, nil
	}
	_ = userID
	return middleware.HasPermission(ctx, permissionSQLEditorSensitiveRev), nil
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
	if h.notifRepo == nil {
		return
	}
	reviewerIDs, err := listActiveUserIDsByPermissions(ctx, h.users, reviewPermissionsForTicket(ticket.TicketType))
	if err != nil {
		return
	}
	for _, reviewerID := range reviewerIDs {
		if reviewerID == ticket.SubmitterID {
			continue
		}
		h.sendInApp(ctx, reviewerID, "ticket_pending_review", title, body, "ticket", ticket.ID)
	}
}

func (h *TicketHandler) notifyExecutors(ctx context.Context, ticket *model.Ticket, title, body string) {
	if h.notifRepo == nil {
		return
	}
	executorIDs, err := listActiveUserIDsByPermissions(ctx, h.users, []string{permissionTicketExecute})
	if err != nil {
		return
	}
	for _, executorID := range executorIDs {
		if executorID == ticket.SubmitterID {
			continue
		}
		h.sendInApp(ctx, executorID, "ticket_pending_execution", title, body, "ticket", ticket.ID)
	}
}

// runTicketSQLReview runs static + EXPLAIN-based checks against each SQL statement.
// Returns a list of blocking issue messages.
func (h *TicketHandler) runTicketSQLReview(ctx context.Context, dbConnID uint64, sqlContent string, databaseName *string) []ticketReviewItem {
	return h.runTicketSQLReviewWithType(ctx, dbConnID, model.TicketTypeDDL, sqlContent, databaseName)
}

// runTicketSQLReview runs static + EXPLAIN-based checks against each SQL statement.
func (h *TicketHandler) runTicketSQLReviewWithType(ctx context.Context, dbConnID uint64, ticketType model.TicketType, sqlContent string, databaseName *string) []ticketReviewItem {
	parsedStatements, dialect, err := h.parseTicketStatements(ctx, dbConnID, sqlContent)
	if err != nil {
		return buildSyntaxErrorReviewItems(err, sqlContent)
	}
	if ticketType == model.TicketTypeDDL || ticketType == model.TicketTypeDML {
		if err := sqlpolicy.CheckTicketStatementKinds(ticketType, parsedStatements); err != nil {
			return buildTicketKindReviewItems(parsedStatements, err)
		}
	}

	rules, err := h.sqlReviewRules.List(ctx)
	if err != nil {
		return buildPassThroughReviewItems(parsedStatements)
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

	results := make([]ticketReviewItem, 0, len(parsedStatements))

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
		for _, stmt := range parsedStatements {
			issues := sqlreview.RunStaticChecksParsed(stmt, ruleMap)
			results = append(results, buildTicketReviewItem(stmt.Seq, stmt.RawSQL, 0, issues))
		}
	} else {
		for _, stmt := range parsedStatements {
			results = append(results, buildTicketReviewItem(stmt.Seq, stmt.RawSQL, 0, nil))
		}
	}

	// EXPLAIN-based checks (need DB connection)
	if !ruleMap["full_table_scan"] && !ruleMap["high_row_count"] {
		return results
	}

	queryDB, cleanup, err := h.openTicketSQLDB(ctx, dbConnID, model.DBCredentialRoleReadonly, databaseName)
	if err != nil {
		return results
	}
	defer cleanup()

	for index, stmt := range parsedStatements {
		if dialect == sqlparse.DialectMySQL && stmt.Kind != sqlparse.StatementKindSelect {
			continue
		}
		if dialect != sqlparse.DialectMySQL && stmt.Kind != sqlparse.StatementKindSelect {
			continue
		}
		issues, err := sqlreview.CheckExplain(ctx, queryDB, stmt.RawSQL, rowThreshold)
		if err != nil {
			continue
		}
		maxRows := int64(0)
		explainMessages := make([]string, 0)
		for _, issue := range issues {
			if issue.Rows > maxRows {
				maxRows = issue.Rows
			}
			if ruleMap[issue.Kind] {
				explainMessages = append(explainMessages, issue.Msg)
			}
		}
		if len(explainMessages) == 0 && results[index].ScanRows < maxRows {
			results[index].ScanRows = maxRows
			continue
		}
		if len(explainMessages) > 0 {
			results[index] = buildTicketReviewItem(stmt.Seq, stmt.RawSQL, maxRows, explainMessages)
		}
	}
	return results
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
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, nil, nil, err
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(2 * time.Minute)

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
		items = append(items, buildTicketReviewItem(stmt.Seq, stmt.RawSQL, 0, nil))
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
		return []ticketReviewItem{buildTicketReviewItem(seq, strings.TrimSpace(sqlContent), 0, []string{message})}
	}
	return []ticketReviewItem{buildTicketReviewItem(1, strings.TrimSpace(sqlContent), 0, []string{message})}
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
			items = append(items, buildTicketReviewItem(stmt.Seq, stmt.RawSQL, 0, []string{message}))
			continue
		}
		items = append(items, buildTicketReviewItem(stmt.Seq, stmt.RawSQL, 0, nil))
	}
	return items
}

func buildTicketReviewItem(seq int, stmt string, scanRows int64, issues []string) ticketReviewItem {
	if len(issues) == 0 {
		return ticketReviewItem{
			Seq:      seq,
			SQLStmt:  stmt,
			ScanRows: scanRows,
			Status:   "pass",
		}
	}
	message := strings.Join(issues, "; ")
	return ticketReviewItem{
		Seq:      seq,
		SQLStmt:  stmt,
		ScanRows: scanRows,
		Status:   "error",
		Message:  &message,
	}
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
