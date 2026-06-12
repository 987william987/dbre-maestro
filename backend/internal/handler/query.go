package handler

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/masking"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/pool"
	"github.com/dbre-maestro/maestro/internal/repository"
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

type QueryHandler struct {
	dbConns      *repository.DBConnectionRepo
	users        *repository.UserRepo
	maskingRules *repository.MaskingRuleRepo
	audit        *repository.AuditRepo
	artifacts    *repository.QueryArtifactRepo
	tickets      *repository.TicketRepo
	masking      *maskingRuntime
	notifRepo    *repository.NotificationRepo
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
	engine *masking.Engine,
	whitelist *repository.MaskingWhitelistRepo,
	notifRepo *repository.NotificationRepo,
) *QueryHandler {
	return &QueryHandler{
		dbConns:      dbConns,
		users:        users,
		maskingRules: maskingRules,
		audit:        audit,
		artifacts:    artifacts,
		tickets:      tickets,
		masking:      newMaskingRuntime(users, maskingRules, whitelist, tickets, engine),
		notifRepo:    notifRepo,
	}
}

func (h *QueryHandler) sendInApp(ctx context.Context, userID uint64, notifType, title, body, resType string, resID uint64) {
	if h.notifRepo == nil {
		return
	}
	_ = h.notifRepo.Create(ctx, userID, notifType, title, body, &resType, &resID)
}

func (h *QueryHandler) notifyReviewers(ctx context.Context, ticketID, submitterID uint64, title, body string) {
	reviewerIDs, err := listActiveUserIDsByPermissions(ctx, h.users, []string{permissionSQLEditorSensitiveRev})
	if err != nil {
		return
	}
	for _, reviewerID := range reviewerIDs {
		if reviewerID == submitterID {
			continue
		}
		h.sendInApp(ctx, reviewerID, "ticket_pending_review", title, body, "ticket", ticketID)
	}
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
	if err := sqlreview.CheckReadOnly(req.SQL); err != nil {
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

	ctx, cancel := context.WithTimeout(r.Context(), defaultQueryTimeout)
	defer cancel()

	// Inject LIMIT if not present (simple heuristic for SELECT statements)
	execSQL := injectLimit(req.SQL, limit, conn.DBType)
	queryCtx := queryExecutionContext{
		DatabaseName: strings.TrimSpace(req.Database),
		SchemaName:   strings.TrimSpace(req.Schema),
	}

	start := time.Now()
	result, err := executeQueryForConnection(ctx, resolvedConn, password, pools.QueryPool, execSQL, queryCtx)
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "query failed: "+err.Error())
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
	if req.ApprovedDurationMinutes == 0 {
		req.ApprovedDurationMinutes = 10
	}
	if req.ApprovedDurationMinutes != 10 && req.ApprovedDurationMinutes != 30 && req.ApprovedDurationMinutes != 60 {
		jsonErr(w, http.StatusUnprocessableEntity, "approved_duration_minutes must be 10, 30, or 60")
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
	if err := sqlreview.CheckReadOnly(req.SQLContent); err != nil {
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
	description := fmt.Sprintf("由 SQL Editor 建立的臨時敏感查詢申請。Duration=%d minutes", req.ApprovedDurationMinutes)
	ticket, err := h.tickets.CreateWithScopes(r.Context(), &model.Ticket{
		Title:                   fmt.Sprintf("Sensitive Query Access / %s", conn.Name),
		Description:             &description,
		SQLContent:              req.SQLContent,
		TicketType:              model.TicketTypeSensitiveQueryAccess,
		DBConnectionID:          &req.DBConnectionID,
		SubmitterID:             userID,
		ApprovedDurationMinutes: &req.ApprovedDurationMinutes,
	}, analysis.Scopes)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create sensitive access ticket failed")
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
			"approved_duration_minutes":  req.ApprovedDurationMinutes,
			"contains_sensitive_columns": true,
			"scope_count":                len(analysis.Scopes),
		},
		IPAddress: clientIP(r),
	})
	body := fmt.Sprintf("工單 %s 已提交，等待敏感查詢審核", ticket.TicketNo)
	h.sendInApp(r.Context(), userID, "ticket_submitted", "Sensitive Access 工單已建立", body, "ticket", ticket.ID)
	h.notifyReviewers(r.Context(), ticket.ID, userID, "新的 Sensitive Access 工單待審核", body)

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
) (*masking.QueryResult, error) {
	if conn.DBType == "postgres" || conn.DBType == "postgresql" {
		return executePostgresQuery(ctx, conn, password, db, sqlStr, queryCtx)
	}
	return executeSQLQuery(ctx, conn, db, sqlStr, queryCtx)
}

