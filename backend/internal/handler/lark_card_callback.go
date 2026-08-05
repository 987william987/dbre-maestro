package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/notification"
	"github.com/go-chi/chi/v5"
)

type larkCardActionRequest struct {
	Action    string
	Ticket    string
	OpenID    string
	UnionID   string
	CardStage string
	Handled   bool
}

func (h *TicketHandler) LarkCardCallback(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid callback body")
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid callback json")
		return
	}
	if challenge, _ := payload["challenge"].(string); challenge != "" {
		if !h.verifyLarkCardToken(r.Context(), payload) {
			jsonErr(w, http.StatusForbidden, "invalid lark callback token")
			return
		}
		jsonOK(w, map[string]string{"challenge": challenge})
		return
	}
	if !h.verifyLarkCardToken(r.Context(), payload) {
		jsonErr(w, http.StatusForbidden, "invalid lark callback token")
		return
	}

	actionReq := parseLarkCardAction(payload)
	if actionReq.Action == "" || actionReq.Ticket == "" {
		jsonErr(w, http.StatusBadRequest, "invalid lark card action")
		return
	}
	resp, err := h.HandleLarkCardAction(r.Context(), actionReq)
	if err != nil {
		jsonOK(w, resp)
		return
	}
	jsonOK(w, resp)
}

func (h *TicketHandler) HandleLarkCardAction(ctx context.Context, actionReq larkCardActionRequest) (map[string]any, error) {
	user, err := h.users.GetByLarkOperator(ctx, actionReq.OpenID, actionReq.UnionID)
	if err != nil {
		return larkCardToast("error", "resolve lark operator failed"), err
	}
	if user == nil {
		return larkCardToast("error", "User is not linked to DBRE Maestro."), fmt.Errorf("lark operator is not linked")
	}
	if !user.IsActive {
		return larkCardToast("error", "User is disabled."), fmt.Errorf("lark operator user is disabled")
	}

	if actionReq.Action == larkTicketActionViewDetails {
		resp, err := h.larkTicketDetailsResponse(ctx, actionReq, user.ID)
		if err != nil {
			return larkCardToast("error", err.Error()), err
		}
		return resp, nil
	}
	if actionReq.Action == larkTicketActionHideDetails {
		resp, err := h.larkTicketSummaryResponse(ctx, actionReq, user.ID)
		if err != nil {
			return larkCardToast("error", err.Error()), err
		}
		return resp, nil
	}

	updated, err := h.executeLarkTicketAction(ctx, actionReq, user.ID, user.Username)
	if err != nil {
		return larkCardToast("error", err.Error()), err
	}
	resp := larkCardToast("success", "工單操作完成。")
	if updated != nil {
		card := buildLarkTicketSummaryCard(ctx, h.dbConns, h.users, h.appBaseURL, updated, h.ticketStateLabel(updated.Status), actionReq.CardStage, true)
		resp["card"] = notification.BuildCardContent(*card)
	}
	return resp, nil
}

func (h *TicketHandler) larkTicketSummaryResponse(ctx context.Context, action larkCardActionRequest, userID uint64) (map[string]any, error) {
	permissions, err := h.users.GetEffectivePermissionKeys(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load user permissions failed")
	}
	actionCtx := context.WithValue(ctx, middleware.CtxUserID, userID)
	actionCtx = context.WithValue(actionCtx, middleware.CtxPermissions, permissions)

	ticket, err := h.resolveTicketByRef(actionCtx, action.Ticket)
	if err != nil {
		return nil, fmt.Errorf("load ticket failed")
	}
	if ticket == nil {
		return nil, fmt.Errorf("ticket not found")
	}
	canView, err := h.canViewTicket(actionCtx, ticket, userID)
	if err != nil {
		return nil, fmt.Errorf("check ticket permission failed")
	}
	if !canView {
		return nil, fmt.Errorf("permission denied")
	}

	card := buildLarkTicketSummaryCard(actionCtx, h.dbConns, h.users, h.appBaseURL, ticket, h.ticketStateLabel(ticket.Status), action.CardStage, action.Handled)
	resp := larkCardToast("success", "工單詳情已收起。")
	if card != nil {
		resp["card"] = notification.BuildCardContent(*card)
	}
	return resp, nil
}

func (h *TicketHandler) larkTicketDetailsResponse(ctx context.Context, action larkCardActionRequest, userID uint64) (map[string]any, error) {
	permissions, err := h.users.GetEffectivePermissionKeys(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load user permissions failed")
	}
	actionCtx := context.WithValue(ctx, middleware.CtxUserID, userID)
	actionCtx = context.WithValue(actionCtx, middleware.CtxPermissions, permissions)

	ticket, err := h.resolveTicketByRef(actionCtx, action.Ticket)
	if err != nil {
		return nil, fmt.Errorf("load ticket failed")
	}
	if ticket == nil {
		return nil, fmt.Errorf("ticket not found")
	}
	canView, err := h.canViewTicket(actionCtx, ticket, userID)
	if err != nil {
		return nil, fmt.Errorf("check ticket permission failed")
	}
	if !canView {
		return nil, fmt.Errorf("permission denied")
	}
	executions, err := h.tickets.ListExecutions(actionCtx, ticket.ID)
	if err != nil {
		return nil, fmt.Errorf("load statement results failed")
	}
	card := buildLarkTicketDetailCard(actionCtx, h.dbConns, h.users, h.appBaseURL, ticket, h.ticketStateLabel(ticket.Status), executions, action.CardStage, action.Handled)
	resp := larkCardToast("success", "工單詳情已載入。")
	if card != nil {
		resp["card"] = notification.BuildCardContent(*card)
	}
	return resp, nil
}

