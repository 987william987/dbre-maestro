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

func listActiveUserIDsByPermissions(ctx context.Context, users *repository.UserRepo, permissionKeys []string) ([]uint64, error) {
	if users == nil || len(permissionKeys) == 0 {
		return []uint64{}, nil
	}
	return users.ListActiveUserIDsByPermissionKeys(ctx, permissionKeys)
}

func approvalPolicyReviewerIDs(ctx context.Context, settings *repository.SettingsRepo, users *repository.UserRepo, workflowType model.ApprovalWorkflowType) ([]uint64, bool, error) {
	if settings == nil || users == nil || workflowType == "" {
		return nil, false, nil
	}
	policy, err := settings.GetApprovalPolicy(ctx, workflowType)
	if err != nil || policy == nil || !policy.Enabled {
		return nil, false, err
	}
	seen := make(map[uint64]struct{})
	reviewerIDs := make([]uint64, 0, len(policy.ReviewerUserIDs))
	addUser := func(userID uint64) {
		if userID == 0 {
			return
		}
		if _, ok := seen[userID]; ok {
			return
		}
		seen[userID] = struct{}{}
		reviewerIDs = append(reviewerIDs, userID)
	}
	for _, userID := range policy.ReviewerUserIDs {
		user, err := users.GetByID(ctx, userID)
		if err != nil {
			return nil, true, err
		}
		if user != nil && user.IsActive {
			addUser(user.ID)
		}
	}
	for _, authGroup := range policy.ReviewerAuthGroups {
		groupUsers, err := users.ListUsersByAuthGroup(ctx, authGroup)
		if err != nil {
			return nil, true, err
		}
		for _, user := range groupUsers {
			if user.IsActive {
				addUser(user.ID)
			}
		}
	}
	permissionKeys := reviewPermissionsForWorkflow(workflowType)
	if len(permissionKeys) > 0 && len(reviewerIDs) > 0 {
		allowedIDs, err := users.ListActiveUserIDsByPermissionKeys(ctx, permissionKeys)
		if err != nil {
			return nil, true, err
		}
		allowed := make(map[uint64]struct{}, len(allowedIDs))
		for _, userID := range allowedIDs {
			allowed[userID] = struct{}{}
		}
		filtered := reviewerIDs[:0]
		for _, reviewerID := range reviewerIDs {
			if _, ok := allowed[reviewerID]; ok {
				filtered = append(filtered, reviewerID)
			}
		}
		reviewerIDs = filtered
	}
	return reviewerIDs, len(reviewerIDs) > 0 || len(policy.ReviewerUserIDs) > 0 || len(policy.ReviewerAuthGroups) > 0, nil
}
