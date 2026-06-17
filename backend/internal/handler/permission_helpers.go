package handler

import (
	"context"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
)

const (
	permissionTicketReview          = "tickets.review"
	permissionTicketExecute         = "tickets.execute"
	permissionSQLEditorExportReview = "sql_editor.export_review"
	permissionSQLEditorSensitiveRev = "sql_editor.sensitive_review"
)

func ticketWorkspaceRealtimePermissions() []string {
	return []string{
		permissionTicketReview,
		permissionTicketExecute,
		permissionSQLEditorExportReview,
		permissionSQLEditorSensitiveRev,
	}
}

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

func listActiveUserIDsByPermissions(ctx context.Context, users *repository.UserRepo, permissionKeys []string) ([]uint64, error) {
	if users == nil || len(permissionKeys) == 0 {
		return []uint64{}, nil
	}
	return users.ListActiveUserIDsByPermissionKeys(ctx, permissionKeys)
}
