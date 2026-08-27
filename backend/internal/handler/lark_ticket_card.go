package handler

import (
	"context"
	"fmt"
	"strings"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/notification"
	"github.com/dbre-maestro/maestro/internal/repository"
)

const (
	larkTicketActionApprove     = "ticket_approve"
	larkTicketActionReject      = "ticket_reject"
	larkTicketActionExecute     = "ticket_execute"
	larkTicketActionViewDetails = "ticket_view_details"
	larkTicketActionHideDetails = "ticket_hide_details"

	larkTicketCardStageReview    = "review"
	larkTicketCardStageExecution = "execution"
	larkTicketCardStageResult    = "result"
)

func buildLarkTicketCard(
	ctx context.Context,
	settingsRepo *repository.SettingsRepo,
	dbConns *repository.DBConnectionRepo,
	users *repository.UserRepo,
	appBaseURL string,
	ticket *model.Ticket,
	title string,
	notifType string,
	statusLabel string,
) *notification.Card {
	if ticket == nil || settingsRepo == nil {
		return nil
	}
	settings, err := settingsRepo.Get(ctx)
	if err != nil || settings == nil {
		return nil
	}
	if !settings.LarkInteractiveCardsEnabled ||
		strings.TrimSpace(settings.LarkAppID) == "" ||
		!settings.LarkAppSecretConfigured ||
		(settings.LarkCardCallbackMode != "long_connection" && !settings.LarkCardVerificationTokenConfigured) {
		return nil
	}

	fields := []notification.CardField{
		{Label: "工單號", Value: ticket.TicketNo},
		{Label: "工單類型", Value: larkTicketTypeLabel(ticket.TicketType)},
		{Label: "目前狀態", Value: statusLabel},
	}
	if submitter := larkTicketSubmitterName(ctx, users, ticket); submitter != "" {
		fields = append(fields, notification.CardField{Label: "提交者", Value: submitter})
	}
	if ticket.DBConnectionID != nil && dbConns != nil {
		if conn, err := dbConns.GetAnyByID(ctx, *ticket.DBConnectionID); err == nil && conn != nil {
			fields = append(fields, notification.CardField{Label: "數據庫實例", Value: conn.Name})
		}
	}
	if ticket.DatabaseName != nil && strings.TrimSpace(*ticket.DatabaseName) != "" {
		fields = append(fields, notification.CardField{Label: "數據庫", Value: strings.TrimSpace(*ticket.DatabaseName)})
	}
	if ticket.SchemaName != nil && strings.TrimSpace(*ticket.SchemaName) != "" {
		fields = append(fields, notification.CardField{Label: "Schema", Value: strings.TrimSpace(*ticket.SchemaName)})
	}

	viewURL := ticketURL(appBaseURL, ticket.TicketNo)
	fields = appendLarkTicketLinkField(fields, viewURL)
	actions := buildLarkTicketCardActions(ticket, notifType)
	return &notification.Card{
		Title:    title,
		Template: larkTicketCardTemplate(notifType),
		Fields:   fields,
		Actions:  actions,
	}
}

func buildLarkTicketSummaryCard(
	ctx context.Context,
	dbConns *repository.DBConnectionRepo,
	users *repository.UserRepo,
	appBaseURL string,
	ticket *model.Ticket,
	statusLabel string,
	cardStage string,
	handled bool,
) *notification.Card {
	if ticket == nil {
		return nil
	}
	fields := buildLarkTicketSummaryFields(ctx, dbConns, users, appBaseURL, ticket, statusLabel)
	return &notification.Card{
		Title:    larkTicketSummaryTitleForCardContext(ticket.Status, cardStage, handled),
		Template: larkTicketCardTemplate(string(ticket.Status)),
		Fields:   fields,
		Actions:  buildLarkTicketCardActionsForCardContext(ticket, cardStage, handled, false),
	}
}

