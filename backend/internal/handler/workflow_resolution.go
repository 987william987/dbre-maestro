package handler

import (
	"context"
	"fmt"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
)

const (
	workflowErrorNoMatchingRule       = "no_matching_rule"
	workflowErrorNoEffectiveApprovers = "no_effective_approval_users"
	workflowErrorNoEffectiveExecutors = "no_effective_executor_users"
	workflowErrorInvalidRule          = "invalid_rule"
	workflowExcludedInactive          = "inactive"
	workflowExcludedMissingPermission = "missing_permission"
	workflowExcludedSubmitter         = "submitter"
	workflowExecutionModeManual       = "manual"
	workflowExecutionModeAutoApproval = "auto_after_approval"
)

type workflowRuleMatcher interface {
	MatchWorkflowRule(ctx context.Context, ticketType model.TicketType, dbConnectionID *uint64, exportSensitivity *string) (*model.WorkflowRule, error)
}

func resolveTicketWorkflow(ctx context.Context, settings *repository.SettingsRepo, users *repository.UserRepo, ticket *model.Ticket) (*model.WorkflowResolution, error) {
	if ticket == nil {
		return nil, nil
	}
	dbConnectionID := ticket.DBConnectionID
	exportSensitivity := workflowExportSensitivity(ticket)
	resolution, err := resolveWorkflow(ctx, settings, users, ticket.TicketType, dbConnectionID, exportSensitivity)
	if err != nil || resolution == nil {
		return resolution, err
	}
	excludeSubmitterFromWorkflowResolution(ticket, resolution)
	return resolution, nil
}

func resolveWorkflow(ctx context.Context, settings *repository.SettingsRepo, users *repository.UserRepo, ticketType model.TicketType, dbConnectionID *uint64, exportSensitivity *string) (*model.WorkflowResolution, error) {
	return resolveWorkflowWithMatcher(ctx, settings, users, ticketType, dbConnectionID, exportSensitivity)
}

func resolveWorkflowWithMatcher(ctx context.Context, settings workflowRuleMatcher, users *repository.UserRepo, ticketType model.TicketType, dbConnectionID *uint64, exportSensitivity *string) (*model.WorkflowResolution, error) {
	resolution := &model.WorkflowResolution{
		TicketType:            ticketType,
		DBConnectionID:        dbConnectionID,
		ExportSensitivity:     exportSensitivity,
		ApprovalUserIDs:       []uint64{},
		ExecutorUserIDs:       []uint64{},
		MissingApprovalGroups: []model.AuthGroup{},
		MissingExecutorGroups: []model.AuthGroup{},
		ExcludedApprovalUsers: []model.WorkflowExcludedUser{},
		ExcludedExecutorUsers: []model.WorkflowExcludedUser{},
		ApprovalEnabled:       true,
		ExecutionMode:         workflowExecutionModeManual,
	}
	if settings == nil || users == nil {
		resolution.ErrorCode = workflowErrorInvalidRule
		resolution.ErrorMessage = "workflow resolver is not configured"
		return resolution, nil
	}
	rule, err := settings.MatchWorkflowRule(ctx, ticketType, dbConnectionID, exportSensitivity)
	if err != nil {
		return nil, err
	}
	if rule == nil {
		adminIDs, err := workflowAdminUserIDs(ctx, users)
		if err != nil {
			return nil, err
		}
		resolution.AdminUserIDs = adminIDs
		resolution.ErrorCode = workflowErrorNoMatchingRule
		resolution.ErrorMessage = "no matching workflow rule"
		return resolution, nil
	}
	resolution.RuleID = &rule.ID
	resolution.RuleName = rule.RuleName
	resolution.ApprovalEnabled = rule.ApprovalEnabled
	resolution.ExecutionMode = normalizeWorkflowExecutionMode(rule.ExecutionMode)

	approvalUsers, missingApprovalGroups, excludedApprovalUsers, err := resolveWorkflowUsers(ctx, users, rule.ApprovalAuthGroups, reviewPermissionsForTicket(ticketType))
	if err != nil {
		return nil, err
	}
	resolution.ApprovalUserIDs = approvalUsers
	resolution.MissingApprovalGroups = missingApprovalGroups
	resolution.ExcludedApprovalUsers = excludedApprovalUsers

	if resolution.ExecutionMode != workflowExecutionModeAutoApproval {
		executorUsers, missingExecutorGroups, excludedExecutorUsers, err := resolveWorkflowUsers(ctx, users, rule.ExecutorAuthGroups, []string{permissionTicketExecute})
		if err != nil {
			return nil, err
		}
		resolution.ExecutorUserIDs = executorUsers
		resolution.MissingExecutorGroups = missingExecutorGroups
		resolution.ExcludedExecutorUsers = excludedExecutorUsers
	}

	if rule.ApprovalEnabled && len(resolution.ApprovalUserIDs) == 0 {
		adminIDs, err := workflowAdminUserIDs(ctx, users)
		if err != nil {
			return nil, err
		}
		resolution.AdminUserIDs = adminIDs
		resolution.ErrorCode = workflowErrorNoEffectiveApprovers
		resolution.ErrorMessage = fmt.Sprintf("workflow rule %q has no effective approval users", rule.RuleName)
		return resolution, nil
	}
	if isExecutableTicketType(ticketType) && resolution.ExecutionMode != workflowExecutionModeAutoApproval && len(resolution.ExecutorUserIDs) == 0 {
		adminIDs, err := workflowAdminUserIDs(ctx, users)
		if err != nil {
			return nil, err
		}
		resolution.AdminUserIDs = adminIDs
		resolution.ErrorCode = workflowErrorNoEffectiveExecutors
		resolution.ErrorMessage = fmt.Sprintf("workflow rule %q has no effective executor users", rule.RuleName)
		return resolution, nil
	}
	return resolution, nil
}

