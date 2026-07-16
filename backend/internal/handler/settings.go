package handler

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/dbre-maestro/maestro/internal/job"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
)

type SettingsHandler struct {
	settings *repository.SettingsRepo
	users    *repository.UserRepo
	auths    *repository.AuthGroupRepo
	dbConns  *repository.DBConnectionRepo
	audit    *repository.AuditRepo
	appEnv   string
}

type SettingsHandlerOption func(*SettingsHandler)

func WithSettingsHandlerAppEnv(appEnv string) SettingsHandlerOption {
	return func(h *SettingsHandler) {
		h.appEnv = strings.TrimSpace(appEnv)
	}
}

func NewSettingsHandler(settings *repository.SettingsRepo, users *repository.UserRepo, auths *repository.AuthGroupRepo, dbConns *repository.DBConnectionRepo, audit *repository.AuditRepo, opts ...SettingsHandlerOption) *SettingsHandler {
	h := &SettingsHandler{
		settings: settings,
		users:    users,
		auths:    auths,
		dbConns:  dbConns,
		audit:    audit,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func (h *SettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settings.Get(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load settings failed")
		return
	}
	settings.AppEnv = h.appEnv
	jsonOK(w, settings)
}

func (h *SettingsHandler) ListDBConnections(w http.ResponseWriter, r *http.Request) {
	connections, err := h.dbConns.List(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list db connections failed")
		return
	}

	type item struct {
		ID     uint64 `json:"id"`
		Name   string `json:"name"`
		DBType string `json:"db_type"`
		Host   string `json:"host"`
		Port   uint16 `json:"port"`
	}

	items := make([]item, 0, len(connections))
	for _, connection := range connections {
		items = append(items, item{
			ID:     connection.ID,
			Name:   connection.Name,
			DBType: connection.DBType,
			Host:   connection.Host,
			Port:   connection.Port,
		})
	}

	jsonOK(w, map[string]any{
		"connections": items,
	})
}

func (h *SettingsHandler) ApprovalResolution(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settings.Get(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load settings failed")
		return
	}
	resolution, err := h.resolveApprovalPolicies(r.Context(), settings.ApprovalPolicies)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "resolve approval policies failed")
		return
	}
	jsonOK(w, map[string]any{"workflows": resolution})
}

func (h *SettingsHandler) ListWorkflowRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.settings.ListWorkflowRules(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list workflow rules failed")
		return
	}
	jsonOK(w, map[string]any{"workflow_rules": rules})
}