func executeSQLQuery(
	ctx context.Context,
	conn *model.DBConnection,
	db *sql.DB,
	sqlStr string,
	queryCtx queryExecutionContext,
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
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	origins := inferColumnOriginsFromLabels(cols, effectiveQueryDatabaseName(conn, queryCtx))
	result := &masking.QueryResult{
		Columns:    buildDisplayColumns(cols, origins),
		RawColumns: cols,
		Origins:    origins,
		Rows:       make([][]any, 0),
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
	return result, rows.Err()
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
	defer rows.Close()

	fieldDescriptions := rows.FieldDescriptions()
	columns := make([]string, len(fieldDescriptions))
	for i, field := range fieldDescriptions {
		columns[i] = field.Name
	}

	origins, err := resolvePostgresOrigins(ctx, pgxConn, fieldDescriptions)
	if err != nil {
		return nil, err
	}

	queryResult := &masking.QueryResult{
		Columns:    buildDisplayColumns(columns, origins),
		RawColumns: columns,
		Origins:    origins,
		Rows:       make([][]any, 0),
	}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		for i, value := range values {
			if b, ok := value.([]byte); ok {
				values[i] = string(b)
			}
		}
		queryResult.Rows = append(queryResult.Rows, values)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return queryResult, nil
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

	statements := splitSQLStatementsForLimit(sqlStr)
	if len(statements) == 0 {
		return sqlStr
	}

	transformed := make([]string, 0, len(statements))
	for _, statement := range statements {
		trimmed := strings.TrimSpace(statement)
		if trimmed == "" {
			continue
		}
		withoutTrailingSemicolon := strings.TrimSpace(strings.TrimSuffix(trimmed, ";"))
		rewritten, changed := injectLimitIntoStatement(withoutTrailingSemicolon, limit)
		if changed {
			transformed = append(transformed, rewritten)
			continue
		}
		transformed = append(transformed, withoutTrailingSemicolon)
	}

	if len(transformed) == 0 {
		return sqlStr
	}
	return strings.Join(transformed, "; ")
}

func injectLimitIntoStatement(statement string, limit int) (string, bool) {
	upper := strings.ToUpper(statement)
	if strings.HasPrefix(upper, "SELECT") {
		if hasTopLevelLimitClause(statement, 0) {
			return statement, false
		}
		return statement + " LIMIT " + strconv.Itoa(limit), true
	}

	if !strings.HasPrefix(upper, "WITH") {
		return statement, false
	}

	mainQueryStart, ok := withMainQueryStart(statement)
	if !ok {
		return statement, false
	}
	mainQuery := strings.TrimSpace(statement[mainQueryStart:])
	if !strings.HasPrefix(strings.ToUpper(mainQuery), "SELECT") {
		return statement, false
	}
	if hasTopLevelLimitClause(statement, mainQueryStart) {
		return statement, false
	}
	return statement + " LIMIT " + strconv.Itoa(limit), true
}

func withMainQueryStart(statement string) (int, bool) {
	upper := strings.ToUpper(statement)
	pos := limitSkipToken(upper, 0)
	pos = limitSkipWhitespace(upper, pos)
	if strings.HasPrefix(upper[pos:], "RECURSIVE") {
		pos = limitSkipToken(upper, pos)
		pos = limitSkipWhitespace(upper, pos)
	}

	for pos < len(upper) {
		pos = limitSkipToken(upper, pos)
		pos = limitSkipWhitespace(upper, pos)
		if pos >= len(upper) {
			return 0, false
		}
		if strings.HasPrefix(upper[pos:], "AS") {
			pos = limitSkipToken(upper, pos)
			pos = limitSkipWhitespace(upper, pos)
		}
		if pos < len(upper) && upper[pos] == '(' {
			pos = limitSkipBalancedParen(upper, pos)
			pos = limitSkipWhitespace(upper, pos)
		}
		if pos < len(upper) && upper[pos] == ',' {
			pos++
			pos = limitSkipWhitespace(upper, pos)
			continue
		}
		break
	}

	return pos, pos < len(upper)
}

func hasTopLevelLimitClause(statement string, start int) bool {
	upper := strings.ToUpper(statement)
	depth := 0
	var quote rune

	for i := start; i < len(upper); i++ {
		ch := rune(upper[i])
		switch {
		case quote != 0:
			if ch == quote {
				quote = 0
			}
		case ch == '\'' || ch == '"' || ch == '`':
			quote = ch
		case ch == '(':
			depth++
		case ch == ')':
			if depth > 0 {
				depth--
			}
		case depth == 0 && isLimitTokenAt(upper, i):
			return true
		}
	}

	return false
}

func isLimitTokenAt(statement string, index int) bool {
	if !strings.HasPrefix(statement[index:], "LIMIT") {
		return false
	}
	if index > 0 {
		prev := rune(statement[index-1])
		if prev == '_' || prev == '.' || ('A' <= prev && prev <= 'Z') || ('0' <= prev && prev <= '9') {
			return false
		}
	}
	end := index + len("LIMIT")
	if end < len(statement) {
		next := rune(statement[end])
		if next == '_' || ('A' <= next && next <= 'Z') || ('0' <= next && next <= '9') {
			return false
		}
	}
	return true
}

func limitSkipWhitespace(s string, pos int) int {
	for pos < len(s) && (s[pos] == ' ' || s[pos] == '\t' || s[pos] == '\n' || s[pos] == '\r') {
		pos++
	}
	return pos
}

func limitSkipToken(s string, pos int) int {
	pos = limitSkipWhitespace(s, pos)
	for pos < len(s) && ((s[pos] >= 'A' && s[pos] <= 'Z') || (s[pos] >= '0' && s[pos] <= '9') || s[pos] == '_') {
		pos++
	}
	return pos
}

func limitSkipBalancedParen(s string, pos int) int {
	if pos >= len(s) || s[pos] != '(' {
		return pos
	}

	depth := 0
	var quote rune
	for pos < len(s) {
		ch := rune(s[pos])
		switch {
		case quote != 0:
			if ch == quote {
				quote = 0
			}
		case ch == '\'' || ch == '"' || ch == '`':
			quote = ch
		case ch == '(':
			depth++
		case ch == ')':
			depth--
			if depth == 0 {
				return pos + 1
			}
		}
		pos++
	}

	return pos
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

	ctx, cancel := context.WithTimeout(r.Context(), defaultQueryTimeout)
	defer cancel()

	cmd, args := sqlreview.ParseRedisCommand(cmdLine)
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
		jsonErr(w, http.StatusUnprocessableEntity, "redis command failed: "+err.Error())
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
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			return nil, nil, err
		}
		db.SetMaxOpenConns(2)
		db.SetMaxIdleConns(1)
		db.SetConnMaxLifetime(2 * time.Minute)
		db.SetConnMaxIdleTime(1 * time.Minute)
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
