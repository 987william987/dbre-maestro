package handler

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dbre-maestro/maestro/internal/masking"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/notification"
	"github.com/dbre-maestro/maestro/internal/pool"
	"github.com/dbre-maestro/maestro/internal/queryaccess"
	"github.com/dbre-maestro/maestro/internal/realtime"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/dbre-maestro/maestro/internal/sqlparse"
	"github.com/dbre-maestro/maestro/internal/sqlreview"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
)

const (
	defaultQueryLimit         = 200
	maxQueryLimit             = 1000
	defaultQueryTimeout       = 30 * time.Second
	queryHistoryRetentionDays = 90
	defaultQueryHistoryLimit  = 20
	maxQueryHistoryLimit      = 100
)

type sqlEditorTimeoutSettings struct {
	AppTimeout                 time.Duration
	MySQLMaxExecutionTimeMs    int
	PostgresStatementTimeoutMs int
}

func defaultSQLEditorTimeoutSettings() sqlEditorTimeoutSettings {
	return sqlEditorTimeoutSettings{
		AppTimeout:                 defaultQueryTimeout,
		MySQLMaxExecutionTimeMs:    25000,
		PostgresStatementTimeoutMs: 25000,
	}
}

func writeQueryExecutionError(w http.ResponseWriter, err error, operation string, timeout time.Duration) {
	if errors.Is(err, context.DeadlineExceeded) {
		jsonErr(w, http.StatusGatewayTimeout, fmt.Sprintf("%s timed out after %s", operation, timeout))
		return
	}
	if errors.Is(err, context.Canceled) {
		jsonErr(w, http.StatusRequestTimeout, fmt.Sprintf("%s was cancelled", operation))
		return
	}
	jsonErr(w, http.StatusUnprocessableEntity, fmt.Sprintf("%s failed: %s", operation, err.Error()))
}

type QueryHandler struct {
	dbConns       *repository.DBConnectionRepo
	users         *repository.UserRepo
	maskingRules  *repository.MaskingRuleRepo
	audit         *repository.AuditRepo
	artifacts     *repository.QueryArtifactRepo
	tickets       *repository.TicketRepo
	redisPrefixes *repository.RedisSensitiveKeyPrefixRepo
	settings      *repository.SettingsRepo
	queryAccess   *queryaccess.Service
	masking       *maskingRuntime
	notifRepo     *repository.NotificationRepo
	broker        *realtime.Broker
	lark          *notification.Dispatcher
	notifications *NotificationRouter
	appBaseURL    string
	jwtSecret     []byte
	activeQueries *activeSQLQueryRegistry
}

type sqlEditorConstraintsResponse struct {
	DefaultLimit               int `json:"default_limit"`
	MaxLimit                   int `json:"max_limit"`
	AppTimeoutSeconds          int `json:"app_timeout_seconds"`
	MySQLMaxExecutionTimeMs    int `json:"mysql_max_execution_time_ms"`
	PostgresStatementTimeoutMs int `json:"postgres_statement_timeout_ms"`
}

type queryExecutionContext struct {
	DatabaseName string
	SchemaName   string
	RedisDBIndex *int
}

type sqlCancelDBOpener func(ctx context.Context) (*sql.DB, string, func(), error)

type sqlQueryExecutionOptions struct {
	QueryExecutionID string
	UserID           uint64
	Registry         *activeSQLQueryRegistry
	CancelDBOpener   sqlCancelDBOpener
}

type activeSQLQuery struct {
	UserID         uint64
	ConnectionID   uint64
	TicketID       uint64
	DBType         string
	MySQLThreadID  uint64
	PostgresPID    uint64
	Statement      string
	Conn           *model.DBConnection
	CancelDBOpener sqlCancelDBOpener
	Cancel         context.CancelFunc
	RegisteredAt   time.Time
}

type pendingSQLQueryCancel struct {
	UserID    uint64
	CreatedAt time.Time
}

type activeSQLQueryRegistry struct {
	mu             sync.Mutex
	queries        map[string]activeSQLQuery
	pendingCancels map[string]pendingSQLQueryCancel
}

func newActiveSQLQueryRegistry() *activeSQLQueryRegistry {
	return &activeSQLQueryRegistry{
		queries:        make(map[string]activeSQLQuery),
		pendingCancels: make(map[string]pendingSQLQueryCancel),
	}
}

func (r *activeSQLQueryRegistry) register(queryID string, query activeSQLQuery) bool {
	if r == nil || strings.TrimSpace(queryID) == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(time.Now())
	if pending, ok := r.pendingCancels[queryID]; ok && (pending.UserID == 0 || pending.UserID == query.UserID) {
		delete(r.pendingCancels, queryID)
		return true
	}
	r.queries[queryID] = query
	return false
}

func (r *activeSQLQueryRegistry) remove(queryID string) {
	if r == nil || strings.TrimSpace(queryID) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.queries, queryID)
}

