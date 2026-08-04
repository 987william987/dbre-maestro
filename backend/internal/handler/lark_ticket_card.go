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
		!settings.LarkCardVerificationTokenConfigured {
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
		if conn, err := dbConns.GetByID(ctx, *ticket.DBConnectionID); err == nil && conn != nil {
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

func buildLarkTicketCardActions(ticket *model.Ticket, notifType string) []notification.CardAction {
	baseValue := map[string]any{
		"ticket_id": ticket.ID,
		"ticket_no": ticket.TicketNo,
	}
	actions := []notification.CardAction{}
	if notifType == "ticket_pending_review" {
		actions = append(actions,
			notification.CardAction{Action: larkTicketActionApprove, Text: "Approve", Type: "primary", Value: baseValue},
			notification.CardAction{Action: larkTicketActionReject, Text: "Reject", Type: "danger", Value: baseValue},
		)
	}
	if notifType == "ticket_pending_execution" && isExecutableTicketType(ticket.TicketType) {
		actions = append(actions,
			notification.CardAction{Action: larkTicketActionExecute, Text: "Execute", Type: "primary", Value: baseValue},
			notification.CardAction{Action: larkTicketActionReject, Text: "Reject", Type: "danger", Value: baseValue},
		)
	}
	actions = append(actions, notification.CardAction{Action: larkTicketActionViewDetails, Text: "查看詳情", Type: "default", Value: baseValue})
	return actions
}

func buildLarkTicketDetailCard(
	ctx context.Context,
	dbConns *repository.DBConnectionRepo,
	users *repository.UserRepo,
	appBaseURL string,
	ticket *model.Ticket,
	statusLabel string,
	executions []model.TicketExecution,
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
		if conn, err := dbConns.GetByID(ctx, *ticket.DBConnectionID); err == nil && conn != nil {
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
	if len(executions) > 0 {
		blocks = append(blocks, "**語句狀態**\n"+larkTicketExecutionSummary(executions))
	}
	return &notification.Card{
		Title:          "工單詳情",
		Template:       larkTicketCardTemplate(string(ticket.Status)),
		Fields:         fields,
		MarkdownBlocks: blocks,
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

func larkTicketExecutionSummary(executions []model.TicketExecution) string {
	lines := make([]string, 0, len(executions))
	for _, execution := range executions {
		stmt := strings.TrimSpace(execution.SQLStmt)
		if stmt == "" {
			stmt = "-"
		}
		lines = append(lines, fmt.Sprintf("%d. %s - %s", execution.Seq, larkTicketExecutionStatusLabel(execution.Status), escapeLarkCardText(truncate(stmt, 160))))
	}
	return strings.Join(lines, "\n")
}

func larkTicketExecutionStatusLabel(status string) string {
	switch strings.TrimSpace(status) {
	case "pending":
		return "待執行"
	case "running":
		return "執行中"
	case "completed":
		return "已完成"
	case "failed":
		return "執行失敗"
	case "stopped":
		return "已停止"
	default:
		return status
	}
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