func buildLarkTicketSummaryFields(
	ctx context.Context,
	dbConns *repository.DBConnectionRepo,
	users *repository.UserRepo,
	appBaseURL string,
	ticket *model.Ticket,
	statusLabel string,
) []notification.CardField {
	fields := []notification.CardField{
		{Label: "工單號", Value: ticket.TicketNo},
		{Label: "工單類型", Value: larkTicketTypeLabel(ticket.TicketType)},
		{Label: "目前狀態", Value: statusLabel},
	}
	if submitter := larkTicketSubmitterName(ctx, users, ticket); submitter != "" {
		fields = append(fields, notification.CardField{Label: "提交者", Value: submitter})
	}
	if ticket.DBConnectionID != nil && dbConns != nil {
		if conn, err := dbConns.GetAnyByID(ctx, *ticket.DBConnectionID); err == nil && conn != nil {
			fields = append(fields, notification.CardField{Label: "數據庫實例", Value: conn.Name})
		}
	}
	if ticket.DatabaseName != nil && strings.TrimSpace(*ticket.DatabaseName) != "" {
		fields = append(fields, notification.CardField{Label: "數據庫", Value: strings.TrimSpace(*ticket.DatabaseName)})
	}
	if ticket.SchemaName != nil && strings.TrimSpace(*ticket.SchemaName) != "" {
		fields = append(fields, notification.CardField{Label: "Schema", Value: strings.TrimSpace(*ticket.SchemaName)})
	}
	return appendLarkTicketLinkField(fields, ticketURL(appBaseURL, ticket.TicketNo))
}

func larkTicketSummaryTitle(status model.TicketStatus) string {
	switch status {
	case model.TicketStatusPendingReview:
		return "工單待審核"
	case model.TicketStatusApproved, model.TicketStatusPendingExecution:
		return "工單待執行"
	case model.TicketStatusExecuting:
		return "工單執行中"
	case model.TicketStatusCompleted:
		return "工單已完成"
	case model.TicketStatusRejected:
		return "工單已拒絕"
	case model.TicketStatusFailed, model.TicketStatusStopped, model.TicketStatusInterrupted:
		return "工單失敗"
	default:
		return "工單狀態更新"
	}
}

func larkTicketSummaryTitleForCardContext(status model.TicketStatus, cardStage string, handled bool) string {
	if !handled {
		return larkTicketSummaryTitle(status)
	}
	switch normalizeLarkTicketCardStage(cardStage) {
	case larkTicketCardStageReview:
		if status == model.TicketStatusRejected {
			return "工單已拒絕"
		}
		return "審批已完成"
	case larkTicketCardStageExecution:
		switch status {
		case model.TicketStatusRejected:
			return "工單已拒絕"
		case model.TicketStatusFailed, model.TicketStatusStopped, model.TicketStatusInterrupted:
			return "工單失敗"
		case model.TicketStatusCompleted:
			return "工單已完成"
		default:
			return "執行操作已完成"
		}
	default:
		return larkTicketSummaryTitle(status)
	}
}

func buildLarkTicketCardActions(ticket *model.Ticket, notifType string) []notification.CardAction {
	baseValue := map[string]any{
		"ticket_id":  ticket.ID,
		"ticket_no":  ticket.TicketNo,
		"card_stage": larkTicketCardStageForNotification(notifType),
		"handled":    larkTicketCardHandledForNotification(notifType),
	}
	actions := []notification.CardAction{}
	if baseValue["card_stage"] == larkTicketCardStageReview && !baseValue["handled"].(bool) {
		actions = append(actions,
			notification.CardAction{Action: larkTicketActionApprove, Text: "Approve", Type: "primary", Value: baseValue},
			notification.CardAction{Action: larkTicketActionReject, Text: "Reject", Type: "danger", Value: baseValue},
		)
	}
	if baseValue["card_stage"] == larkTicketCardStageExecution && !baseValue["handled"].(bool) && isExecutableTicketType(ticket.TicketType) {
		actions = append(actions,
			notification.CardAction{Action: larkTicketActionExecute, Text: "Execute", Type: "primary", Value: baseValue},
			notification.CardAction{Action: larkTicketActionReject, Text: "Reject", Type: "danger", Value: baseValue},
		)
	}
	actions = append(actions, notification.CardAction{Action: larkTicketActionViewDetails, Text: "查看詳情", Type: "default", Value: baseValue})
	return actions
}