func (h *SettingsHandler) ReplaceWorkflowRules(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkflowRules []model.WorkflowRule `json:"workflow_rules"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.validateWorkflowRules(r.Context(), req.WorkflowRules); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := h.settings.ReplaceWorkflowRules(r.Context(), req.WorkflowRules); err != nil {
		jsonErr(w, http.StatusInternalServerError, "replace workflow rules failed")
		return
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "workflow_rules_update",
		ResourceType: "settings",
		Details:      map[string]any{"workflow_rules": req.WorkflowRules},
		IPAddress:    clientIP(r),
	})
	jsonOK(w, map[string]any{"workflow_rules": req.WorkflowRules})
}

func (h *SettingsHandler) PreviewWorkflowRule(w http.ResponseWriter, r *http.Request) {
	var req model.WorkflowRule
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.validateWorkflowRuleShape(r.Context(), req); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	resolution, err := resolveWorkflowWithMatcher(r.Context(), &workflowRulePreviewSettings{rule: req}, h.users, req.TicketType, req.DBConnectionID, req.ExportSensitivity)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "preview workflow rule failed")
		return
	}
	jsonOK(w, map[string]any{"workflow_resolution": resolution})
}

func (h *SettingsHandler) PreviewWorkflowRules(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkflowRules []model.WorkflowRule `json:"workflow_rules"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	rules := withWorkflowPreviewIDs(req.WorkflowRules)
	previews := make([]model.WorkflowRulePreview, 0, len(rules))
	matcher := &workflowRuleSetPreviewSettings{rules: rules}
	for _, rule := range rules {
		resolution, err := resolveWorkflowWithMatcher(r.Context(), &workflowRulePreviewSettings{rule: rule}, h.users, rule.TicketType, rule.DBConnectionID, rule.ExportSensitivity)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "preview workflow rule failed")
			return
		}
		if resolution == nil {
			resolution = &model.WorkflowResolution{TicketType: rule.TicketType, DBConnectionID: rule.DBConnectionID, ExportSensitivity: rule.ExportSensitivity}
		}
		selected, err := matcher.MatchWorkflowRule(r.Context(), rule.TicketType, rule.DBConnectionID, rule.ExportSensitivity)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "preview workflow rule conflicts failed")
			return
		}
		preview := model.WorkflowRulePreview{
			Rule:              rule,
			Resolution:        *resolution,
			ApprovalUsers:     h.workflowRulePreviewUsers(r.Context(), resolution.ApprovalUserIDs),
			ExecutorUsers:     h.workflowRulePreviewUsers(r.Context(), resolution.ExecutorUserIDs),
			AdminUsers:        h.workflowRulePreviewUsers(r.Context(), resolution.AdminUserIDs),
			Effective:         rule.Enabled && selected != nil && selected.ID == rule.ID,
			ConflictRuleIDs:   []uint64{},
			ConflictRuleNames: []string{},
		}
		if rule.Enabled && selected != nil && selected.ID != rule.ID {
			preview.ShadowedByRuleID = &selected.ID
			preview.ShadowedByRuleName = selected.RuleName
		}
		for _, other := range rules {
			if other.ID == rule.ID || !other.Enabled || !rule.Enabled {
				continue
			}
			if workflowRulesConflict(rule, other) {
				preview.ConflictRuleIDs = append(preview.ConflictRuleIDs, other.ID)
				preview.ConflictRuleNames = append(preview.ConflictRuleNames, other.RuleName)
			}
		}
		previews = append(previews, preview)
	}
	jsonOK(w, map[string]any{"previews": previews})
}

