package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
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
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
)

const (
	defaultQueryLimit   = 200
	maxQueryLimit       = 1000
	defaultQueryTimeout = 30 * time.Second
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
	settings      *repository.SettingsRepo
	queryAccess   *queryaccess.Service
	masking       *maskingRuntime
	notifRepo     *repository.NotificationRepo
	broker        *realtime.Broker
	lark          *notification.Dispatcher
	notifications *NotificationRouter
	appBaseURL    string
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

func NewQueryHandler(
	dbConns *repository.DBConnectionRepo,
	users *repository.UserRepo,
	maskingRules *repository.MaskingRuleRepo,
	audit *repository.AuditRepo,
	artifacts *repository.QueryArtifactRepo,
	tickets *repository.TicketRepo,
	settings *repository.SettingsRepo,
	queryAccessRepo *repository.QueryAccessRepo,
	engine *masking.Engine,
	whitelist *repository.MaskingWhitelistRepo,
	notifRepo *repository.NotificationRepo,
	broker *realtime.Broker,
	lark *notification.Dispatcher,
	appBaseURL string,
) *QueryHandler {
	return &QueryHandler{
		dbConns:       dbConns,
		users:         users,
		maskingRules:  maskingRules,
		audit:         audit,
		artifacts:     artifacts,
		tickets:       tickets,
		settings:      settings,
		queryAccess:   queryaccess.NewService(queryAccessRepo, users),
		masking:       newMaskingRuntime(users, maskingRules, whitelist, tickets, engine),
		notifRepo:     notifRepo,
		broker:        broker,
		lark:          lark,
		notifications: NewNotificationRouter(notifRepo, audit, broker, lark),
		appBaseURL:    strings.TrimRight(appBaseURL, "/"),
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
		DBConnectionID uint64 `json:"db_connection_id"`
		SQL            string `json:"sql"`
		Limit          int    `json:"limit"`
		Database       string `json:"database"`
		Schema         string `json:"schema"`
		RedisDBIndex   *int   `json:"redis_db_index"`
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

	// Whitelist check: only SELECT/SHOW/EXPLAIN/DESC/WITH
	if err := sqlreview.CheckReadOnly(sqlparse.DialectFromDBType(conn.DBType), req.SQL); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "only read-only SQL is allowed: "+err.Error())
		return
	}

	queryCtx := queryExecutionContext{
		DatabaseName: strings.TrimSpace(req.Database),
		SchemaName:   strings.TrimSpace(req.Schema),
	}
	if err := h.queryAccess.CheckSQL(r.Context(), userID, conn, req.SQL, queryaccess.CheckContext{
		DatabaseName: queryCtx.DatabaseName,
		SchemaName:   queryCtx.SchemaName,
	}); err != nil {
		if missingErr, ok := err.(*queryaccess.MissingAccessError); ok {
			jsonErr(w, http.StatusForbidden, missingErr.Error())
			return
		}
		slog.Error("query access check failed", "user_id", userID, "connection_id", conn.ID, "err", err)
		jsonErr(w, http.StatusUnprocessableEntity, "Query access is temporarily unavailable. Please try again later.")
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
	result, err := executeQueryForConnection(ctx, resolvedConn, password, pools.QueryPool, execSQL, queryCtx, timeoutSettings)
	if err != nil {
		writeQueryExecutionError(w, err, "query", timeoutSettings.AppTimeout)
		return
	}
	durationMs := time.Since(start).Milliseconds()

	sensitiveOverrideActive, sensitiveColumnIndexes, err := h.masking.applyResult(r.Context(), resolvedConn, userID, result)
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "masking failed")
		return
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "query_execute",
		ResourceType: "db_connection",
		ResourceID:   &req.DBConnectionID,
		Details: map[string]any{
			"sql":         truncate(req.SQL, 500),
			"row_count":   len(result.Rows),
			"duration_ms": durationMs,
		},
		IPAddress: clientIP(r),
	})

	if h.artifacts != nil {
		_, _ = h.artifacts.AddHistory(r.Context(), &model.QueryHistoryEntry{
			UserID:           userID,
			DBConnectionID:   req.DBConnectionID,
			DBConnectionName: conn.Name,
			DatabaseName:     optionalTrimmedString(req.Database),
			SchemaName:       optionalTrimmedString(req.Schema),
			RedisDBIndex:     req.RedisDBIndex,
			SQLContent:       req.SQL,
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
	})
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
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DBConnectionID == 0 || strings.TrimSpace(req.SQLContent) == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "db_connection_id and sql_content are required")
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

	analysis, err := analyzeSQLScopes(r.Context(), h.dbConns, h.masking, conn, req.SQLContent, buildQueryExecutionContext(req.DatabaseName, req.SchemaName))
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "analyze sensitive access query failed: "+err.Error())
		return
	}
	if !analysis.ContainsSensitive {
		jsonErr(w, http.StatusUnprocessableEntity, "query does not contain sensitive columns")
		return
	}
	description := fmt.Sprintf("由 SQL Editor 建立的臨時敏感查詢申請。Duration=%d minutes", approvedDurationMinutes)
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
		body := buildTicketNotificationBody(ticket, &conn.Name, exportTicketStateLabel(ticket.Status), "請修正 Workflow Rules 後重試路由", comment, h.ticketLink(ticket.TicketNo))
		h.notifications.SendTicket(r.Context(), ticket, NotificationRoute{
			RecipientIDs: resolution.AdminUserIDs,
			ActorID:      &userID,
			NotifType:    "ticket_needs_admin_attention",
			Title:        "工單需要管理員處理",
			Body:         body,
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
	body := buildTicketNotificationBody(
		ticket,
		&conn.Name,
		exportTicketStateLabel(model.TicketStatusPendingReview),
		"請審核是否通過此工單",
		"提交人已送出工單，等待 reviewer 處理。",
		h.ticketLink(ticket.TicketNo),
	)
	h.notifications.SendTicket(r.Context(), ticket, NotificationRoute{
		RecipientIDs: resolution.ApprovalUserIDs,
		ActorID:      &userID,
		NotifType:    "ticket_pending_review",
		Title:        exportPendingReviewTitle(),
		Body:         body,
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
	limit := 20
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	history, err := h.artifacts.ListHistory(r.Context(), userID, limit)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list query history failed")
		return
	}
	if history == nil {
		history = []model.QueryHistoryEntry{}
	}
	jsonOK(w, map[string]any{"history": history})
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
		jsonErr(w, http.StatusConflict, "saved queries limit reached (max 10)")
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
) (*masking.QueryResult, error) {
	if conn.DBType == "postgres" || conn.DBType == "postgresql" {
		return executePostgresQuery(ctx, conn, password, db, sqlStr, queryCtx, timeoutSettings)
	}
	return executeSQLQuery(ctx, conn, db, sqlStr, queryCtx, timeoutSettings)
}

func executeSQLQuery(
	ctx context.Context,
	conn *model.DBConnection,
	db *sql.DB,
	sqlStr string,
	queryCtx queryExecutionContext,
	timeoutSettings sqlEditorTimeoutSettings,
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

	statements := splitSQLStatementsForLimit(sqlStr)
	var result *masking.QueryResult
	for _, statement := range statements {
		trimmed := strings.TrimSpace(statement)
		if trimmed == "" {
			continue
		}

		statementResult, err := executeSingleSQLStatement(ctx, pinnedConn, conn, trimmed, queryCtx)
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
		statements := splitSQLStatementsForLimit(sqlStr)
		for _, statement := range statements {
			trimmed := strings.TrimSpace(statement)
			if trimmed == "" {
				continue
			}

			queryResult, err := executeSinglePostgresStatement(ctx, pgxConn, connModel, trimmed, queryCtx)
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
) (*masking.QueryResult, error) {
	rows, err := pinnedConn.QueryContext(ctx, statement)
	if err != nil {
		return nil, err
	}

	cols, err := rows.Columns()
	if err != nil {
		rows.Close()
		return nil, err
	}

	origins := inferColumnOriginsFromLabels(cols, effectiveQueryDatabaseName(conn, queryCtx))
	dependencies := dependenciesFromOrigins(origins)
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
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
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

	if shouldResolveMySQLOrigins(cols) {
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

func executeSinglePostgresStatement(
	ctx context.Context,
	pgxConn *pgx.Conn,
	connModel *model.DBConnection,
	statement string,
	queryCtx queryExecutionContext,
) (*masking.QueryResult, error) {
	rows, err := pgxConn.Query(ctx, statement)
	if err != nil {
		return nil, err
	}

	return collectPostgresQueryResult(ctx, rows, connModel, queryCtx, func(ctx context.Context, fields []pgconn.FieldDescription) ([]masking.ColumnOrigin, error) {
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
			if b, ok := value.([]byte); ok {
				values[i] = string(b)
			}
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
		if i < len(origins) && strings.TrimSpace(origins[i].Column) != "" {
			displayColumns[i] = origins[i].Column
			continue
		}

		parts := strings.Split(rawColumn, ".")
		if len(parts) > 1 {
			displayColumns[i] = parts[len(parts)-1]
			continue
		}

		displayColumns[i] = rawColumn
	}
	return displayColumns
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
	if err := sqlreview.CheckRedisReadOnly(cmdLine); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "only read-only Redis commands are allowed: "+err.Error())
		return
	}

	resolvedConn, password, err := h.dbConns.ResolveCredential(conn, model.DBCredentialRoleReadonly)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	timeoutSettings := h.loadSQLEditorTimeoutSettings(r.Context())
	ctx, cancel := context.WithTimeout(r.Context(), timeoutSettings.AppTimeout)
	defer cancel()

	cmd, args, err := sqlreview.ParseRedisCommand(cmdLine)
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "invalid redis command: "+err.Error())
		return
	}
	ifaces := make([]interface{}, len(args))
	for i, a := range args {
		ifaces[i] = a
	}
	dbIndex := 0
	if queryCtx.RedisDBIndex != nil {
		dbIndex = *queryCtx.RedisDBIndex
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

	userID := middleware.UserIDFromCtx(r.Context())
	connID := conn.ID
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "query_execute",
		ResourceType: "db_connection",
		ResourceID:   &connID,
		Details: map[string]any{
			"sql":         truncate(cmdLine, 500),
			"row_count":   len(result.Rows),
			"duration_ms": durationMs,
		},
		IPAddress: clientIP(r),
	})

	if h.artifacts != nil {
		_, _ = h.artifacts.AddHistory(r.Context(), &model.QueryHistoryEntry{
			UserID:           userID,
			DBConnectionID:   conn.ID,
			DBConnectionName: conn.Name,
			RedisDBIndex:     &dbIndex,
			SQLContent:       cmdLine,
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
		return &masking.QueryResult{Columns: []string{"result"}, Rows: [][]any{{fmt.Sprintf("%d", v)}}}
	case []interface{}:
		result := &masking.QueryResult{Columns: []string{"value"}, Rows: make([][]any, 0)}
		for _, item := range v {
			result.Rows = append(result.Rows, []any{fmt.Sprintf("%v", item)})
		}
		return result
	case map[interface{}]interface{}:
		result := &masking.QueryResult{Columns: []string{"field", "value"}, Rows: make([][]any, 0)}
		for k, fv := range v {
			result.Rows = append(result.Rows, []any{fmt.Sprintf("%v", k), fmt.Sprintf("%v", fv)})
		}
		return result
	case map[string]interface{}:
		result := &masking.QueryResult{Columns: []string{"field", "value"}, Rows: make([][]any, 0)}
		for k, fv := range v {
			result.Rows = append(result.Rows, []any{k, fmt.Sprintf("%v", fv)})
		}
		return result
	default:
		return &masking.QueryResult{Columns: []string{"result"}, Rows: [][]any{{fmt.Sprintf("%v", v)}}}
	}
}