func (h *TicketHandler) verifyLarkCardToken(ctx context.Context, payload map[string]any) bool {
	if h == nil || h.settings == nil {
		return false
	}
	settings, err := h.settings.Get(ctx)
	if err != nil || settings == nil || !settings.LarkInteractiveCardsEnabled || !settings.LarkCardVerificationTokenConfigured {
		return false
	}
	expected, err := h.settings.GetLarkCardVerificationToken(ctx)
	if err != nil || strings.TrimSpace(expected) == "" {
		return false
	}
	actual := larkFirstNonEmptyString(
		stringAt(payload, "token"),
		stringAt(mapAt(payload, "header"), "token"),
		stringAt(mapAt(payload, "event"), "token"),
	)
	return strings.TrimSpace(actual) == strings.TrimSpace(expected)
}

func parseLarkCardAction(payload map[string]any) larkCardActionRequest {
	event := mapAt(payload, "event")
	action := mapAt(event, "action")
	if len(action) == 0 {
		action = mapAt(payload, "action")
	}
	value := mapAt(action, "value")
	operator := mapAt(event, "operator")
	if len(operator) == 0 {
		operator = mapAt(payload, "operator")
	}

	ticket := larkFirstNonEmptyString(
		stringAt(value, "ticket_no"),
		stringAt(value, "ticket_id"),
		stringAt(action, "ticket_no"),
		stringAt(payload, "ticket_no"),
	)
	return larkCardActionRequest{
		Action: larkFirstNonEmptyString(stringAt(value, "action"), stringAt(action, "action"), stringAt(payload, "action")),
		Ticket: ticket,
		CardStage: larkFirstNonEmptyString(
			stringAt(value, "card_stage"),
			stringAt(action, "card_stage"),
			stringAt(payload, "card_stage"),
		),
		Handled: boolAt(value, "handled") || boolAt(action, "handled") || boolAt(payload, "handled"),
		OpenID: larkFirstNonEmptyString(
			stringAt(operator, "open_id"),
			stringAt(operator, "open_id_v2"),
			stringAt(event, "open_id"),
			stringAt(payload, "open_id"),
		),
		UnionID: larkFirstNonEmptyString(
			stringAt(operator, "union_id"),
			stringAt(event, "union_id"),
			stringAt(payload, "union_id"),
		),
	}
}

func (h *TicketHandler) executeLarkTicketAction(ctx context.Context, action larkCardActionRequest, userID uint64, username string) (*model.Ticket, error) {
	permissions, err := h.users.GetEffectivePermissionKeys(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load user permissions failed")
	}
	actionCtx := context.WithValue(ctx, middleware.CtxUserID, userID)
	actionCtx = context.WithValue(actionCtx, middleware.CtxUsername, username)
	actionCtx = context.WithValue(actionCtx, middleware.CtxPermissions, permissions)

	var body string
	var handlerFunc http.HandlerFunc
	switch action.Action {
	case larkTicketActionApprove:
		body = fmt.Sprintf(`{"comment":%q}`, username+" 經由 Lark app 觸發審批")
		handlerFunc = h.Approve
	case larkTicketActionReject:
		body = fmt.Sprintf(`{"reason":%q}`, username+" 經由 Lark app 觸發拒絕")
		handlerFunc = h.Reject
	case larkTicketActionExecute:
		body = fmt.Sprintf(`{"comment":%q}`, username+" 經由 Lark app 觸發執行")
		handlerFunc = h.Execute
	default:
		return nil, fmt.Errorf("unsupported lark ticket action")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/tickets/"+action.Ticket+"/"+action.Action, bytes.NewBufferString(body))
	req = req.WithContext(actionCtx)
	req.Header.Set("Content-Type", "application/json")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("id", action.Ticket)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	rec := httptest.NewRecorder()
	handlerFunc(rec, req)
	if rec.Code >= 200 && rec.Code < 300 {
		updated, _ := h.resolveTicketByRef(ctx, action.Ticket)
		return updated, nil
	}
	return nil, errors.New(readHandlerError(rec.Body.String(), rec.Code))
}

func (h *TicketHandler) resolveTicketByRef(ctx context.Context, ref string) (*model.Ticket, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" || h.tickets == nil {
		return nil, nil
	}
	if id, err := strconv.ParseUint(ref, 10, 64); err == nil && id > 0 {
		return h.tickets.GetByID(ctx, id)
	}
	return h.tickets.GetByTicketNo(ctx, ref)
}

func larkCardToast(tone string, content string) map[string]any {
	return map[string]any{
		"toast": map[string]string{
			"type":    tone,
			"content": content,
		},
	}
}

func readHandlerError(body string, status int) string {
	var payload map[string]string
	if err := json.Unmarshal([]byte(body), &payload); err == nil {
		if msg := strings.TrimSpace(payload["error"]); msg != "" {
			return msg
		}
	}
	return "ticket action failed: " + strconv.Itoa(status)
}

func mapAt(values map[string]any, key string) map[string]any {
	if values == nil {
		return nil
	}
	item, _ := values[key].(map[string]any)
	return item
}

func stringAt(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	item := values[key]
	switch value := item.(type) {
	case string:
		return strings.TrimSpace(value)
	case float64:
		if value == float64(uint64(value)) {
			return strconv.FormatUint(uint64(value), 10)
		}
	}
	return ""
}

func boolAt(values map[string]any, key string) bool {
	if values == nil {
		return false
	}
	item := values[key]
	switch value := item.(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "true")
	}
	return false
}

func larkFirstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