func (h *SettingsHandler) SimulateWorkflowRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TicketType        model.TicketType `json:"ticket_type"`
		DBConnectionID    *uint64          `json:"db_connection_id"`
		ExportSensitivity *string          `json:"export_sensitivity"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TicketType == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "ticket_type is required")
		return
	}
	if req.TicketType == model.TicketTypeSQLExport {
		if req.ExportSensitivity == nil || (*req.ExportSensitivity != "normal" && *req.ExportSensitivity != "sensitive") {
			jsonErr(w, http.StatusUnprocessableEntity, "sql_export simulation requires export_sensitivity normal or sensitive")
			return
		}
	} else {
		req.ExportSensitivity = nil
	}
	resolution, err := resolveWorkflow(r.Context(), h.settings, h.users, req.TicketType, req.DBConnectionID, req.ExportSensitivity)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "simulate workflow rule failed")
		return
	}
	jsonOK(w, map[string]any{"workflow_resolution": resolution})
}

func (h *SettingsHandler) workflowRulePreviewUsers(ctx context.Context, userIDs []uint64) []model.WorkflowRuleUser {
	if h.users == nil || len(userIDs) == 0 {
		return []model.WorkflowRuleUser{}
	}
	users, err := h.users.ListByIDs(ctx, userIDs)
	if err != nil {
		return []model.WorkflowRuleUser{}
	}
	items := make([]model.WorkflowRuleUser, 0, len(users))
	for _, user := range users {
		items = append(items, model.WorkflowRuleUser{ID: user.ID, Username: user.Username})
	}
	return items
}

func (h *SettingsHandler) Patch(w http.ResponseWriter, r *http.Request) {
	var req model.PlatformSettings
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	currentSettings, err := h.settings.Get(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load settings failed")
		return
	}

	for _, userID := range append([]uint64{}, req.SensitiveExportReviewerUserIDs...) {
		if err := h.validateUserExists(r, userID); err != nil {
			jsonErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	for _, userID := range append([]uint64{}, req.SensitiveQueryAccessReviewerUserIDs...) {
		if err := h.validateUserExists(r, userID); err != nil {
			jsonErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	larkSecretConfigured, larkSecretRequired := resolveLarkSecretState(req.LarkAppID, req.LarkAppSecret, req.LarkAppSecretConfigured, currentSettings.LarkAppSecretConfigured)
	req.LarkAppSecretConfigured = larkSecretConfigured
	if larkSecretRequired {
		jsonErr(w, http.StatusUnprocessableEntity, "lark_app_secret is required when configuring lark for the first time")
		return
	}
	req.LarkOAuthSite = normalizeLarkOAuthSite(req.LarkOAuthSite)
	req.LarkOAuthRedirectURL = strings.TrimSpace(req.LarkOAuthRedirectURL)
	if req.LarkOAuthEnabled && strings.TrimSpace(req.LarkAppID) == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "lark_app_id is required when lark oauth login is enabled")
		return
	}
	if req.LarkOAuthEnabled && !req.LarkAppSecretConfigured {
		jsonErr(w, http.StatusUnprocessableEntity, "lark_app_secret is required when lark oauth login is enabled")
		return
	}
	if req.LarkOAuthEnabled && req.LarkOAuthRedirectURL == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "lark_oauth_redirect_url is required when lark oauth login is enabled")
		return
	}
	for _, connectionID := range append([]uint64{}, req.DBMetadataObjectEnabledConnectionIDs...) {
		if err := h.validateConnectionExists(r, connectionID); err != nil {
			jsonErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	if req.SQLEditorAppTimeoutSeconds <= 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "sql_editor_app_timeout_seconds must be greater than 0")
		return
	}
	if req.SQLEditorMySQLMaxExecutionTimeMs <= 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "sql_editor_mysql_max_execution_time_ms must be greater than 0")
		return
	}
	if req.SQLEditorPostgresStatementTimeoutMs <= 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "sql_editor_postgres_statement_timeout_ms must be greater than 0")
		return
	}
	if req.SQLExportAppTimeoutSeconds <= 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "sql_export_app_timeout_seconds must be greater than 0")
		return
	}
	if req.SQLExportMySQLMaxExecutionTimeMs <= 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "sql_export_mysql_max_execution_time_ms must be greater than 0")
		return
	}
	if req.SQLExportPostgresStatementTimeoutMs <= 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "sql_export_postgres_statement_timeout_ms must be greater than 0")
		return
	}
	if req.DBMetadataInventorySyncIntervalMins <= 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "db_metadata_inventory_sync_interval_minutes must be greater than 0")
		return
	}
	if err := job.ValidateCronExpression(req.DBMetadataInventoryCron); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "db_metadata_inventory_cron is invalid: "+err.Error())
		return
	}
	if req.DBMetadataObjectSyncIntervalMins <= 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "db_metadata_object_sync_interval_minutes must be greater than 0")
		return
	}
	if err := job.ValidateCronExpression(req.DBMetadataObjectCron); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "db_metadata_object_cron is invalid: "+err.Error())
		return
	}
	if strings.TrimSpace(req.DBMetadataCronTimezone) == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "db_metadata_cron_timezone is required")
		return
	}
	if _, err := time.LoadLocation(strings.TrimSpace(req.DBMetadataCronTimezone)); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "db_metadata_cron_timezone is invalid")
		return
	}
	if len(req.ApprovalPolicies) == 0 {
		req.ApprovalPolicies = currentSettings.ApprovalPolicies
	}
	if req.WorkflowRules == nil {
		req.WorkflowRules = currentSettings.WorkflowRules
	}
	if err := h.validateWorkflowRules(r.Context(), req.WorkflowRules); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	if err := h.settings.Replace(r.Context(), &req); err != nil {
		jsonErr(w, http.StatusInternalServerError, "update settings failed")
		return
	}
	req.AppEnv = h.appEnv

	actorID := middleware.UserIDFromCtx(r.Context())
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "settings_update",
		ResourceType: "settings",
		Details: map[string]any{
			"sensitive_export_reviewer_user_ids":          req.SensitiveExportReviewerUserIDs,
			"sensitive_query_access_reviewer_user_ids":    req.SensitiveQueryAccessReviewerUserIDs,
			"deprecated_reviewer_settings":                true,
			"require_non_sensitive_export_review":         req.RequireNonSensitiveExportReview,
			"lark_app_id":                                 req.LarkAppID,
			"lark_app_secret_configured":                  req.LarkAppSecretConfigured || req.LarkAppSecret != "",
			"lark_oauth_enabled":                          req.LarkOAuthEnabled,
			"lark_oauth_site":                             req.LarkOAuthSite,
			"lark_oauth_redirect_url_configured":          req.LarkOAuthRedirectURL != "",
			"sql_editor_app_timeout_seconds":              req.SQLEditorAppTimeoutSeconds,
			"sql_editor_mysql_max_execution_time_ms":      req.SQLEditorMySQLMaxExecutionTimeMs,
			"sql_editor_postgres_statement_timeout_ms":    req.SQLEditorPostgresStatementTimeoutMs,
			"sql_export_app_timeout_seconds":              req.SQLExportAppTimeoutSeconds,
			"sql_export_mysql_max_execution_time_ms":      req.SQLExportMySQLMaxExecutionTimeMs,
			"sql_export_postgres_statement_timeout_ms":    req.SQLExportPostgresStatementTimeoutMs,
			"db_metadata_inventory_enabled":               req.DBMetadataInventoryEnabled,
			"db_metadata_inventory_regions":               req.DBMetadataInventoryRegions,
			"db_metadata_inventory_engines":               req.DBMetadataInventoryEngines,
			"db_metadata_inventory_cron":                  req.DBMetadataInventoryCron,
			"db_metadata_inventory_sync_interval_minutes": req.DBMetadataInventorySyncIntervalMins,
			"db_metadata_object_enabled":                  req.DBMetadataObjectEnabled,
			"db_metadata_object_enabled_connection_ids":   req.DBMetadataObjectEnabledConnectionIDs,
			"db_metadata_object_cron":                     req.DBMetadataObjectCron,
			"db_metadata_object_sync_interval_minutes":    req.DBMetadataObjectSyncIntervalMins,
			"db_metadata_cron_timezone":                   req.DBMetadataCronTimezone,
			"approval_policies":                           req.ApprovalPolicies,
		},
		IPAddress: clientIP(r),
	})

	jsonOK(w, req)
}

func (h *SettingsHandler) validateUserExists(r *http.Request, userID uint64) error {
	user, err := h.users.GetByID(r.Context(), userID)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("user %d does not exist", userID)
	}
	return nil
}

func resolveLarkSecretState(appID string, appSecret string, requestConfigured bool, currentConfigured bool) (configured bool, required bool) {
	if strings.TrimSpace(appSecret) != "" {
		return true, false
	}
	if strings.TrimSpace(appID) == "" {
		return requestConfigured || currentConfigured, false
	}
	if requestConfigured || currentConfigured {
		return true, false
	}
	return false, true
}

func normalizeLarkOAuthSite(site string) string {
	switch strings.ToLower(strings.TrimSpace(site)) {
	case "feishu":
		return "feishu"
	default:
		return "lark"
	}
}

type approvalResolutionUser struct {
	ID       uint64   `json:"id"`
	Username string   `json:"username"`
	Sources  []string `json:"sources,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