func buildLarkTicketCardActionsForCardContext(ticket *model.Ticket, cardStage string, handled bool, detailExpanded bool) []notification.CardAction {
	if ticket == nil {
		return nil
	}
	viewAction := larkTicketActionViewDetails
	viewText := "查看詳情"
	if detailExpanded {
		viewAction = larkTicketActionHideDetails
		viewText = "收起詳情"
	}
	switch normalizeLarkTicketCardStage(cardStage) {
	case larkTicketCardStageReview:
		return buildLarkTicketCardActionsWithView(ticket, "ticket_pending_review", handled, viewAction, viewText)
	case larkTicketCardStageExecution:
		return buildLarkTicketCardActionsWithView(ticket, "ticket_pending_execution", handled, viewAction, viewText)
	default:
		return buildLarkTicketCardActionsWithView(ticket, string(ticket.Status), true, viewAction, viewText)
	}
}

func buildLarkTicketCardActionsWithView(ticket *model.Ticket, notifType string, handled bool, viewAction string, viewText string) []notification.CardAction {
	if handled {
		return []notification.CardAction{
			{
				Action: viewAction,
				Text:   viewText,
				Type:   "default",
				Value:  larkTicketCardActionValue(ticket, larkTicketCardStageForNotification(notifType), true),
			},
		}
	}
	actions := buildLarkTicketCardActions(ticket, notifType)
	if len(actions) == 0 {
		return actions
	}
	last := len(actions) - 1
	if actions[last].Action == larkTicketActionViewDetails {
		actions[last].Action = viewAction
		actions[last].Text = viewText
		actions[last].Value = cloneLarkCardActionValue(actions[last].Value)
		actions[last].Value["handled"] = handled
	}
	return actions
}

func larkTicketCardActionValue(ticket *model.Ticket, cardStage string, handled bool) map[string]any {
	return map[string]any{
		"ticket_id":  ticket.ID,
		"ticket_no":  ticket.TicketNo,
		"card_stage": normalizeLarkTicketCardStage(cardStage),
		"handled":    handled,
	}
}

func larkTicketCardStageForNotification(notifType string) string {
	switch notifType {
	case "ticket_pending_review":
		return larkTicketCardStageReview
	case "ticket_pending_execution":
		return larkTicketCardStageExecution
	default:
		return larkTicketCardStageResult
	}
}

func larkTicketCardHandledForNotification(notifType string) bool {
	return larkTicketCardStageForNotification(notifType) == larkTicketCardStageResult
}

func larkTicketCardContextHandled(ticket *model.Ticket, cardStage string, handled bool) bool {
	if handled || ticket == nil {
		return true
	}
	switch normalizeLarkTicketCardStage(cardStage) {
	case larkTicketCardStageReview:
		return ticket.Status != model.TicketStatusPendingReview
	case larkTicketCardStageExecution:
		return ticket.Status != model.TicketStatusPendingExecution
	default:
		return true
	}
}

func larkTicketCardStageForStatus(status model.TicketStatus) string {
	switch status {
	case model.TicketStatusPendingReview:
		return larkTicketCardStageReview
	case model.TicketStatusApproved, model.TicketStatusPendingExecution:
		return larkTicketCardStageExecution
	default:
		return larkTicketCardStageResult
	}
}

func normalizeLarkTicketCardStage(stage string) string {
	switch strings.TrimSpace(stage) {
	case larkTicketCardStageReview, "ticket_pending_review":
		return larkTicketCardStageReview
	case larkTicketCardStageExecution, "ticket_pending_execution":
		return larkTicketCardStageExecution
	default:
		return larkTicketCardStageResult
	}
}

