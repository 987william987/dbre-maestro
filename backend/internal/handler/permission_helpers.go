package handler

import "github.com/dbre-maestro/maestro/internal/model"

const (
	permissionTicketReview          = "tickets.review"
	permissionTicketExecute         = "tickets.execute"
	permissionSQLEditorExportReview = "sql_editor.export_review"
	permissionSQLEditorSensitiveRev = "sql_editor.sensitive_review"
)

func reviewPermissionsForTicket(ticketType model.TicketType) []string {
	switch ticketType {
	case model.TicketTypeSQLExport:
		return []string{permissionSQLEditorExportReview}
	case model.TicketTypeSensitiveQueryAccess:
		return []string{permissionSQLEditorSensitiveRev}
	default:
		return []string{permissionTicketReview}
	}
}

func reviewPermissionsForWorkflow(workflowType model.ApprovalWorkflowType) []string {
	switch workflowType {
	case model.ApprovalWorkflowSQLExportNormal, model.ApprovalWorkflowSQLExportSensitive:
		return []string{permissionSQLEditorExportReview}
	case model.ApprovalWorkflowSensitiveQueryAccess:
		return []string{permissionSQLEditorSensitiveRev}
	case model.ApprovalWorkflowDDL, model.ApprovalWorkflowDML, model.ApprovalWorkflowRedisCommand, model.ApprovalWorkflowQueryAccess:
		return []string{permissionTicketReview}
	default:
		return nil
	}
}