type approvalResolutionWorkflow struct {
	WorkflowType           model.ApprovalWorkflowType `json:"workflow_type"`
	Enabled                bool                       `json:"enabled"`
	RequiredPermissions    []string                   `json:"required_permissions"`
	ReviewerUserIDs        []uint64                   `json:"reviewer_user_ids"`
	ReviewerAuthGroups     []model.AuthGroup          `json:"reviewer_auth_groups"`
	CandidateReviewers     []approvalResolutionUser   `json:"candidate_reviewers"`
	EffectiveReviewers     []approvalResolutionUser   `json:"effective_reviewers"`
	ExcludedReviewers      []approvalResolutionUser   `json:"excluded_reviewers"`
	MissingReviewerUserIDs []uint64                   `json:"missing_reviewer_user_ids"`
}

func (h *SettingsHandler) resolveApprovalPolicies(ctx context.Context, policies []model.ApprovalPolicy) ([]approvalResolutionWorkflow, error) {
	items := make([]approvalResolutionWorkflow, 0, len(policies))
	for _, policy := range policies {
		requiredPermissions := reviewPermissionsForWorkflow(policy.WorkflowType)
		candidatesByID := make(map[uint64]approvalResolutionUser)
		missingUserIDs := []uint64{}
		addSource := func(user model.User, source string) {
			item := candidatesByID[user.ID]
			if item.ID == 0 {
				item = approvalResolutionUser{ID: user.ID, Username: user.Username}
			}
			if !containsString(item.Sources, source) {
				item.Sources = append(item.Sources, source)
			}
			candidatesByID[user.ID] = item
		}

		for _, userID := range policy.ReviewerUserIDs {
			user, err := h.users.GetByID(ctx, userID)
			if err != nil {
				return nil, err
			}
			if user == nil {
				missingUserIDs = append(missingUserIDs, userID)
				continue
			}
			addSource(*user, "user")
		}
		for _, authGroup := range policy.ReviewerAuthGroups {
			users, err := h.users.ListUsersByAuthGroup(ctx, authGroup)
			if err != nil {
				return nil, err
			}
			for _, user := range users {
				addSource(user, "group:"+string(authGroup))
			}
		}

		candidateIDs := make([]uint64, 0, len(candidatesByID))
		for userID := range candidatesByID {
			candidateIDs = append(candidateIDs, userID)
		}
		sort.Slice(candidateIDs, func(i, j int) bool { return candidateIDs[i] < candidateIDs[j] })

		candidates := make([]approvalResolutionUser, 0, len(candidateIDs))
		effective := []approvalResolutionUser{}
		excluded := []approvalResolutionUser{}
		for _, userID := range candidateIDs {
			item := candidatesByID[userID]
			sort.Strings(item.Sources)
			candidates = append(candidates, item)

			user, err := h.users.GetByID(ctx, userID)
			if err != nil {
				return nil, err
			}
			if user == nil {
				item.Reason = "missing"
				excluded = append(excluded, item)
				continue
			}
			if !user.IsActive {
				item.Reason = "inactive"
				excluded = append(excluded, item)
				continue
			}
			permissions, err := h.users.GetEffectivePermissionKeys(ctx, userID)
			if err != nil {
				return nil, err
			}
			if !hasAnyString(permissions, requiredPermissions) {
				item.Reason = "missing_required_permission"
				excluded = append(excluded, item)
				continue
			}
			effective = append(effective, item)
		}

		items = append(items, approvalResolutionWorkflow{
			WorkflowType:           policy.WorkflowType,
			Enabled:                policy.Enabled,
			RequiredPermissions:    requiredPermissions,
			ReviewerUserIDs:        policy.ReviewerUserIDs,
			ReviewerAuthGroups:     policy.ReviewerAuthGroups,
			CandidateReviewers:     candidates,
			EffectiveReviewers:     effective,
			ExcludedReviewers:      excluded,
			MissingReviewerUserIDs: missingUserIDs,
		})
	}
	return items, nil
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func hasAnyString(items []string, targets []string) bool {
	for _, item := range items {
		for _, target := range targets {
			if item == target {
				return true
			}
		}
	}
	return false
}

func (h *SettingsHandler) validateConnectionExists(r *http.Request, connectionID uint64) error {
	conn, err := h.dbConns.GetByID(r.Context(), connectionID)
	if err != nil {
		return err
	}
	if conn == nil {
		return fmt.Errorf("db connection %d does not exist", connectionID)
	}
	return nil
}

func (h *SettingsHandler) validateAuthGroupExists(r *http.Request, authGroup model.AuthGroup) error {
	if h.auths == nil {
		return fmt.Errorf("auth group validation is not configured")
	}
	group, err := h.auths.GetByKey(r.Context(), string(authGroup))
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("auth group %s does not exist", authGroup)
	}
	return nil
}