func (r *activeSQLQueryRegistry) cancel(queryID string, userID uint64) (activeSQLQuery, bool) {
	if r == nil || strings.TrimSpace(queryID) == "" {
		return activeSQLQuery{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(time.Now())
	query, ok := r.queries[queryID]
	if ok && query.UserID == userID {
		delete(r.queries, queryID)
		return query, true
	}
	if !ok {
		r.pendingCancels[queryID] = pendingSQLQueryCancel{UserID: userID, CreatedAt: time.Now()}
	}
	return activeSQLQuery{}, false
}

func (r *activeSQLQueryRegistry) cancelAny(queryID string) (activeSQLQuery, bool) {
	if r == nil || strings.TrimSpace(queryID) == "" {
		return activeSQLQuery{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(time.Now())
	query, ok := r.queries[queryID]
	if ok {
		delete(r.queries, queryID)
	}
	return query, ok
}

func (r *activeSQLQueryRegistry) cancelAnyOrPending(queryID string) (activeSQLQuery, bool) {
	if r == nil || strings.TrimSpace(queryID) == "" {
		return activeSQLQuery{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(time.Now())
	query, ok := r.queries[queryID]
	if ok {
		delete(r.queries, queryID)
		return query, true
	}
	r.pendingCancels[queryID] = pendingSQLQueryCancel{UserID: 0, CreatedAt: time.Now()}
	return activeSQLQuery{}, false
}

func (r *activeSQLQueryRegistry) cancelAll() map[string]activeSQLQuery {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(time.Now())
	queries := make(map[string]activeSQLQuery, len(r.queries))
	for queryID, query := range r.queries {
		queries[queryID] = query
	}
	r.queries = make(map[string]activeSQLQuery)
	return queries
}

func (r *activeSQLQueryRegistry) pruneLocked(now time.Time) {
	for queryID, pending := range r.pendingCancels {
		if now.Sub(pending.CreatedAt) > 2*time.Minute {
			delete(r.pendingCancels, queryID)
		}
	}
}

func NewQueryHandler(
	dbConns *repository.DBConnectionRepo,
	users *repository.UserRepo,
	maskingRules *repository.MaskingRuleRepo,
	audit *repository.AuditRepo,
	artifacts *repository.QueryArtifactRepo,
	tickets *repository.TicketRepo,
	redisPrefixes *repository.RedisSensitiveKeyPrefixRepo,
	settings *repository.SettingsRepo,
	queryAccessRepo *repository.QueryAccessRepo,
	engine *masking.Engine,
	whitelist *repository.MaskingWhitelistRepo,
	notifRepo *repository.NotificationRepo,
	broker *realtime.Broker,
	lark *notification.Dispatcher,
	appBaseURL string,
	jwtSecret []byte,
) *QueryHandler {
	return &QueryHandler{
		dbConns:       dbConns,
		users:         users,
		maskingRules:  maskingRules,
		audit:         audit,
		artifacts:     artifacts,
		tickets:       tickets,
		redisPrefixes: redisPrefixes,
		settings:      settings,
		queryAccess:   queryaccess.NewService(queryAccessRepo, users),
		masking:       newMaskingRuntime(users, maskingRules, whitelist, tickets, engine),
		notifRepo:     notifRepo,
		broker:        broker,
		lark:          lark,
		notifications: NewNotificationRouter(notifRepo, audit, users, broker, lark),
		appBaseURL:    strings.TrimRight(appBaseURL, "/"),
		jwtSecret:     append([]byte(nil), jwtSecret...),
		activeQueries: newActiveSQLQueryRegistry(),
	}
}

func (h *QueryHandler) Constraints(w http.ResponseWriter, r *http.Request) {
	timeoutSettings := h.loadSQLEditorTimeoutSettings(r.Context())
	jsonOK(w, sqlEditorConstraintsResponse{
		DefaultLimit:               defaultQueryLimit,
		MaxLimit:                   maxQueryLimit,
		AppTimeoutSeconds:          int(timeoutSettings.AppTimeout / time.Second),
		MySQLMaxExecutionTimeMs:    timeoutSettings.MySQLMaxExecutionTimeMs,
		PostgresStatementTimeoutMs: timeoutSettings.PostgresStatementTimeoutMs,
	})
}

func (h *QueryHandler) loadSQLEditorTimeoutSettings(ctx context.Context) sqlEditorTimeoutSettings {
	settings := defaultSQLEditorTimeoutSettings()
	if h.settings == nil {
		return settings
	}

	platformSettings, err := h.settings.Get(ctx)
	if err != nil || platformSettings == nil {
		return settings
	}
	if platformSettings.SQLEditorAppTimeoutSeconds > 0 {
		settings.AppTimeout = time.Duration(platformSettings.SQLEditorAppTimeoutSeconds) * time.Second
	}
	if platformSettings.SQLEditorMySQLMaxExecutionTimeMs > 0 {
		settings.MySQLMaxExecutionTimeMs = platformSettings.SQLEditorMySQLMaxExecutionTimeMs
	}
	if platformSettings.SQLEditorPostgresStatementTimeoutMs > 0 {
		settings.PostgresStatementTimeoutMs = platformSettings.SQLEditorPostgresStatementTimeoutMs
	}
	return settings
}

func (h *QueryHandler) ticketLink(ticketNo string) string {
	path := fmt.Sprintf("/tickets/%s", ticketNo)
	if h.appBaseURL == "" {
		return path
	}
	return h.appBaseURL + path
}

// GET /query/connections
func (h *QueryHandler) ListConnections(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())
	connections, err := listAccessibleConnections(r.Context(), h.dbConns, h.users, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list query connections failed")
		return
	}
	jsonOK(w, map[string]any{"connections": connections})
}

// POST /query
// Body: { "db_connection_id": 1, "sql": "SELECT ...", "limit": 200 }
// Returns: { "columns": [...], "rows": [[...]], "row_count": N, "duration_ms": N }
func (h *QueryHandler) Execute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DBConnectionID   uint64 `json:"db_connection_id"`
		SQL              string `json:"sql"`
		Limit            int    `json:"limit"`
		Database         string `json:"database"`
		Schema           string `json:"schema"`
		RedisDBIndex     *int   `json:"redis_db_index"`
		QueryExecutionID string `json:"query_execution_id"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DBConnectionID == 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "db_connection_id is required")
		return
	}
	if req.SQL == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "sql is required")
		return
	}

	limit := defaultQueryLimit
	if req.Limit > 0 && req.Limit <= maxQueryLimit {
		limit = req.Limit
	} else if req.Limit > maxQueryLimit {
		limit = maxQueryLimit
	}

	conn, err := h.dbConns.GetByID(r.Context(), req.DBConnectionID)
	if err != nil || conn == nil {
		jsonErr(w, http.StatusNotFound, "db connection not found")
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	hasAccess, err := h.userCanAccessConnection(r.Context(), userID, req.DBConnectionID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "db scope check failed")
		return
	}
	if !hasAccess {
		jsonErr(w, http.StatusForbidden, "access to this connection is not allowed")
		return
	}

	// Redis takes a completely different path
	if conn.DBType == "redis" {
		h.executeRedis(w, r, conn, req.SQL, queryExecutionContext{
			DatabaseName: strings.TrimSpace(req.Database),
			RedisDBIndex: req.RedisDBIndex,
		})
		return
	}

	queryCtx := queryExecutionContext{
		DatabaseName: strings.TrimSpace(req.Database),
		SchemaName:   queryContextSchemaName(conn, req.Schema),
	}
	if err := h.queryAccess.CheckSQL(r.Context(), userID, conn, req.SQL, queryaccess.CheckContext{
		DatabaseName: queryCtx.DatabaseName,
		SchemaName:   queryCtx.SchemaName,
	}); err != nil {
		if missingErr, ok := err.(*queryaccess.MissingAccessError); ok {
			h.auditBlockedQuery(r, userID, conn.ID, req.SQL, "query_access_policy", map[string]any{
				"database": strings.TrimSpace(req.Database),
				"schema":   queryCtx.SchemaName,
				"missing":  missingErr.Missing,
			})
			jsonErr(w, http.StatusForbidden, missingErr.Error())
			return
		}
		slog.Error("query access check failed", "user_id", userID, "connection_id", conn.ID, "err", err)
		jsonErr(w, http.StatusUnprocessableEntity, "Query access is temporarily unavailable. Please try again later.")
		return
	}

	// Whitelist check: only SELECT/SHOW/EXPLAIN/DESC/WITH
	if err := sqlreview.CheckReadOnly(sqlparse.DialectFromDBType(conn.DBType), req.SQL); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "only read-only SQL is allowed: "+err.Error())
		return
	}

	resolvedConn, password, err := h.dbConns.ResolveCredential(conn, model.DBCredentialRoleReadonly)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	driver, dsn := pool.BuildDSN(resolvedConn, password)
	pools, err := pool.Global().GetOrCreate(conn.ID, driver, dsn)
	if err != nil {
		jsonErr(w, http.StatusServiceUnavailable, "cannot connect to database: "+err.Error())
		return
	}

	timeoutSettings := h.loadSQLEditorTimeoutSettings(r.Context())
	ctx, cancel := context.WithTimeout(r.Context(), timeoutSettings.AppTimeout)
	defer cancel()

	// Inject LIMIT if not present (simple heuristic for SELECT statements)
	execSQL := injectLimit(req.SQL, limit, conn.DBType)
	start := time.Now()
	cancelDBOpener := h.readonlyCancelDBOpener(conn)
	result, err := executeQueryForConnection(ctx, resolvedConn, password, pools.QueryPool, execSQL, queryCtx, timeoutSettings, sqlQueryExecutionOptions{
		QueryExecutionID: strings.TrimSpace(req.QueryExecutionID),
		UserID:           userID,
		Registry:         h.activeQueries,
		CancelDBOpener:   cancelDBOpener,
	})
	if err != nil {
		logSQLEditorQueryError(ctx, userID, conn, queryCtx, req.SQL, time.Since(start), timeoutSettings, err)
		writeQueryExecutionError(w, err, "query", timeoutSettings.AppTimeout)
		return
	}
	durationMs := time.Since(start).Milliseconds()

	decisions, sensitiveColumnIndexes, err := h.masking.analyzeSensitiveColumns(r.Context(), resolvedConn, result)
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "masking failed")
		return
	}
	sensitiveOverrideActive, sensitiveColumnIndexes, err := h.masking.applyAnalyzedResult(r.Context(), resolvedConn, userID, result, decisions, sensitiveColumnIndexes)
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "masking failed")
		return
	}
	queryContextToken, err := newQueryContextToken(h.jwtSecret, userID, conn.ID, req.SQL, queryCtx.DatabaseName, queryCtx.SchemaName, buildSQLScopeAnalysisFromDecisions(conn.ID, decisions))
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create query context failed")
		return
	}

	auditDetails := map[string]any{
		"sql":         truncate(req.SQL, 500),
		"row_count":   len(result.Rows),
		"duration_ms": durationMs,
	}
	addAuditConnectionDetails(auditDetails, conn)
	addAuditQueryContextDetails(auditDetails, queryCtx)
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "query_execute",
		ResourceType: "db_connection",
		ResourceID:   &req.DBConnectionID,
		Details:      auditDetails,
		IPAddress:    clientIP(r),
	})

	if h.artifacts != nil {
		rowCount := len(result.Rows)
		_, _ = h.artifacts.AddHistory(r.Context(), &model.QueryHistoryEntry{
			UserID:           userID,
			DBConnectionID:   req.DBConnectionID,
			DBConnectionName: conn.Name,
			DatabaseName:     optionalTrimmedString(req.Database),
			SchemaName:       optionalTrimmedString(req.Schema),
			RedisDBIndex:     req.RedisDBIndex,
			SQLContent:       req.SQL,
			RowCount:         &rowCount,
			DurationMs:       durationMs,
		})
	}

	jsonOK(w, map[string]any{
		"columns":                   result.Columns,
		"raw_columns":               result.RawColumns,
		"rows":                      result.Rows,
		"row_count":                 len(result.Rows),
		"duration_ms":               durationMs,
		"sensitive_column_indexes":  sensitiveColumnIndexes,
		"sensitive_override_active": sensitiveOverrideActive,
		"query_context_token":       queryContextToken,
	})
}

func (h *QueryHandler) readonlyCancelDBOpener(conn *model.DBConnection) sqlCancelDBOpener {
	return func(ctx context.Context) (*sql.DB, string, func(), error) {
		resolvedConn, password, err := h.dbConns.ResolveCredential(conn, model.DBCredentialRoleReadonly)
		if err != nil {
			return nil, model.DBCredentialRoleReadonly, func() {}, err
		}
		driver, dsn := pool.BuildDSN(resolvedConn, password)
		db, err := pool.Open(driver, dsn, pool.ProfileExec)
		if err != nil {
			return nil, model.DBCredentialRoleReadonly, func() {}, err
		}
		return db, model.DBCredentialRoleReadonly, func() { db.Close() }, nil
	}
}

func logSQLEditorQueryError(ctx context.Context, userID uint64, conn *model.DBConnection, queryCtx queryExecutionContext, sqlText string, duration time.Duration, timeoutSettings sqlEditorTimeoutSettings, err error) {
	attrs := []any{
		"user_id", userID,
		"connection_id", conn.ID,
		"connection_name", conn.Name,
		"db_type", conn.DBType,
		"database", queryCtx.DatabaseName,
		"schema", queryCtx.SchemaName,
		"duration_ms", duration.Milliseconds(),
		"app_timeout", timeoutSettings.AppTimeout.String(),
		"context_err", ctx.Err(),
		"err", err,
		"sql", truncate(sqlText, 500),
	}
	if errors.Is(err, context.Canceled) {
		slog.Info("sql editor query canceled", attrs...)
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		slog.Info("sql editor query timed out", attrs...)
		return
	}
	slog.Warn("sql editor query failed", attrs...)
}

func (h *QueryHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		QueryExecutionID string `json:"query_execution_id"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	queryID := strings.TrimSpace(req.QueryExecutionID)
	if queryID == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "query_execution_id is required")
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	query, ok := h.activeQueries.cancel(queryID, userID)
	if !ok {
		jsonOK(w, map[string]any{"cancel_requested": true, "pending": true})
		return
	}
	if query.CancelDBOpener == nil {
		jsonErr(w, http.StatusInternalServerError, "query cancel credential is not configured")
		return
	}

	if query.Cancel != nil {
		defer query.Cancel()
	}
	if err := cancelActiveSQLQuery(context.Background(), query); err != nil {
		jsonErr(w, http.StatusInternalServerError, "query cancel failed")
		return
	}
	jsonOK(w, map[string]any{"cancel_requested": true})
}

// POST /query/sensitive-access
func (h *QueryHandler) CreateSensitiveAccessTicket(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())

	var req struct {
		DBConnectionID          uint64 `json:"db_connection_id"`
		SQLContent              string `json:"sql_content"`
		DatabaseName            string `json:"database_name"`
		SchemaName              string `json:"schema_name"`
		ApprovedDurationMinutes int    `json:"approved_duration_minutes"`
		QueryContext            string `json:"query_context_token"`
		Reason                  string `json:"reason"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DBConnectionID == 0 || strings.TrimSpace(req.SQLContent) == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "db_connection_id and sql_content are required")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "reason is required")
		return
	}
	approvedDurationMinutes, err := normalizeSensitiveAccessDurationMinutes(req.ApprovedDurationMinutes)
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	conn, err := h.dbConns.GetByID(r.Context(), req.DBConnectionID)
	if err != nil || conn == nil {
		jsonErr(w, http.StatusNotFound, "db connection not found")
		return
	}
	if conn.DBType != "mysql" {
		jsonErr(w, http.StatusUnprocessableEntity, "sensitive query access only supports mysql connections")
		return
	}
	if err := sqlreview.CheckReadOnly(sqlparse.DialectFromDBType(conn.DBType), req.SQLContent); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "only read-only SQL is allowed: "+err.Error())
		return
	}
	hasAccess, err := h.userCanAccessConnection(r.Context(), userID, req.DBConnectionID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "db scope check failed")
		return
	}
	if !hasAccess {
		jsonErr(w, http.StatusForbidden, "access to this connection is not allowed")
		return
	}

	analysis, err := validateQueryContextToken(h.jwtSecret, req.QueryContext, userID, conn.ID, req.SQLContent, req.DatabaseName, queryContextSchemaName(conn, req.SchemaName))
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if !analysis.ContainsSensitive {
		jsonErr(w, http.StatusUnprocessableEntity, "query does not contain sensitive columns")
		return
	}
	description := fmt.Sprintf("申請原因：%s\nDuration=%d minutes\nSensitive=true", reason, approvedDurationMinutes)
	ticket, err := h.tickets.CreateWithScopes(r.Context(), &model.Ticket{
		Title:                   fmt.Sprintf("Sensitive Query Access / %s", conn.Name),
		Description:             &description,
		SQLContent:              req.SQLContent,
		TicketType:              model.TicketTypeSensitiveQueryAccess,
		DBConnectionID:          &req.DBConnectionID,
		DatabaseName:            optionalTrimmedString(req.DatabaseName),
		SubmitterID:             userID,
		ApprovedDurationMinutes: &approvedDurationMinutes,
	}, analysis.Scopes)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create sensitive access ticket failed")
		return
	}
	resolution, err := resolveTicketWorkflow(r.Context(), h.settings, h.users, ticket)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "resolve sensitive access workflow failed")
		return
	}
	if err := h.tickets.SaveWorkflowSnapshot(r.Context(), ticket.ID, resolution); err != nil {
		jsonErr(w, http.StatusInternalServerError, "save sensitive access workflow snapshot failed")
		return
	}
	if resolution == nil || resolution.ErrorCode != "" {
		comment := "Workflow resolution failed."
		if resolution != nil && resolution.ErrorMessage != "" {
			comment = resolution.ErrorMessage
		}
		if _, err := h.tickets.UpdateStatus(r.Context(), ticket.ID, model.TicketStatusPendingReview, model.TicketStatusNeedsAdminAttention, nil, &comment, nil); err != nil {
			jsonErr(w, http.StatusInternalServerError, "mark sensitive access workflow attention failed")
			return
		}
		if updated, err := h.tickets.GetByID(r.Context(), ticket.ID); err == nil && updated != nil {
			ticket = updated
		}
		h.audit.Log(r.Context(), repository.AuditEntry{
			ActorID:      &userID,
			ActorName:    middleware.UsernameFromCtx(r.Context()),
			ActionType:   "workflow_resolution_failed",
			ResourceType: "ticket",
			ResourceID:   &ticket.ID,
			Details:      workflowAuditDetails(ticket, resolution),
			IPAddress:    clientIP(r),
		})
		submitterName := strings.TrimSpace(middleware.UsernameFromCtx(r.Context()))
		if submitterName == "" {
			submitterName = strconv.FormatUint(userID, 10)
		}
		body := buildTicketNotificationBody(ticket, &conn.Name, submitterName, exportTicketStateLabel(ticket.Status), h.ticketLink(ticket.TicketNo))
		h.notifications.SendTicket(r.Context(), ticket, NotificationRoute{
			RecipientIDs: resolution.AdminUserIDs,
			ActorID:      &userID,
			NotifType:    "ticket_needs_admin_attention",
			Title:        "工單需要管理員處理",
			Body:         body,
			LarkCard: buildLarkTicketCard(
				r.Context(),
				h.settings,
				h.dbConns,
				h.users,
				h.appBaseURL,
				ticket,
				"工單需要管理員處理",
				"ticket_needs_admin_attention",
				exportTicketStateLabel(ticket.Status),
			),
		})
		publishTicketRealtimeEvent(r.Context(), h.broker, ticket, resolution, &userID)
		jsonCreated(w, map[string]any{
			"ticket_id":   ticket.ID,
			"ticket_no":   ticket.TicketNo,
			"status":      string(ticket.Status),
			"scope_count": len(analysis.Scopes),
		})
		return
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "ticket_submit",
		ResourceType: "ticket",
		ResourceID:   &ticket.ID,
		Details: map[string]any{
			"ticket_type":                ticket.TicketType,
			"approved_duration_minutes":  approvedDurationMinutes,
			"contains_sensitive_columns": true,
			"scope_count":                len(analysis.Scopes),
		},
		IPAddress: clientIP(r),
	})
	submitterName := strings.TrimSpace(middleware.UsernameFromCtx(r.Context()))
	if submitterName == "" {
		submitterName = strconv.FormatUint(userID, 10)
	}
	body := buildTicketNotificationBody(
		ticket,
		&conn.Name,
		submitterName,
		exportTicketStateLabel(model.TicketStatusPendingReview),
		h.ticketLink(ticket.TicketNo),
	)
	h.notifications.SendTicket(r.Context(), ticket, NotificationRoute{
		RecipientIDs: resolution.ApprovalUserIDs,
		ActorID:      &userID,
		NotifType:    "ticket_pending_review",
		Title:        exportPendingReviewTitle(),
		Body:         body,
		LarkCard: buildLarkTicketCard(
			r.Context(),
			h.settings,
			h.dbConns,
			h.users,
			h.appBaseURL,
			ticket,
			exportPendingReviewTitle(),
			"ticket_pending_review",
			exportTicketStateLabel(model.TicketStatusPendingReview),
		),
	})
	publishTicketRealtimeEvent(r.Context(), h.broker, ticket, resolution, &userID)

	jsonCreated(w, map[string]any{
		"ticket_id":   ticket.ID,
		"ticket_no":   ticket.TicketNo,
		"status":      string(ticket.Status),
		"scope_count": len(analysis.Scopes),
	})
}

func (h *QueryHandler) ListHistory(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())
	limit := defaultQueryHistoryLimit
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			switch {
			case parsed <= 0:
				limit = defaultQueryHistoryLimit
			case parsed > maxQueryHistoryLimit:
				limit = maxQueryHistoryLimit
			default:
				limit = parsed
			}
		}
	}
	offset := 0
	if v := strings.TrimSpace(r.URL.Query().Get("offset")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			offset = parsed
		}
	}

	since := timeutil.NowUTC().AddDate(0, 0, -queryHistoryRetentionDays)
	total, err := h.artifacts.CountHistory(r.Context(), userID, since)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "count query history failed")
		return
	}

	history, err := h.artifacts.ListHistory(r.Context(), userID, limit, offset, since)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list query history failed")
		return
	}
	if history == nil {
		history = []model.QueryHistoryEntry{}
	}
	jsonOK(w, map[string]any{
		"history":        history,
		"total":          total,
		"limit":          limit,
		"offset":         offset,
		"retention_days": queryHistoryRetentionDays,
	})
}

func (h *QueryHandler) ListSavedQueries(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())
	savedQueries, err := h.artifacts.ListSavedQueries(r.Context(), userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list saved queries failed")
		return
	}
	if savedQueries == nil {
		savedQueries = []model.SavedQuery{}
	}
	jsonOK(w, map[string]any{"saved_queries": savedQueries})
}

func (h *QueryHandler) CreateSavedQuery(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())

	var req struct {
		Label          string `json:"label"`
		DBConnectionID uint64 `json:"db_connection_id"`
		Database       string `json:"database"`
		Schema         string `json:"schema"`
		RedisDBIndex   *int   `json:"redis_db_index"`
		SQLContent     string `json:"sql_content"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Label = strings.TrimSpace(req.Label)
	req.SQLContent = strings.TrimSpace(req.SQLContent)
	if req.Label == "" || req.DBConnectionID == 0 || req.SQLContent == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "label, db_connection_id and sql_content are required")
		return
	}

	if ok, err := h.userCanAccessConnection(r.Context(), userID, req.DBConnectionID); err != nil {
		jsonErr(w, http.StatusInternalServerError, "db scope check failed")
		return
	} else if !ok {
		jsonErr(w, http.StatusForbidden, "access to this connection is not allowed")
		return
	}

	existing, err := h.artifacts.FindSavedQueryBySignature(
		r.Context(),
		userID,
		req.DBConnectionID,
		req.SQLContent,
		optionalTrimmedString(req.Database),
		optionalTrimmedString(req.Schema),
		req.RedisDBIndex,
	)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "check saved query failed")
		return
	}
	if existing != nil {
		jsonErr(w, http.StatusConflict, "saved query already exists")
		return
	}

	count, err := h.artifacts.CountSavedQueries(r.Context(), userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "count saved queries failed")
		return
	}
	if count >= repository.MaxSavedQueriesPerUser {
		jsonErr(w, http.StatusConflict, "saved queries limit reached (max 20)")
		return
	}

	conn, err := h.dbConns.GetByID(r.Context(), req.DBConnectionID)
	if err != nil || conn == nil {
		jsonErr(w, http.StatusNotFound, "db connection not found")
		return
	}

	savedQuery := &model.SavedQuery{
		UserID:           userID,
		Label:            req.Label,
		DBConnectionID:   req.DBConnectionID,
		DBConnectionName: conn.Name,
		DatabaseName:     optionalTrimmedString(req.Database),
		SchemaName:       optionalTrimmedString(req.Schema),
		RedisDBIndex:     req.RedisDBIndex,
		SQLContent:       req.SQLContent,
	}
	id, err := h.artifacts.CreateSavedQuery(r.Context(), savedQuery)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create saved query failed")
		return
	}
	savedQuery.ID = id
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "saved_query_create",
		ResourceType: "saved_query",
		ResourceID:   &savedQuery.ID,
		Details: map[string]any{
			"label":            savedQuery.Label,
			"db_connection_id": savedQuery.DBConnectionID,
			"database":         nullableStringValue(savedQuery.DatabaseName),
			"schema":           nullableStringValue(savedQuery.SchemaName),
			"redis_db_index":   savedQuery.RedisDBIndex,
		},
		IPAddress: clientIP(r),
	})

	jsonCreated(w, savedQuery)
}

