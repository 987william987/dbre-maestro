package handler

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/dbre-maestro/maestro/internal/timeutil"
)

type dashboardTicketSummary struct {
	Total     int64                               `json:"total"`
	Completed int64                               `json:"completed"`
	Failed    int64                               `json:"failed"`
	Active    int64                               `json:"active"`
	ByType    []repository.WorkflowDashboardCount `json:"by_type"`
	ByStatus  []repository.WorkflowDashboardCount `json:"by_status"`
}

type dashboardAccessScope struct {
	ID              uint64     `json:"id"`
	ConnectionID    uint64     `json:"connection_id"`
	ConnectionName  string     `json:"connection_name"`
	SubjectType     string     `json:"subject_type"`
	Effect          string     `json:"effect"`
	DatabasePattern string     `json:"database_pattern"`
	TablePattern    string     `json:"table_pattern"`
	GrantedVia      string     `json:"granted_via"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	RemainingDays   *int       `json:"remaining_days,omitempty"`
	SourceTicketID  *uint64    `json:"source_ticket_id,omitempty"`
	SourceTicketNo  *string    `json:"source_ticket_no,omitempty"`
	ExpiringSoon    bool       `json:"expiring_soon"`
	RenewTicketPath string     `json:"renew_ticket_path"`
}

type dashboardDBScope struct {
	ID     uint64 `json:"id"`
	Name   string `json:"name"`
	DBType string `json:"db_type"`
}

type dashboardDBMetadataHealth struct {
	DBTypeCounts                   []repository.WorkflowDashboardCount `json:"db_type_counts"`
	EnabledMetadataConnectionCount int                                 `json:"enabled_metadata_connection_count"`
	ObjectSnapshotConnectionCount  int64                               `json:"object_snapshot_connection_count"`
	StaleObjectConnectionCount     int64                               `json:"stale_object_connection_count"`
	ObjectCount                    int64                               `json:"object_count"`
	InventoryJob                   *model.DBMetadataJobRun             `json:"inventory_job,omitempty"`
	ObjectJob                      *model.DBMetadataJobRun             `json:"object_job,omitempty"`
	ObjectSyncFailed               bool                                `json:"object_sync_failed"`
}

type dashboardPlatform struct {
	TicketSummary        dashboardTicketSummary                `json:"ticket_summary"`
	Queue                repository.PlatformQueueStats         `json:"queue"`
	Aging                repository.PlatformAgingStats         `json:"aging"`
	ExecutionRisk        repository.PlatformExecutionRiskStats `json:"execution_risk"`
	AccessGovernance     repository.AccessGovernanceStats      `json:"access_governance"`
	DBMetadataHealth     dashboardDBMetadataHealth             `json:"db_metadata_health"`
	NotificationHealth   repository.NotificationHealthStats    `json:"notification_health"`
	TopUsage             repository.PlatformTopUsageStats      `json:"top_usage"`
	RecentAttention      []ticketResponse                      `json:"recent_attention"`
	LongPendingTickets   []ticketResponse                      `json:"long_pending_tickets"`
	RecentFailedTickets  []ticketResponse                      `json:"recent_failed_tickets"`
	DBConnectionFailures []model.DBConnection                  `json:"db_connection_failures"`
}

// GET /account/access-scopes
func (h *TicketHandler) AccountAccessScopes(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())
	dbScopes, err := h.dashboardDBScopes(r.Context(), userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load account db scopes failed")
		return
	}
	queryScopes, err := h.accountQueryAccessScopes(r.Context(), userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load account query access scopes failed")
		return
	}
	jsonOK(w, map[string]any{
		"db_scopes":           dbScopes,
		"query_access_scopes": queryScopes,
		"db_scope_count":      len(dbScopes),
		"query_scope_count":   len(queryScopes),
	})
}

// GET /dashboard
func (h *TicketHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())
	fullQueueVisible, err := h.canViewFullTicketQueue(r.Context(), userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "dashboard access check failed")
		return
	}

	personalSummary, err := h.dashboardTicketSummary(r.Context(), &userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load dashboard ticket summary failed")
		return
	}
	recentTickets, err := h.tickets.RecentTicketsBySubmitter(r.Context(), userID, 6)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load dashboard tickets failed")
		return
	}
	recentTicketResponses, err := h.enrichDashboardTickets(r.Context(), recentTickets)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load dashboard tickets failed")
		return
	}
	activeTickets, err := h.tickets.ActiveTicketsBySubmitter(r.Context(), userID, 6)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load dashboard tickets failed")
		return
	}
	activeTicketResponses, err := h.enrichDashboardTickets(r.Context(), activeTickets)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load dashboard tickets failed")
		return
	}
	dbScopes, err := h.dashboardDBScopes(r.Context(), userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load dashboard db scopes failed")
		return
	}
	queryScopes, err := h.dashboardQueryAccessScopes(r.Context(), userID, 20)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load dashboard query access scopes failed")
		return
	}

	var platform *dashboardPlatform
	if fullQueueVisible {
		platformSummary, err := h.dashboardTicketSummary(r.Context(), nil)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "load dashboard platform summary failed")
			return
		}
		queueStats, err := h.tickets.PlatformQueueStats(r.Context(), timeutil.NowUTC().Add(-24*time.Hour))
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "load dashboard platform queue failed")
			return
		}
		agingStats, err := h.tickets.PlatformAgingStats(r.Context())
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "load dashboard platform aging failed")
			return
		}
		executionRisk, err := h.tickets.PlatformExecutionRiskStats(r.Context())
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "load dashboard execution risk failed")
			return
		}
		accessGovernance, err := h.dashboardAccessGovernance(r.Context())
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "load dashboard access governance failed")
			return
		}
		notificationHealth, err := h.dashboardNotificationHealth(r.Context())
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "load dashboard notification health failed")
			return
		}
		topUsage, err := h.tickets.PlatformTopUsageStats(r.Context())
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "load dashboard top usage failed")
			return
		}
		attentionTickets, err := h.tickets.RecentPlatformAttentionTickets(r.Context(), 8)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "load dashboard platform tickets failed")
			return
		}
		attentionTicketResponses, err := h.enrichDashboardTickets(r.Context(), attentionTickets)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "load dashboard platform tickets failed")
			return
		}
		longPendingTickets, err := h.tickets.LongPendingTickets(r.Context(), timeutil.NowUTC().Add(-24*time.Hour), 8)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "load dashboard long pending tickets failed")
			return
		}
		longPendingResponses, err := h.enrichDashboardTickets(r.Context(), longPendingTickets)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "load dashboard long pending tickets failed")
			return
		}
		failedTickets, err := h.tickets.RecentFailedExecutionTickets(r.Context(), 8)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "load dashboard failed tickets failed")
			return
		}
		failedTicketResponses, err := h.enrichDashboardTickets(r.Context(), failedTickets)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "load dashboard failed tickets failed")
			return
		}
		dbFailures, err := h.dashboardDBConnectionFailures(r.Context())
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "load dashboard db health failed")
			return
		}
		dbMetadataHealth, err := h.dashboardDBMetadataHealth(r.Context())
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "load dashboard metadata health failed")
			return
		}
		platform = &dashboardPlatform{
			TicketSummary:        platformSummary,
			Queue:                *queueStats,
			Aging:                *agingStats,
			ExecutionRisk:        *executionRisk,
			AccessGovernance:     *accessGovernance,
			DBMetadataHealth:     dbMetadataHealth,
			NotificationHealth:   *notificationHealth,
			TopUsage:             *topUsage,
			RecentAttention:      attentionTicketResponses,
			LongPendingTickets:   longPendingResponses,
			RecentFailedTickets:  failedTicketResponses,
			DBConnectionFailures: dbFailures,
		}
	}

	jsonOK(w, map[string]any{
		"personal": map[string]any{
			"ticket_summary":      personalSummary,
			"active_tickets":      activeTicketResponses,
			"recent_tickets":      recentTicketResponses,
			"db_scopes":           dbScopes,
			"query_access_scopes": queryScopes,
		},
		"platform": platform,
	})
}

func (h *TicketHandler) dashboardTicketSummary(ctx context.Context, submitterID *uint64) (dashboardTicketSummary, error) {
	summary, err := h.tickets.TicketDashboardSummary(ctx, submitterID)
	if err != nil {
		return dashboardTicketSummary{}, err
	}
	return buildDashboardTicketSummary(summary), nil
}

func buildDashboardTicketSummary(summary *repository.TicketDashboardSummary) dashboardTicketSummary {
	if summary == nil {
		return dashboardTicketSummary{}
	}
	result := dashboardTicketSummary{
		Total:    summary.Total,
		ByType:   summary.ByType,
		ByStatus: summary.ByStatus,
	}
	for _, item := range summary.ByStatus {
		switch model.TicketStatus(item.Key) {
		case model.TicketStatusCompleted:
			result.Completed += item.Count
		case model.TicketStatusFailed, model.TicketStatusStopped, model.TicketStatusInterrupted, model.TicketStatusRejected:
			result.Failed += item.Count
		case model.TicketStatusPendingReview, model.TicketStatusPendingExecution, model.TicketStatusExecuting, model.TicketStatusNeedsAdminAttention:
			result.Active += item.Count
		}
	}
	return result
}

func (h *TicketHandler) enrichDashboardTickets(ctx context.Context, tickets []model.Ticket) ([]ticketResponse, error) {
	responses := make([]ticketResponse, 0, len(tickets))
	for _, ticket := range tickets {
		t := ticket
		enriched, err := h.buildTicketResponse(ctx, &t)
		if err != nil {
			return nil, err
		}
		responses = append(responses, enriched)
	}
	return responses, nil
}

func (h *TicketHandler) dashboardDBScopes(ctx context.Context, userID uint64) ([]dashboardDBScope, error) {
	if h.users == nil || h.dbConns == nil {
		return []dashboardDBScope{}, nil
	}
	ids, err := h.users.GetEffectiveDBConnectionIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	conns, err := h.dbConns.List(ctx)
	if err != nil {
		return nil, err
	}
	allowed := map[uint64]bool{}
	for _, id := range ids {
		allowed[id] = true
	}
	scopes := make([]dashboardDBScope, 0)
	for _, conn := range conns {
		if allowed[conn.ID] {
			scopes = append(scopes, dashboardDBScope{ID: conn.ID, Name: conn.Name, DBType: conn.DBType})
		}
	}
	return scopes, nil
}

func (h *TicketHandler) dashboardQueryAccessScopes(ctx context.Context, userID uint64, limit int) ([]dashboardAccessScope, error) {
	if h.queryAccess == nil {
		return []dashboardAccessScope{}, nil
	}
	authGroupIDs := []uint64{}
	if h.users != nil {
		ids, err := h.users.GetEffectiveAuthGroupIDs(ctx, userID)
		if err != nil {
			return nil, err
		}
		authGroupIDs = ids
	}
	rules, err := h.queryAccess.ListActiveRulesForSubjects(ctx, userID, authGroupIDs, limit)
	if err != nil {
		return nil, err
	}
	connectionNames, err := h.dashboardConnectionNames(ctx)
	if err != nil {
		return nil, err
	}
	now := timeutil.NowUTC()
	scopes := make([]dashboardAccessScope, 0, len(rules))
	for _, rule := range rules {
		var remainingDays *int
		expiringSoon := false
		if rule.ExpiresAt != nil {
			days := int(rule.ExpiresAt.Sub(now).Hours() / 24)
			if days < 0 {
				days = 0
			}
			remainingDays = &days
			expiringSoon = rule.ExpiresAt.Before(now.Add(7 * 24 * time.Hour))
		}
		scopes = append(scopes, dashboardAccessScope{
			ID:              rule.ID,
			ConnectionID:    rule.ConnectionID,
			ConnectionName:  connectionNames[rule.ConnectionID],
			SubjectType:     string(rule.SubjectType),
			Effect:          string(rule.Effect),
			DatabasePattern: rule.DatabasePattern,
			TablePattern:    rule.TablePattern,
			GrantedVia:      rule.GrantedVia,
			ExpiresAt:       rule.ExpiresAt,
			RemainingDays:   remainingDays,
			SourceTicketID:  rule.SourceTicketID,
			SourceTicketNo:  rule.SourceTicketNo,
			ExpiringSoon:    expiringSoon,
			RenewTicketPath: dashboardRenewTicketPath(rule),
		})
	}
	return scopes, nil
}

func (h *TicketHandler) accountQueryAccessScopes(ctx context.Context, userID uint64) ([]dashboardAccessScope, error) {
	if h.queryAccess == nil {
		return []dashboardAccessScope{}, nil
	}
	authGroupIDs := []uint64{}
	if h.users != nil {
		ids, err := h.users.GetEffectiveAuthGroupIDs(ctx, userID)
		if err != nil {
			return nil, err
		}
		authGroupIDs = ids
	}
	rules, err := h.queryAccess.ListAllActiveRulesForSubjects(ctx, userID, authGroupIDs)
	if err != nil {
		return nil, err
	}
	return h.dashboardAccessScopesFromRules(ctx, rules)
}

func (h *TicketHandler) dashboardAccessScopesFromRules(ctx context.Context, rules []model.QueryAccessRule) ([]dashboardAccessScope, error) {
	connectionNames, err := h.dashboardConnectionNames(ctx)
	if err != nil {
		return nil, err
	}
	now := timeutil.NowUTC()
	scopes := make([]dashboardAccessScope, 0, len(rules))
	for _, rule := range rules {
		var remainingDays *int
		expiringSoon := false
		if rule.ExpiresAt != nil {
			days := int(rule.ExpiresAt.Sub(now).Hours() / 24)
			if days < 0 {
				days = 0
			}
			remainingDays = &days
			expiringSoon = rule.ExpiresAt.Before(now.Add(7 * 24 * time.Hour))
		}
		scopes = append(scopes, dashboardAccessScope{
			ID:              rule.ID,
			ConnectionID:    rule.ConnectionID,
			ConnectionName:  connectionNames[rule.ConnectionID],
			SubjectType:     string(rule.SubjectType),
			Effect:          string(rule.Effect),
			DatabasePattern: rule.DatabasePattern,
			TablePattern:    rule.TablePattern,
			GrantedVia:      rule.GrantedVia,
			ExpiresAt:       rule.ExpiresAt,
			RemainingDays:   remainingDays,
			SourceTicketID:  rule.SourceTicketID,
			SourceTicketNo:  rule.SourceTicketNo,
			ExpiringSoon:    expiringSoon,
			RenewTicketPath: dashboardRenewTicketPath(rule),
		})
	}
	return scopes, nil
}

func (h *TicketHandler) dashboardConnectionNames(ctx context.Context) (map[uint64]string, error) {
	names := map[uint64]string{}
	if h.dbConns == nil {
		return names, nil
	}
	conns, err := h.dbConns.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, conn := range conns {
		names[conn.ID] = conn.Name
	}
	return names, nil
}

func (h *TicketHandler) dashboardDBConnectionFailures(ctx context.Context) ([]model.DBConnection, error) {
	if h.dbConns == nil {
		return []model.DBConnection{}, nil
	}
	conns, err := h.dbConns.List(ctx)
	if err != nil {
		return nil, err
	}
	failures := make([]model.DBConnection, 0)
	for _, conn := range conns {
		if conn.LastTestStatus != nil && *conn.LastTestStatus == "failed" {
			failures = append(failures, conn)
		}
	}
	return failures, nil
}

func (h *TicketHandler) dashboardAccessGovernance(ctx context.Context) (*repository.AccessGovernanceStats, error) {
	if h.queryAccess == nil {
		return &repository.AccessGovernanceStats{}, nil
	}
	return h.queryAccess.AccessGovernanceStats(ctx)
}

func (h *TicketHandler) dashboardNotificationHealth(ctx context.Context) (*repository.NotificationHealthStats, error) {
	if h.notifRepo == nil {
		return &repository.NotificationHealthStats{}, nil
	}
	return h.notifRepo.HealthStats(ctx)
}

func (h *TicketHandler) dashboardDBMetadataHealth(ctx context.Context) (dashboardDBMetadataHealth, error) {
	health := dashboardDBMetadataHealth{DBTypeCounts: []repository.WorkflowDashboardCount{}}
	conns := []model.DBConnection{}
	if h.dbConns != nil {
		loaded, err := h.dbConns.List(ctx)
		if err != nil {
			return health, err
		}
		conns = loaded
		byType := map[string]int64{}
		for _, conn := range conns {
			byType[conn.DBType]++
		}
		for dbType, count := range byType {
			health.DBTypeCounts = append(health.DBTypeCounts, repository.WorkflowDashboardCount{Key: dbType, Count: count})
		}
	}

	staleBefore := timeutil.NowUTC().Add(-2 * time.Hour)
	if h.settings != nil {
		settings, err := h.settings.Get(ctx)
		if err != nil {
			return health, err
		}
		if settings.DBMetadataObjectEnabled {
			health.EnabledMetadataConnectionCount = len(settings.DBMetadataObjectEnabledConnectionIDs)
		}
		interval := settings.DBMetadataObjectSyncIntervalMins
		if interval <= 0 {
			interval = 60
		}
		staleBefore = timeutil.NowUTC().Add(-2 * time.Duration(interval) * time.Minute)
	}

	if h.dbMetadata == nil {
		return health, nil
	}
	objectStats, err := h.dbMetadata.ObjectHealthStats(ctx, staleBefore)
	if err != nil {
		return health, err
	}
	health.ObjectSnapshotConnectionCount = objectStats.SnapshotConnectionCount
	health.StaleObjectConnectionCount = objectStats.StaleConnectionCount
	health.ObjectCount = objectStats.ObjectCount
	inventoryJob, err := h.dbMetadata.GetJobRun(ctx, "db_metadata_inventory")
	if err != nil {
		return health, err
	}
	objectJob, err := h.dbMetadata.GetJobRun(ctx, "db_metadata_object")
	if err != nil {
		return health, err
	}
	health.InventoryJob = inventoryJob
	health.ObjectJob = objectJob
	health.ObjectSyncFailed = objectJob != nil && objectJob.Status == "failed"
	return health, nil
}

func dashboardRenewTicketPath(rule model.QueryAccessRule) string {
	return "/tickets/new?ticket_type=query_access&db_connection_id=" + strconv.FormatUint(rule.ConnectionID, 10) +
		"&database_name=" + url.QueryEscape(rule.DatabasePattern) +
		"&table_name=" + url.QueryEscape(rule.TablePattern)
}