type workflowRulePreviewSettings struct {
	rule model.WorkflowRule
}

func (s *workflowRulePreviewSettings) MatchWorkflowRule(ctx context.Context, ticketType model.TicketType, dbConnectionID *uint64, exportSensitivity *string) (*model.WorkflowRule, error) {
	_ = ctx
	if s.rule.TicketType != ticketType {
		return nil, nil
	}
	if !workflowConnectionMatches(s.rule.DBConnectionID, dbConnectionID) {
		return nil, nil
	}
	if !workflowSensitivityMatches(s.rule.ExportSensitivity, exportSensitivity) {
		return nil, nil
	}
	if s.rule.ID == 0 {
		s.rule.ID = 1
	}
	return &s.rule, nil
}

type workflowRuleSetPreviewSettings struct {
	rules []model.WorkflowRule
}

func (s *workflowRuleSetPreviewSettings) MatchWorkflowRule(ctx context.Context, ticketType model.TicketType, dbConnectionID *uint64, exportSensitivity *string) (*model.WorkflowRule, error) {
	_ = ctx
	var best *model.WorkflowRule
	bestScore := -1
	for i := range s.rules {
		rule := &s.rules[i]
		if !rule.Enabled || rule.TicketType != ticketType {
			continue
		}
		if !workflowConnectionMatches(rule.DBConnectionID, dbConnectionID) {
			continue
		}
		if !workflowSensitivityPreviewMatches(rule.ExportSensitivity, exportSensitivity) {
			continue
		}
		score := 0
		if rule.DBConnectionID != nil {
			score += 2
		}
		if rule.ExportSensitivity != nil {
			score++
		}
		if best == nil || score > bestScore || (score == bestScore && (rule.Priority < best.Priority || (rule.Priority == best.Priority && rule.ID < best.ID))) {
			copyRule := *rule
			best = &copyRule
			bestScore = score
		}
	}
	return best, nil
}