func (h *QueryHandler) DeleteSavedQuery(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	deleted, err := h.artifacts.DeleteSavedQuery(r.Context(), userID, id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "delete saved query failed")
		return
	}
	if !deleted {
		jsonErr(w, http.StatusNotFound, "saved query not found")
		return
	}
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "saved_query_delete",
		ResourceType: "saved_query",
		ResourceID:   &id,
		IPAddress:    clientIP(r),
	})
	w.WriteHeader(http.StatusNoContent)
}

func executeQueryForConnection(
	ctx context.Context,
	conn *model.DBConnection,
	password string,
	db *sql.DB,
	sqlStr string,
	queryCtx queryExecutionContext,
	timeoutSettings sqlEditorTimeoutSettings,
	options sqlQueryExecutionOptions,
) (*masking.QueryResult, error) {
	if conn.DBType == "postgres" || conn.DBType == "postgresql" {
		return executePostgresQuery(ctx, conn, password, db, sqlStr, queryCtx, timeoutSettings, options)
	}
	return executeSQLQuery(ctx, conn, db, sqlStr, queryCtx, timeoutSettings, options)
}

func executeSQLQuery(
	ctx context.Context,
	conn *model.DBConnection,
	db *sql.DB,
	sqlStr string,
	queryCtx queryExecutionContext,
	timeoutSettings sqlEditorTimeoutSettings,
	options sqlQueryExecutionOptions,
) (*masking.QueryResult, error) {
	pinnedConn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer pinnedConn.Close()

	if queryCtx.DatabaseName != "" {
		if _, err := pinnedConn.ExecContext(ctx, fmt.Sprintf("USE %s", quoteMySQLIdentifier(queryCtx.DatabaseName))); err != nil {
			return nil, err
		}
	}
	if _, err := pinnedConn.ExecContext(ctx, fmt.Sprintf("SET SESSION max_execution_time = %d", timeoutSettings.MySQLMaxExecutionTimeMs)); err != nil {
		return nil, err
	}
	threadID := currentMySQLConnectionID(ctx, pinnedConn)

	statements := splitSQLStatementsForLimit(sqlStr)
	var result *masking.QueryResult
	for _, statement := range statements {
		trimmed := strings.TrimSpace(statement)
		if trimmed == "" {
			continue
		}

		statementResult, err := executeSingleSQLStatement(ctx, pinnedConn, conn, trimmed, queryCtx, threadID, options)
		if err != nil {
			return nil, err
		}
		result = statementResult
	}
	if result == nil {
		return nil, fmt.Errorf("empty query")
	}
	return result, nil
}

