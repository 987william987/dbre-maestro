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
	masking      *maskingRuntime
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
	engine *masking.Engine,
	whitelist *repository.MaskingWhitelistRepo,
) *QueryHandler {
	return &QueryHandler{
		dbConns:      dbConns,
		users:        users,
		maskingRules: maskingRules,
		audit:        audit,
		masking:      newMaskingRuntime(users, maskingRules, whitelist, engine),
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
	accessibleIDs, err := h.users.GetEffectiveDBConnectionIDs(r.Context(), userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "db scope check failed")
		return
	}
	hasAccess := false
	for _, accessibleID := range accessibleIDs {
		if accessibleID == req.DBConnectionID {
			hasAccess = true
			break
		}
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

	password, err := h.dbConns.DecryptPassword(conn)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	driver, dsn := pool.BuildDSN(conn, password)
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
	result, err := executeQueryForConnection(ctx, conn, password, pools.QueryPool, execSQL, queryCtx)
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "query failed: "+err.Error())
		return
	}
	durationMs := time.Since(start).Milliseconds()

	sensitiveOverrideActive, err := h.masking.applyResult(r.Context(), conn, userID, result)
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

	jsonOK(w, map[string]any{
		"columns":                   result.Columns,
		"rows":                      result.Rows,
		"row_count":                 len(result.Rows),
		"duration_ms":               durationMs,
		"sensitive_override_active": sensitiveOverrideActive,
	})
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

	rows, err := pinnedConn.QueryContext(ctx, sqlStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	result := &masking.QueryResult{
		Columns: cols,
		Origins: inferColumnOriginsFromLabels(cols, effectiveQueryDatabaseName(conn, queryCtx)),
		Rows:    make([][]any, 0),
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
		// Convert []byte → string for JSON serialization
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		result.Rows = append(result.Rows, vals)
	}
	return result, rows.Err()
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
		rows, err := pgxConn.Query(ctx, sqlStr)
		if err != nil {
			return err
		}
		defer rows.Close()

		fieldDescriptions := rows.FieldDescriptions()
		columns := make([]string, len(fieldDescriptions))
		for i, field := range fieldDescriptions {
			columns[i] = field.Name
		}

		origins, err := resolvePostgresOrigins(ctx, pgxConn, fieldDescriptions)
		if err != nil {
			return err
		}

		queryResult := &masking.QueryResult{
			Columns: columns,
			Origins: origins,
			Rows:    make([][]any, 0),
		}
		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				return err
			}
			for i, value := range values {
				if b, ok := value.([]byte); ok {
					values[i] = string(b)
				}
			}
			queryResult.Rows = append(queryResult.Rows, values)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		result = queryResult
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
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

// injectLimit appends a LIMIT clause if the SQL is a SELECT and doesn't already have one.
func injectLimit(sqlStr string, limit int, dbType string) string {
	if dbType == "redis" {
		return sqlStr
	}

	trimmed := strings.TrimSpace(sqlStr)
	withoutTrailingSemicolon := strings.TrimSpace(strings.TrimSuffix(trimmed, ";"))
	upper := strings.ToUpper(withoutTrailingSemicolon)
	if !strings.HasPrefix(upper, "SELECT") {
		return sqlStr
	}
	if strings.Contains(upper, " LIMIT ") {
		return withoutTrailingSemicolon
	}
	return withoutTrailingSemicolon + " LIMIT " + strconv.Itoa(limit)
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

	password, err := h.dbConns.DecryptPassword(conn)
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
		ConnID:   conn.ID,
		Host:     conn.Host,
		Port:     conn.Port,
		Username: conn.Username,
		Password: password,
		DB:       dbIndex,
		SSLMode:  conn.SSLMode,
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