func withWorkflowPreviewIDs(rules []model.WorkflowRule) []model.WorkflowRule {
	next := make([]model.WorkflowRule, len(rules))
	copy(next, rules)
	maxID := uint64(0)
	for _, rule := range next {
		if rule.ID > maxID {
			maxID = rule.ID
		}
	}
	for i := range next {
		if next[i].ID == 0 {
			maxID++
			next[i].ID = maxID
		}
	}
	return next
}

func workflowConnectionMatches(ruleConnID *uint64, ticketConnID *uint64) bool {
	if ruleConnID == nil {
		return true
	}
	if ticketConnID == nil {
		return false
	}
	return *ruleConnID == *ticketConnID
}

func workflowSensitivityMatches(ruleSensitivity *string, ticketSensitivity *string) bool {
	if ruleSensitivity == nil && ticketSensitivity == nil {
		return true
	}
	if ruleSensitivity == nil || ticketSensitivity == nil {
		return false
	}
	return *ruleSensitivity == *ticketSensitivity
}

func workflowSensitivityPreviewMatches(ruleSensitivity *string, ticketSensitivity *string) bool {
	if ruleSensitivity == nil {
		return true
	}
	return ticketSensitivity != nil && *ruleSensitivity == *ticketSensitivity
}

func workflowRulesConflict(a model.WorkflowRule, b model.WorkflowRule) bool {
	if a.TicketType != b.TicketType || a.Priority != b.Priority {
		return false
	}
	if !uint64PtrEqual(a.DBConnectionID, b.DBConnectionID) {
		return false
	}
	return stringPtrEqual(a.ExportSensitivity, b.ExportSensitivity)
}