func cloneLarkCardActionValue(value map[string]any) map[string]any {
	cloned := make(map[string]any, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
}

func buildLarkTicketDetailCard(
	ctx context.Context,
	dbConns *repository.DBConnectionRepo,
	users *repository.UserRepo,
	appBaseURL string,
	ticket *model.Ticket,
	statusLabel string,
	cardStage string,
	handled bool,
) *notification.Card {
	if ticket == nil {
		return nil
	}
	fields := []notification.CardField{
		{Label: "工單號", Value: ticket.TicketNo},
		{Label: "標題", Value: ticket.Title},
		{Label: "工單類型", Value: larkTicketTypeLabel(ticket.TicketType)},
		{Label: "目前狀態", Value: statusLabel},
	}
	if submitter := larkTicketSubmitterName(ctx, users, ticket); submitter != "" {
		fields = append(fields, notification.CardField{Label: "提交者", Value: submitter})
	}
	if ticket.DBConnectionID != nil && dbConns != nil {
		if conn, err := dbConns.GetAnyByID(ctx, *ticket.DBConnectionID); err == nil && conn != nil {
			fields = append(fields, notification.CardField{Label: "數據庫實例", Value: conn.Name})
		}
	}
	if ticket.DatabaseName != nil && strings.TrimSpace(*ticket.DatabaseName) != "" {
		fields = append(fields, notification.CardField{Label: "數據庫", Value: strings.TrimSpace(*ticket.DatabaseName)})
	}
	if ticket.SchemaName != nil && strings.TrimSpace(*ticket.SchemaName) != "" {
		fields = append(fields, notification.CardField{Label: "Schema", Value: strings.TrimSpace(*ticket.SchemaName)})
	}
	fields = appendLarkTicketLinkField(fields, ticketURL(appBaseURL, ticket.TicketNo))
	blocks := []string{}
	if ticket.Description != nil && strings.TrimSpace(*ticket.Description) != "" {
		blocks = append(blocks, "**說明**\n"+escapeLarkCardText(*ticket.Description))
	}
	if ticket.RejectionReason != nil && strings.TrimSpace(*ticket.RejectionReason) != "" {
		blocks = append(blocks, "**拒絕原因**\n"+escapeLarkCardText(*ticket.RejectionReason))
	}
	if strings.TrimSpace(ticket.SQLContent) != "" {
		blocks = append(blocks, "**SQL**\n```sql\n"+larkCardCodeBlock(ticket.SQLContent, 3000)+"\n```")
	}
	return &notification.Card{
		Title:          "工單詳情",
		Template:       larkTicketCardTemplate(string(ticket.Status)),
		Fields:         fields,
		MarkdownBlocks: blocks,
		Actions:        buildLarkTicketCardActionsForCardContext(ticket, cardStage, handled, true),
	}
}

func larkTicketCardTemplate(notifType string) string {
	switch notifType {
	case "ticket_pending_review", "ticket_pending_execution":
		return "orange"
	case "ticket_execution_failed", "ticket_rejected":
		return "red"
	case "ticket_executed", "ticket_approved":
		return "green"
	case string(model.TicketStatusPendingReview), string(model.TicketStatusPendingExecution), string(model.TicketStatusExecuting):
		return "orange"
	case string(model.TicketStatusFailed), string(model.TicketStatusRejected), string(model.TicketStatusInterrupted):
		return "red"
	case string(model.TicketStatusCompleted), string(model.TicketStatusApproved):
		return "green"
	default:
		return "blue"
	}
}

func larkTicketTypeLabel(ticketType model.TicketType) string {
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

func ticketURL(appBaseURL string, ticketNo string) string {
	if strings.TrimSpace(ticketNo) == "" {
		return ""
	}
	path := fmt.Sprintf("/tickets/%s", ticketNo)
	base := strings.TrimRight(strings.TrimSpace(appBaseURL), "/")
	if base == "" {
		return path
	}
	return base + path
}

func appendLarkTicketLinkField(fields []notification.CardField, viewURL string) []notification.CardField {
	viewURL = strings.TrimSpace(viewURL)
	if viewURL == "" {
		return fields
	}
	return append(fields, notification.CardField{Label: "工單連結", Value: fmt.Sprintf("[%s](%s)", viewURL, larkCardMarkdownURL(viewURL))})
}

func larkTicketSubmitterName(ctx context.Context, users *repository.UserRepo, ticket *model.Ticket) string {
	if users == nil || ticket == nil || ticket.SubmitterID == 0 {
		return ""
	}
	user, err := users.GetByID(ctx, ticket.SubmitterID)
	if err != nil || user == nil {
		return ""
	}
	return strings.TrimSpace(user.Username)
}

func larkCardCodeBlock(value string, limit int) string {
	value = strings.TrimSpace(truncate(value, limit))
	value = strings.ReplaceAll(value, "```", "'''")
	return value
}

func larkCardMarkdownURL(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, ")", "%29")
	return value
}

func escapeLarkCardText(value string) string {
	value = truncate(strings.TrimSpace(value), 1000)
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "*", "\\*")
	value = strings.ReplaceAll(value, "`", "\\`")
	return value
}