func executePostgresQuery(
	ctx context.Context,
	connModel *model.DBConnection,
	password string,
	db *sql.DB,
	sqlStr string,
	queryCtx queryExecutionContext,
	timeoutSettings sqlEditorTimeoutSettings,
	options sqlQueryExecutionOptions,
) (*masking.QueryResult, error) {
	if queryCtx.DatabaseName != "" && !strings.EqualFold(queryCtx.DatabaseName, connectionDatabaseName(connModel)) {
		scopedDB, cleanup, err := openScopedQueryDB(connModel, password, queryCtx)
		if err != nil {
			return nil, err
		}
		defer cleanup()
		db = scopedDB
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, fmt.Sprintf("SET statement_timeout = %d", timeoutSettings.PostgresStatementTimeoutMs)); err != nil {
		return nil, err
	}
	if queryCtx.SchemaName != "" {
		if _, err := conn.ExecContext(ctx, fmt.Sprintf(`SET search_path TO "%s"`, strings.ReplaceAll(queryCtx.SchemaName, `"`, `""`))); err != nil {
			return nil, err
		}
	}

	var result *masking.QueryResult
	err = conn.Raw(func(driverConn any) error {
		pgxConn := driverConn.(*stdlib.Conn).Conn()
		backendPID := uint64(pgxConn.PgConn().PID())
		statements := splitSQLStatementsForLimit(sqlStr)
		for _, statement := range statements {
			trimmed := strings.TrimSpace(statement)
			if trimmed == "" {
				continue
			}

			queryResult, err := executeSinglePostgresStatement(ctx, pgxConn, connModel, trimmed, queryCtx, backendPID, options)
			if err != nil {
				return err
			}
			result = queryResult
		}
		if result == nil {
			return fmt.Errorf("empty query")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func executeSingleSQLStatement(
	ctx context.Context,
	pinnedConn *sql.Conn,
	conn *model.DBConnection,
	statement string,
	queryCtx queryExecutionContext,
	threadID uint64,
	options sqlQueryExecutionOptions,
) (*masking.QueryResult, error) {
	statementCtx := ctx
	if threadID > 0 && options.Registry != nil && options.QueryExecutionID != "" {
		var cancel context.CancelFunc
		statementCtx, cancel = context.WithCancel(ctx)
		if canceled := options.Registry.register(options.QueryExecutionID, activeSQLQuery{
			UserID:         options.UserID,
			ConnectionID:   conn.ID,
			DBType:         conn.DBType,
			MySQLThreadID:  threadID,
			Statement:      statement,
			Conn:           conn,
			CancelDBOpener: options.CancelDBOpener,
			Cancel:         cancel,
			RegisteredAt:   time.Now(),
		}); canceled {
			cancel()
			return nil, context.Canceled
		}
		defer func() {
			options.Registry.remove(options.QueryExecutionID)
			cancel()
		}()
	}

	rows, err := pinnedConn.QueryContext(statementCtx, statement)
	if err != nil {
		return nil, err
	}

	cols, err := rows.Columns()
	if err != nil {
		rows.Close()
		return nil, err
	}
	columnTypes, _ := rows.ColumnTypes()
	databaseTypes := make([]string, len(cols))
	for i := range cols {
		if i < len(columnTypes) {
			databaseTypes[i] = columnTypes[i].DatabaseTypeName()
		}
	}

	origins := make([]masking.ColumnOrigin, len(cols))
	dependencies := make([][]masking.ColumnOrigin, len(cols))
	if !isMySQLMetadataStatement(statement) {
		origins = inferColumnOriginsFromLabels(cols, effectiveQueryDatabaseName(conn, queryCtx))
		dependencies = dependenciesFromOrigins(origins)
	}
	result := &masking.QueryResult{
		Columns:      buildDisplayColumns(cols, origins),
		RawColumns:   cols,
		Origins:      origins,
		Dependencies: dependencies,
		Rows:         make([][]any, 0),
	}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		for i, v := range vals {
			vals[i] = queryResultCellValueForDatabaseType(v, databaseTypes[i])
		}
		result.Rows = append(result.Rows, vals)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	if !isMySQLMetadataStatement(statement) && shouldResolveMySQLOrigins(cols) {
		resolvedColumns, err := resolveMySQLLineageForStatement(ctx, conn, pinnedConn, statement, cols, queryCtx)
		if err == nil && len(resolvedColumns) == len(cols) {
			origins = make([]masking.ColumnOrigin, len(resolvedColumns))
			dependencies = make([][]masking.ColumnOrigin, len(resolvedColumns))
			for i, column := range resolvedColumns {
				origins[i] = column.Origin
				dependencies[i] = append([]masking.ColumnOrigin(nil), column.Dependencies...)
			}
			result.Origins = origins
			result.Dependencies = dependencies
			result.Columns = buildDisplayColumns(cols, origins)
		}
	}

	return result, nil
}

func currentMySQLConnectionID(ctx context.Context, pinnedConn *sql.Conn) uint64 {
	var threadID uint64
	if err := pinnedConn.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&threadID); err != nil {
		slog.Warn("sql editor mysql connection id lookup failed", "err", err)
		return 0
	}
	return threadID
}

func cancelActiveSQLQuery(ctx context.Context, query activeSQLQuery) error {
	switch query.DBType {
	case "redis":
		return nil
	case "postgres", "postgresql":
		return cancelPostgresQuery(ctx, query.Conn, query.PostgresPID, query.Statement, query.CancelDBOpener)
	default:
		return killMySQLQuery(ctx, query.Conn, query.MySQLThreadID, query.Statement, query.CancelDBOpener)
	}
}

func killMySQLQuery(ctx context.Context, conn *model.DBConnection, threadID uint64, statement string, killDBOpener sqlCancelDBOpener) error {
	if killDBOpener == nil {
		return fmt.Errorf("mysql query kill db opener is not configured")
	}
	openCtx, cancelOpen := context.WithTimeout(ctx, 5*time.Second)
	killDB, killCredentialRole, cleanup, err := killDBOpener(openCtx)
	cancelOpen()
	if err != nil {
		slog.Warn("sql editor mysql query kill db open failed",
			"connection_id", conn.ID,
			"connection_name", conn.Name,
			"mysql_thread_id", threadID,
			"kill_credential_role", killCredentialRole,
			"err", err,
			"sql", truncate(statement, 500),
		)
		return err
	}
	defer cleanup()

	killCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := killDB.ExecContext(killCtx, "CALL mysql.rds_kill_query(?)", threadID); err == nil {
		slog.Warn("sql editor mysql query killed",
			"connection_id", conn.ID,
			"connection_name", conn.Name,
			"mysql_thread_id", threadID,
			"method", "mysql.rds_kill_query",
			"kill_credential_role", killCredentialRole,
			"sql", truncate(statement, 500),
		)
		return nil
	} else {
		slog.Warn("sql editor mysql query kill failed",
			"connection_id", conn.ID,
			"connection_name", conn.Name,
			"mysql_thread_id", threadID,
			"method", "mysql.rds_kill_query",
			"kill_credential_role", killCredentialRole,
			"err", err,
			"sql", truncate(statement, 500),
		)
	}

	killConnCtx, cancelKillConn := context.WithTimeout(ctx, 5*time.Second)
	defer cancelKillConn()
	if _, err := killDB.ExecContext(killConnCtx, "CALL mysql.rds_kill(?)", threadID); err != nil {
		slog.Warn("sql editor mysql connection kill failed",
			"connection_id", conn.ID,
			"connection_name", conn.Name,
			"mysql_thread_id", threadID,
			"method", "mysql.rds_kill",
			"kill_credential_role", killCredentialRole,
			"err", err,
			"sql", truncate(statement, 500),
		)
	} else {
		slog.Warn("sql editor mysql query killed",
			"connection_id", conn.ID,
			"connection_name", conn.Name,
			"mysql_thread_id", threadID,
			"method", "mysql.rds_kill",
			"kill_credential_role", killCredentialRole,
			"sql", truncate(statement, 500),
		)
		return nil
	}

	killQueryCtx, cancelKillQuery := context.WithTimeout(ctx, 5*time.Second)
	defer cancelKillQuery()
	if _, err := killDB.ExecContext(killQueryCtx, fmt.Sprintf("KILL QUERY %d", threadID)); err != nil {
		slog.Warn("sql editor mysql query kill failed",
			"connection_id", conn.ID,
			"connection_name", conn.Name,
			"mysql_thread_id", threadID,
			"method", "mysql.kill_query",
			"kill_credential_role", killCredentialRole,
			"err", err,
			"sql", truncate(statement, 500),
		)
		return err
	}
	slog.Warn("sql editor mysql query killed",
		"connection_id", conn.ID,
		"connection_name", conn.Name,
		"mysql_thread_id", threadID,
		"method", "mysql.kill_query",
		"kill_credential_role", killCredentialRole,
		"sql", truncate(statement, 500),
	)
	return nil
}

func cancelPostgresQuery(ctx context.Context, conn *model.DBConnection, backendPID uint64, statement string, cancelDBOpener sqlCancelDBOpener) error {
	if cancelDBOpener == nil {
		return fmt.Errorf("postgres query cancel db opener is not configured")
	}
	openCtx, cancelOpen := context.WithTimeout(ctx, 5*time.Second)
	cancelDB, cancelCredentialRole, cleanup, err := cancelDBOpener(openCtx)
	cancelOpen()
	if err != nil {
		slog.Warn("sql editor postgres query cancel db open failed",
			"connection_id", conn.ID,
			"connection_name", conn.Name,
			"postgres_backend_pid", backendPID,
			"cancel_credential_role", cancelCredentialRole,
			"err", err,
			"sql", truncate(statement, 500),
		)
		return err
	}
	defer cleanup()

	cancelCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var canceled bool
	if err := cancelDB.QueryRowContext(cancelCtx, "SELECT pg_cancel_backend($1)", backendPID).Scan(&canceled); err != nil {
		slog.Warn("sql editor postgres query cancel failed",
			"connection_id", conn.ID,
			"connection_name", conn.Name,
			"postgres_backend_pid", backendPID,
			"method", "postgres.pg_cancel_backend",
			"cancel_credential_role", cancelCredentialRole,
			"err", err,
			"sql", truncate(statement, 500),
		)
		return err
	}
	if !canceled {
		slog.Warn("sql editor postgres query cancel returned false",
			"connection_id", conn.ID,
			"connection_name", conn.Name,
			"postgres_backend_pid", backendPID,
			"method", "postgres.pg_cancel_backend",
			"cancel_credential_role", cancelCredentialRole,
			"sql", truncate(statement, 500),
		)
		return fmt.Errorf("postgres query cancel returned false")
	}
	slog.Warn("sql editor postgres query canceled",
		"connection_id", conn.ID,
		"connection_name", conn.Name,
		"postgres_backend_pid", backendPID,
		"method", "postgres.pg_cancel_backend",
		"cancel_credential_role", cancelCredentialRole,
		"sql", truncate(statement, 500),
	)
	return nil
}

func isMySQLMetadataStatement(statement string) bool {
	fields := strings.Fields(strings.TrimSpace(statement))
	if len(fields) == 0 {
		return false
	}
	switch strings.ToUpper(fields[0]) {
	case "SHOW", "DESC", "DESCRIBE", "EXPLAIN":
		return true
	default:
		return false
	}
}

func executeSinglePostgresStatement(
	ctx context.Context,
	pgxConn *pgx.Conn,
	connModel *model.DBConnection,
	statement string,
	queryCtx queryExecutionContext,
	backendPID uint64,
	options sqlQueryExecutionOptions,
) (*masking.QueryResult, error) {
	statementCtx := ctx
	if backendPID > 0 && options.Registry != nil && options.QueryExecutionID != "" {
		var cancel context.CancelFunc
		statementCtx, cancel = context.WithCancel(ctx)
		if canceled := options.Registry.register(options.QueryExecutionID, activeSQLQuery{
			UserID:         options.UserID,
			ConnectionID:   connModel.ID,
			DBType:         connModel.DBType,
			PostgresPID:    backendPID,
			Statement:      statement,
			Conn:           connModel,
			CancelDBOpener: options.CancelDBOpener,
			Cancel:         cancel,
			RegisteredAt:   time.Now(),
		}); canceled {
			cancel()
			return nil, context.Canceled
		}
		defer func() {
			options.Registry.remove(options.QueryExecutionID)
			cancel()
		}()
	}

	rows, err := pgxConn.Query(statementCtx, statement)
	if err != nil {
		return nil, err
	}

	return collectPostgresQueryResult(statementCtx, rows, connModel, queryCtx, func(ctx context.Context, fields []pgconn.FieldDescription) ([]masking.ColumnOrigin, error) {
		return resolvePostgresOrigins(ctx, pgxConn, fields)
	})
}

func collectPostgresQueryResult(
	ctx context.Context,
	rows pgx.Rows,
	connModel *model.DBConnection,
	queryCtx queryExecutionContext,
	resolveOrigins func(context.Context, []pgconn.FieldDescription) ([]masking.ColumnOrigin, error),
) (*masking.QueryResult, error) {
	fieldDescriptions := rows.FieldDescriptions()
	columns := make([]string, len(fieldDescriptions))
	for i, field := range fieldDescriptions {
		columns[i] = field.Name
	}

	resultRows := make([][]any, 0)
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			rows.Close()
			return nil, err
		}
		for i, value := range values {
			values[i] = queryResultCellValueForPostgresField(value, fieldDescriptions[i])
		}
		resultRows = append(resultRows, values)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	origins, err := resolveOrigins(ctx, fieldDescriptions)
	if err != nil {
		return nil, err
	}
	return &masking.QueryResult{
		Columns:      buildDisplayColumns(columns, origins),
		RawColumns:   columns,
		Origins:      origins,
		Dependencies: dependenciesFromOrigins(origins),
		Rows:         resultRows,
	}, nil
}

func queryResultCellValue(value any) any {
	return queryResultCellValueForDatabaseType(value, "")
}

func queryResultCellValueForDatabaseType(value any, databaseType string) any {
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		if isBinaryDatabaseType(databaseType) {
			return "0x" + hex.EncodeToString(v)
		}
		return string(v)
	case time.Time:
		return v.Format(time.RFC3339Nano)
	case driver.Valuer:
		driverValue, err := v.Value()
		if err != nil {
			return fmt.Sprint(v)
		}
		return queryResultCellValueForDatabaseType(driverValue, databaseType)
	default:
		return fmt.Sprint(v)
	}
}

func queryResultCellValueForPostgresField(value any, field pgconn.FieldDescription) any {
	const postgresByteaOID = 17
	if field.DataTypeOID == postgresByteaOID {
		return queryResultCellValueForDatabaseType(value, "BYTEA")
	}
	return queryResultCellValue(value)
}

func isBinaryDatabaseType(databaseType string) bool {
	switch strings.ToUpper(strings.TrimSpace(databaseType)) {
	case "BINARY", "VARBINARY", "BLOB", "TINYBLOB", "MEDIUMBLOB", "LONGBLOB", "BYTEA", "BIT":
		return true
	default:
		return false
	}
}

func resolvePostgresOrigins(ctx context.Context, conn *pgx.Conn, fields []pgconn.FieldDescription) ([]masking.ColumnOrigin, error) {
	type originKey struct {
		tableOID uint32
		attrNum  uint16
	}

	keys := make([]originKey, 0, len(fields))
	seen := make(map[originKey]struct{}, len(fields))
	for _, field := range fields {
		if field.TableOID == 0 || field.TableAttributeNumber == 0 {
			continue
		}
		key := originKey{tableOID: field.TableOID, attrNum: field.TableAttributeNumber}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}

	lookup := make(map[originKey]masking.ColumnOrigin, len(keys))
	if len(keys) > 0 {
		tableOIDs := make([]uint32, 0, len(keys))
		tableSeen := make(map[uint32]struct{}, len(keys))
		for _, key := range keys {
			if _, ok := tableSeen[key.tableOID]; ok {
				continue
			}
			tableSeen[key.tableOID] = struct{}{}
			tableOIDs = append(tableOIDs, key.tableOID)
		}

		rows, err := conn.Query(ctx,
			`SELECT a.attrelid, a.attnum, n.nspname, c.relname, a.attname
			 FROM pg_catalog.pg_attribute a
			 JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
			 JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			 WHERE a.attrelid = ANY($1)
			   AND a.attnum > 0
			   AND NOT a.attisdropped`,
			tableOIDs,
		)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		databaseName := conn.PgConn().ParameterStatus("database")
		for rows.Next() {
			var tableOID uint32
			var attrNum uint16
			var schemaName string
			var tableName string
			var columnName string
			if err := rows.Scan(&tableOID, &attrNum, &schemaName, &tableName, &columnName); err != nil {
				return nil, err
			}
			lookup[originKey{tableOID: tableOID, attrNum: attrNum}] = masking.ColumnOrigin{
				Database: databaseName,
				Schema:   schemaName,
				Table:    tableName,
				Column:   columnName,
			}
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	origins := make([]masking.ColumnOrigin, len(fields))
	for i, field := range fields {
		key := originKey{tableOID: field.TableOID, attrNum: field.TableAttributeNumber}
		if origin, ok := lookup[key]; ok {
			origins[i] = origin
			continue
		}
		origins[i] = masking.ColumnOrigin{Column: field.Name}
	}
	return origins, nil
}

func inferColumnOriginsFromLabels(columns []string, databaseName string) []masking.ColumnOrigin {
	origins := make([]masking.ColumnOrigin, len(columns))
	for i, column := range columns {
		parts := strings.Split(column, ".")
		switch len(parts) {
		case 2:
			origins[i] = masking.ColumnOrigin{
				Database: databaseName,
				Table:    parts[0],
				Column:   parts[1],
			}
		default:
			origins[i] = masking.ColumnOrigin{Column: column}
		}
	}
	return origins
}

func dependenciesFromOrigins(origins []masking.ColumnOrigin) [][]masking.ColumnOrigin {
	dependencies := make([][]masking.ColumnOrigin, len(origins))
	for i, origin := range origins {
		if strings.TrimSpace(origin.Column) == "" {
			continue
		}
		dependencies[i] = []masking.ColumnOrigin{origin}
	}
	return dependencies
}

func buildDisplayColumns(rawColumns []string, origins []masking.ColumnOrigin) []string {
	displayColumns := make([]string, len(rawColumns))
	for i, rawColumn := range rawColumns {
		displayColumns[i] = displayColumnLabel(rawColumn)
	}
	return displayColumns
}

func displayColumnLabel(rawColumn string) string {
	trimmed := strings.TrimSpace(rawColumn)
	parts := strings.Split(trimmed, ".")
	if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		return strings.TrimSpace(parts[1])
	}
	return rawColumn
}

func (h *QueryHandler) userCanAccessConnection(ctx context.Context, userID, connectionID uint64) (bool, error) {
	accessibleIDs, err := h.users.GetEffectiveDBConnectionIDs(ctx, userID)
	if err != nil {
		return false, err
	}
	for _, accessibleID := range accessibleIDs {
		if accessibleID == connectionID {
			return true, nil
		}
	}
	return false, nil
}

func optionalTrimmedString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// injectLimit appends a LIMIT clause if the SQL is a SELECT and doesn't already have one.
func injectLimit(sqlStr string, limit int, dbType string) string {
	if dbType == "redis" {
		return sqlStr
	}

	dialect := sqlparse.DialectFromDBType(dbType)
	rewritten, _, err := sqlparse.RewriteSelectLimit(dialect, sqlStr, limit)
	if err != nil {
		return sqlStr
	}
	if strings.TrimSpace(rewritten) == "" {
		return sqlStr
	}
	return rewritten
}

func splitSQLStatementsForLimit(sql string) []string {
	var statements []string
	var current strings.Builder
	depth := 0
	var quote rune

	for _, ch := range sql {
		switch {
		case quote != 0:
			current.WriteRune(ch)
			if ch == quote {
				quote = 0
			}
		case ch == '\'' || ch == '"' || ch == '`':
			quote = ch
			current.WriteRune(ch)
		case ch == '(':
			depth++
			current.WriteRune(ch)
		case ch == ')':
			if depth > 0 {
				depth--
			}
			current.WriteRune(ch)
		case ch == ';' && depth == 0:
			statements = append(statements, current.String())
			current.Reset()
		default:
			current.WriteRune(ch)
		}
	}

	if current.Len() > 0 {
		statements = append(statements, current.String())
	}

	return statements
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// executeRedis handles POST /query for Redis connections.
// Accepts a single read-only Redis command (e.g. "GET mykey", "HGETALL myhash").
func (h *QueryHandler) executeRedis(w http.ResponseWriter, r *http.Request, conn *model.DBConnection, cmdLine string, queryCtx queryExecutionContext) {
	cmd, args, err := sqlreview.ParseRedisCommand(cmdLine)
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "invalid redis command: "+err.Error())
		return
	}
	dbIndex := 0
	if queryCtx.RedisDBIndex != nil {
		dbIndex = *queryCtx.RedisDBIndex
	}
	userID := middleware.UserIDFromCtx(r.Context())
	if err := h.queryAccess.CheckRedis(r.Context(), userID, conn, dbIndex, cmd, args); err != nil {
		if missingErr, ok := err.(*queryaccess.MissingAccessError); ok {
			h.auditBlockedQuery(r, userID, conn.ID, cmdLine, "query_access_policy", map[string]any{
				"database":       strconv.Itoa(dbIndex),
				"redis_db_index": dbIndex,
				"missing":        missingErr.Missing,
			})
			jsonErr(w, http.StatusForbidden, missingErr.Error())
			return
		}
		slog.Error("redis query access check failed", "user_id", userID, "connection_id", conn.ID, "redis_db_index", dbIndex, "err", err)
		jsonErr(w, http.StatusUnprocessableEntity, "Query access is temporarily unavailable. Please try again later.")
		return
	}
	if err := sqlreview.CheckRedisReadOnly(cmdLine); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "only read-only Redis commands are allowed: "+err.Error())
		return
	}
	if h.redisPrefixes != nil {
		prefixRules, err := h.redisPrefixes.ListActiveForConnection(r.Context(), conn.ID, dbIndex)
		if err != nil {
			slog.Error("redis sensitive key policy load failed", "connection_id", conn.ID, "redis_db_index", dbIndex, "err", err)
			jsonErr(w, http.StatusInternalServerError, "redis sensitive key policy unavailable")
			return
		}
		if err := sqlreview.CheckRedisSensitiveKeyPrefixes(cmd, args, repository.RedisSensitiveKeyPrefixValues(prefixRules)); err != nil {
			h.auditBlockedQuery(r, middleware.UserIDFromCtx(r.Context()), conn.ID, cmdLine, "redis_sensitive_key_policy", map[string]any{
				"redis_db_index": dbIndex,
			})
			jsonErr(w, http.StatusForbidden, err.Error())
			return
		}
	}

	resolvedConn, password, err := h.dbConns.ResolveCredential(conn, model.DBCredentialRoleReadonly)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	timeoutSettings := h.loadSQLEditorTimeoutSettings(r.Context())
	ctx, cancel := context.WithTimeout(r.Context(), timeoutSettings.AppTimeout)
	defer cancel()

	ifaces := make([]interface{}, len(args))
	for i, a := range args {
		ifaces[i] = a
	}

	start := time.Now()
	val, err := pool.RedisGlobal().DoInDB(ctx, pool.RedisConnOptions{
		ConnID:   resolvedConn.ID,
		Host:     resolvedConn.Host,
		Port:     resolvedConn.Port,
		Username: resolvedConn.Username,
		Password: password,
		DB:       dbIndex,
		SSLMode:  resolvedConn.SSLMode,
	}, append([]interface{}{cmd}, ifaces...)...)
	durationMs := time.Since(start).Milliseconds()
	if err != nil && err != redis.Nil {
		writeQueryExecutionError(w, err, "redis command", timeoutSettings.AppTimeout)
		return
	}

	result := redisResultToQueryResult(val)

	connID := conn.ID
	auditDetails := map[string]any{
		"sql":         truncate(cmdLine, 500),
		"row_count":   len(result.Rows),
		"duration_ms": durationMs,
	}
	addAuditConnectionDetails(auditDetails, conn)
	auditDetails["redis_db_index"] = dbIndex
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "query_execute",
		ResourceType: "db_connection",
		ResourceID:   &connID,
		Details:      auditDetails,
		IPAddress:    clientIP(r),
	})

	if h.artifacts != nil {
		rowCount := len(result.Rows)
		_, _ = h.artifacts.AddHistory(r.Context(), &model.QueryHistoryEntry{
			UserID:           userID,
			DBConnectionID:   conn.ID,
			DBConnectionName: conn.Name,
			RedisDBIndex:     &dbIndex,
			SQLContent:       cmdLine,
			RowCount:         &rowCount,
			DurationMs:       durationMs,
		})
	}

	jsonOK(w, map[string]any{
		"columns":     result.Columns,
		"rows":        result.Rows,
		"row_count":   len(result.Rows),
		"duration_ms": durationMs,
	})
}