func normalizeWorkflowExecutionMode(mode string) string {
	if mode == workflowExecutionModeAutoApproval {
		return workflowExecutionModeAutoApproval
	}
	return workflowExecutionModeManual
}

func resolveWorkflowUsers(ctx context.Context, users *repository.UserRepo, groups []model.AuthGroup, requiredPermissions []string) ([]uint64, []model.AuthGroup, []model.WorkflowExcludedUser, error) {
	seen := map[uint64]struct{}{}
	userIDs := []uint64{}
	missingGroups := []model.AuthGroup{}
	excluded := []model.WorkflowExcludedUser{}
	allowedIDs, err := users.ListActiveUserIDsByPermissionKeys(ctx, requiredPermissions)
	if err != nil {
		return nil, nil, nil, err
	}
	allowed := make(map[uint64]struct{}, len(allowedIDs))
	for _, userID := range allowedIDs {
		allowed[userID] = struct{}{}
	}
	for _, group := range groups {
		groupUsers, err := users.ListUsersByAuthGroup(ctx, group)
		if err != nil {
			return nil, nil, nil, err
		}
		if len(groupUsers) == 0 {
			missingGroups = append(missingGroups, group)
			continue
		}
		for _, user := range groupUsers {
			if _, ok := seen[user.ID]; ok {
				continue
			}
			seen[user.ID] = struct{}{}
			if !user.IsActive {
				excluded = append(excluded, model.WorkflowExcludedUser{UserID: user.ID, Username: user.Username, Reason: workflowExcludedInactive})
				continue
			}
			if _, ok := allowed[user.ID]; !ok {
				excluded = append(excluded, model.WorkflowExcludedUser{UserID: user.ID, Username: user.Username, Reason: workflowExcludedMissingPermission})
				continue
			}
			userIDs = append(userIDs, user.ID)
		}
	}
	return userIDs, missingGroups, excluded, nil
}

func workflowAdminUserIDs(ctx context.Context, users *repository.UserRepo) ([]uint64, error) {
	if users == nil {
		return []uint64{}, nil
	}
	admins, err := users.ListUsersByAuthGroup(ctx, model.AuthGroupAdmin)
	if err != nil {
		return nil, err
	}
	userIDs := make([]uint64, 0, len(admins))
	for _, admin := range admins {
		if admin.IsActive {
			userIDs = append(userIDs, admin.ID)
		}
	}
	return userIDs, nil
}

