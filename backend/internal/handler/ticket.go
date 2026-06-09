package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/notification"
	"github.com/dbre-maestro/maestro/internal/repository"
	ticketsm "github.com/dbre-maestro/maestro/internal/ticket"
	"github.com/go-chi/chi/v5"
)

type TicketHandler struct {
	tickets *repository.TicketRepo
	audit   *repository.AuditRepo
	lark    *notification.Client // nil = notifications disabled
}

func NewTicketHandler(tickets *repository.TicketRepo, audit *repository.AuditRepo, lark *notification.Client) *TicketHandler {
	return &TicketHandler{tickets: tickets, audit: audit, lark: lark}
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

// POST /tickets
func (h *TicketHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())

	var req struct {
		Title          string           `json:"title"`
		Description    *string          `json:"description"`
		SQLContent     string           `json:"sql_content"`
		TicketType     model.TicketType `json:"ticket_type"`
		DBConnectionID *uint64          `json:"db_connection_id"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" || req.SQLContent == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "title and sql_content are required")
		return
	}
	if req.TicketType != model.TicketTypeDDL && req.TicketType != model.TicketTypeDML {
		jsonErr(w, http.StatusUnprocessableEntity, "ticket_type must be ddl or dml")
		return
	}

	t := &model.Ticket{
		Title:          req.Title,
		Description:    req.Description,
		SQLContent:     req.SQLContent,
		TicketType:     req.TicketType,
		DBConnectionID: req.DBConnectionID,
		SubmitterID:    userID,
	}

	created, err := h.tickets.Create(r.Context(), t)
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

	jsonCreated(w, created)
}

// GET /tickets
func (h *TicketHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())
	groups, _ := r.Context().Value(middleware.CtxAuthGroups).([]model.AuthGroup)

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

	// T5: IDOR — developers only see their own tickets
	var submitterFilter *uint64
	if !hasGroup(groups, model.AuthGroupDBA, model.AuthGroupAdmin) {
		submitterFilter = &userID
	}

	tickets, err := h.tickets.List(r.Context(), submitterFilter, statusFilter, limit, offset)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list tickets failed")
		return
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

	// T5: IDOR — developers can only view their own tickets
	userID := middleware.UserIDFromCtx(r.Context())
	groups, _ := r.Context().Value(middleware.CtxAuthGroups).([]model.AuthGroup)
	if !hasGroup(groups, model.AuthGroupDBA, model.AuthGroupAdmin, model.AuthGroupReviewer) {
		if ticket.SubmitterID != userID {
			jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
	}

	jsonOK(w, ticket)
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

	if err := ticketsm.ValidateTransition(ticket.Status, model.TicketStatusApproved); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	ok, err := h.tickets.UpdateStatus(r.Context(), id,
		ticket.Status, model.TicketStatusApproved,
		&userID, req.Comment, nil,
	)
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
	h.notifyLark(r.Context(), "工單審核通過", body)

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

	if err := ticketsm.ValidateTransition(ticket.Status, model.TicketStatusRejected); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
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

	h.notifyLark(r.Context(), "工單已拒絕",
		fmt.Sprintf("工單 %s 已拒絕：%s", ticket.TicketNo, req.Reason))

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

// POST /tickets/{id}/execute — T9: OCC protected
func (h *TicketHandler) Execute(w http.ResponseWriter, r *http.Request) {
	id := parseTicketID(w, r)
	if id == 0 {
		return
	}

	ticket, err := h.tickets.GetByID(r.Context(), id)
	if err != nil || ticket == nil {
		jsonErr(w, http.StatusNotFound, "ticket not found")
		return
	}

	if ticket.Status != model.TicketStatusPendingExecution {
		jsonErr(w, http.StatusUnprocessableEntity, "ticket is not pending execution")
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())

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
		ActionType:   "ticket_execute",
		ResourceType: "ticket",
		ResourceID:   &id,
		IPAddress:    clientIP(r),
	})

	updated, _ := h.tickets.GetByID(r.Context(), id)
	jsonOK(w, updated)
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