func (h *QueryHandler) auditBlockedQuery(r *http.Request, userID uint64, connID uint64, sqlText string, reason string, extra map[string]any) {
	if h.audit == nil {
		return
	}
	details := map[string]any{
		"sql":    truncate(sqlText, 500),
		"reason": reason,
	}
	for key, value := range extra {
		details[key] = value
	}
	if h.dbConns != nil {
		conn, err := h.dbConns.GetByID(r.Context(), connID)
		if err == nil && conn != nil {
			addAuditConnectionDetails(details, conn)
		}
	}
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "query_blocked",
		ResourceType: "db_connection",
		ResourceID:   &connID,
		Details:      details,
		IPAddress:    clientIP(r),
	})
}

func openScopedQueryDB(
	conn *model.DBConnection,
	password string,
	queryCtx queryExecutionContext,
) (*sql.DB, func(), error) {
	switch conn.DBType {
	case "postgres", "postgresql":
		targetDatabase := queryCtx.DatabaseName
		if targetDatabase == "" {
			targetDatabase = connectionDatabaseName(conn)
		}
		dsn := pool.BuildPostgresDSN(conn.Host, conn.Port, conn.Username, password, &targetDatabase, conn.SSLMode)
		db, err := pool.Open("pgx", dsn, pool.ProfileScopedPGQuery)
		if err != nil {
			return nil, nil, err
		}
		return db, func() { _ = db.Close() }, nil
	default:
		return nil, nil, fmt.Errorf("scoped mysql pool is not supported")
	}
}