func uint64PtrEqual(a *uint64, b *uint64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func stringPtrEqual(a *string, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (h *SettingsHandler) validateWorkflowRules(ctx context.Context, rules []model.WorkflowRule) error {
	if len(rules) == 0 {
		return fmt.Errorf("workflow_rules is required")
	}
	for i, rule := range rules {
		if err := h.validateWorkflowRuleShape(ctx, rule); err != nil {
			return fmt.Errorf("workflow rule %d is invalid: %w", i+1, err)
		}
		if !rule.Enabled {
			continue
		}
		resolution, err := resolveWorkflowWithMatcher(ctx, &workflowRulePreviewSettings{rule: rule}, h.users, rule.TicketType, rule.DBConnectionID, rule.ExportSensitivity)
		if err != nil {
			return err
		}
		if resolution == nil || resolution.ErrorCode != "" {
			if resolution != nil && resolution.ErrorMessage != "" {
				return fmt.Errorf("workflow rule %q is invalid: %s", rule.RuleName, resolution.ErrorMessage)
			}
			return fmt.Errorf("workflow rule %q is invalid", rule.RuleName)
		}
	}
	return nil
}

func (h *SettingsHandler) validateWorkflowRuleShape(ctx context.Context, rule model.WorkflowRule) error {
	if strings.TrimSpace(rule.RuleName) == "" {
		return fmt.Errorf("rule_name is required")
	}
	rule.ExecutionMode = normalizeWorkflowExecutionMode(rule.ExecutionMode)
	if rule.TicketType == "" {
		return fmt.Errorf("ticket_type is required")
	}
	if h.appEnv == "production" && !rule.ApprovalEnabled {
		return fmt.Errorf("approval_enabled cannot be disabled in production")
	}
	if rule.ExecutionMode == workflowExecutionModeAutoApproval {
		if h.appEnv == "production" {
			return fmt.Errorf("auto_after_approval execution mode is not allowed in production")
		}
		if rule.TicketType != model.TicketTypeDDL && rule.TicketType != model.TicketTypeDML {
			return fmt.Errorf("auto_after_approval execution mode is only allowed for ddl and dml workflow rules")
		}
	}
	if rule.DBConnectionID != nil {
		conn, err := h.dbConns.GetByID(ctx, *rule.DBConnectionID)
		if err != nil {
			return err
		}
		if conn == nil {
			return fmt.Errorf("db connection %d does not exist", *rule.DBConnectionID)
		}
		if err := validateTicketDBType(rule.TicketType, conn.DBType); err != nil {
			return err
		}
	}
	if rule.TicketType == model.TicketTypeSQLExport {
		if rule.ExportSensitivity == nil || (*rule.ExportSensitivity != "normal" && *rule.ExportSensitivity != "sensitive") {
			return fmt.Errorf("sql_export workflow rule requires export_sensitivity normal or sensitive")
		}
	} else if rule.ExportSensitivity != nil {
		return fmt.Errorf("export_sensitivity is only allowed for sql_export workflow rules")
	}
	for _, group := range append(append([]model.AuthGroup{}, rule.ApprovalAuthGroups...), rule.ExecutorAuthGroups...) {
		if err := h.validateAuthGroupExistsContext(ctx, group); err != nil {
			return err
		}
	}
	return nil
}

func (h *SettingsHandler) validateAuthGroupExistsContext(ctx context.Context, authGroup model.AuthGroup) error {
	if h.auths == nil {
		return fmt.Errorf("auth group validation is not configured")
	}
	group, err := h.auths.GetByKey(ctx, string(authGroup))
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("auth group %s does not exist", authGroup)
	}
	return nil
}