func workflowResolutionFromSnapshot(ticket *model.Ticket, snapshot *model.TicketWorkflowSnapshot) *model.WorkflowResolution {
	if ticket == nil || snapshot == nil {
		return nil
	}
	resolution := &model.WorkflowResolution{
		RuleID:            snapshot.RuleID,
		RuleName:          snapshot.RuleName,
		TicketType:        ticket.TicketType,
		DBConnectionID:    ticket.DBConnectionID,
		ExportSensitivity: workflowExportSensitivity(ticket),
		ApprovalEnabled:   snapshot.ApprovalEnabled,
		ExecutionMode:     normalizeWorkflowExecutionMode(snapshot.ExecutionMode),
		ApprovalUserIDs:   append([]uint64{}, snapshot.ApprovalUserIDs...),
		ExecutorUserIDs:   append([]uint64{}, snapshot.ExecutorUserIDs...),
		AdminUserIDs:      append([]uint64{}, snapshot.AdminUserIDs...),
		ErrorCode:         snapshot.ErrorCode,
		ErrorMessage:      snapshot.ErrorMessage,
	}
	excludeSubmitterFromWorkflowResolution(ticket, resolution)
	return resolution
}

func excludeSubmitterFromWorkflowResolution(ticket *model.Ticket, resolution *model.WorkflowResolution) {
	if ticket == nil || resolution == nil || ticket.SubmitterID == 0 {
		return
	}
	approvalUserIDs, approvalExcluded := excludeWorkflowUserID(resolution.ApprovalUserIDs, ticket.SubmitterID)
	if approvalExcluded {
		resolution.ApprovalUserIDs = approvalUserIDs
		resolution.ExcludedApprovalUsers = append(resolution.ExcludedApprovalUsers, model.WorkflowExcludedUser{
			UserID: ticket.SubmitterID,
			Reason: workflowExcludedSubmitter,
		})
	}
	executorUserIDs, executorExcluded := excludeWorkflowUserID(resolution.ExecutorUserIDs, ticket.SubmitterID)
	if executorExcluded {
		resolution.ExecutorUserIDs = executorUserIDs
		resolution.ExcludedExecutorUsers = append(resolution.ExcludedExecutorUsers, model.WorkflowExcludedUser{
			UserID: ticket.SubmitterID,
			Reason: workflowExcludedSubmitter,
		})
	}
	if resolution.ErrorCode != "" {
		return
	}
	if resolution.ApprovalEnabled && len(resolution.ApprovalUserIDs) == 0 {
		resolution.ErrorCode = workflowErrorNoEffectiveApprovers
		resolution.ErrorMessage = "workflow has no effective approval users after excluding the submitter"
		return
	}
	if isExecutableTicketType(ticket.TicketType) && resolution.ExecutionMode != workflowExecutionModeAutoApproval && len(resolution.ExecutorUserIDs) == 0 {
		resolution.ErrorCode = workflowErrorNoEffectiveExecutors
		resolution.ErrorMessage = "workflow has no effective executor users after excluding the submitter"
		return
	}
}

func excludeWorkflowUserID(userIDs []uint64, excludedID uint64) ([]uint64, bool) {
	next := userIDs[:0]
	excluded := false
	for _, userID := range userIDs {
		if userID == excludedID {
			excluded = true
			continue
		}
		next = append(next, userID)
	}
	if !excluded {
		return userIDs, false
	}
	return next, true
}

func workflowExportSensitivity(ticket *model.Ticket) *string {
	if ticket == nil || ticket.TicketType != model.TicketTypeSQLExport || ticket.ContainsSensitive == nil {
		return nil
	}
	value := "normal"
	if *ticket.ContainsSensitive {
		value = "sensitive"
	}
	return &value
}

func workflowExportSensitivityFromBool(containsSensitive bool) *string {
	value := "normal"
	if containsSensitive {
		value = "sensitive"
	}
	return &value
}