func effectiveQueryDatabaseName(conn *model.DBConnection, queryCtx queryExecutionContext) string {
	if queryCtx.DatabaseName != "" {
		return queryCtx.DatabaseName
	}
	return connectionDatabaseName(conn)
}

func quoteMySQLIdentifier(identifier string) string {
	return "`" + strings.ReplaceAll(strings.TrimSpace(identifier), "`", "``") + "`"
}

// redisResultToQueryResult converts a Redis command result to a tabular QueryResult.
// Scalar values produce one row; arrays/maps produce multiple rows.
func redisResultToQueryResult(val interface{}) *masking.QueryResult {
	switch v := val.(type) {
	case nil:
		return &masking.QueryResult{Columns: []string{"result"}, Rows: [][]any{{"(nil)"}}}
	case string:
		return &masking.QueryResult{Columns: []string{"result"}, Rows: [][]any{{v}}}
	case int64:
		return &masking.QueryResult{Columns: []string{"result"}, Rows: [][]any{{queryResultCellValue(v)}}}
	case []interface{}:
		result := &masking.QueryResult{Columns: []string{"value"}, Rows: make([][]any, 0)}
		for _, item := range v {
			result.Rows = append(result.Rows, []any{queryResultCellValue(item)})
		}
		return result
	case map[interface{}]interface{}:
		result := &masking.QueryResult{Columns: []string{"field", "value"}, Rows: make([][]any, 0)}
		for k, fv := range v {
			result.Rows = append(result.Rows, []any{queryResultCellValue(k), queryResultCellValue(fv)})
		}
		return result
	case map[string]interface{}:
		result := &masking.QueryResult{Columns: []string{"field", "value"}, Rows: make([][]any, 0)}
		for k, fv := range v {
			result.Rows = append(result.Rows, []any{k, queryResultCellValue(fv)})
		}
		return result
	default:
		return &masking.QueryResult{Columns: []string{"result"}, Rows: [][]any{{queryResultCellValue(v)}}}
	}
}
