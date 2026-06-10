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
	"github.com/redis/go-redis/v9"
)

const (
	defaultQueryLimit   = 200
	maxQueryLimit       = 1000
	defaultQueryTimeout = 30 * time.Second
)

type QueryHandler struct {
	dbConns      *repository.DBConnectionRepo
	maskingRules *repository.MaskingRuleRepo
	audit        *repository.AuditRepo
	engine       *masking.Engine
}

func NewQueryHandler(
	dbConns *repository.DBConnectionRepo,
	maskingRules *repository.MaskingRuleRepo,
	audit *repository.AuditRepo,
	engine *masking.Engine,
) *QueryHandler {
	return &QueryHandler{
		dbConns:      dbConns,
		maskingRules: maskingRules,
		audit:        audit,
		engine:       engine,
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

	// Redis takes a completely different path
	if conn.DBType == "redis" {
		h.executeRedis(w, r, conn, req.SQL)
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
	execSQL := injectLimit(req.SQL, limit)

	start := time.Now()
	result, err := executeQuery(ctx, pools.QueryPool, execSQL)
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "query failed: "+err.Error())
		return
	}
	durationMs := time.Since(start).Milliseconds()

	// Apply masking rules (Fail Closed: 422 on error)
	dbRules, err := h.maskingRules.ListForConnection(r.Context(), conn.ID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load masking rules failed")
		return
	}
	if len(dbRules) > 0 {
		maskRules := make([]masking.Rule, 0, len(dbRules))
		for _, mr := range dbRules {
			maskRules = append(maskRules, masking.Rule{
				Table:  mr.TableName,
				Column: mr.ColumnName,
				Mode:   masking.MaskMode(mr.MaskMode),
			})
		}
		if err := h.engine.MaskResult(result, maskRules); err != nil {
			jsonErr(w, http.StatusUnprocessableEntity, "masking failed")
			return
		}
	}

	userID := middleware.UserIDFromCtx(r.Context())
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
		"columns":     result.Columns,
		"rows":        result.Rows,
		"row_count":   len(result.Rows),
		"duration_ms": durationMs,
	})
}

func executeQuery(ctx context.Context, db *sql.DB, sqlStr string) (*masking.QueryResult, error) {
	rows, err := db.QueryContext(ctx, sqlStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	result := &masking.QueryResult{Columns: cols}
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

// injectLimit appends a LIMIT clause if the SQL is a SELECT and doesn't already have one.
func injectLimit(sqlStr string, limit int) string {
	upper := strings.ToUpper(strings.TrimSpace(sqlStr))
	if !strings.HasPrefix(upper, "SELECT") {
		return sqlStr
	}
	if strings.Contains(upper, " LIMIT ") {
		return sqlStr
	}
	return sqlStr + " LIMIT " + strconv.Itoa(limit)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// executeRedis handles POST /query for Redis connections.
// Accepts a single read-only Redis command (e.g. "GET mykey", "HGETALL myhash").
func (h *QueryHandler) executeRedis(w http.ResponseWriter, r *http.Request, conn *model.DBConnection, cmdLine string) {
	if err := sqlreview.CheckRedisReadOnly(cmdLine); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "only read-only Redis commands are allowed: "+err.Error())
		return
	}

	password, err := h.dbConns.DecryptPassword(conn)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	addr := pool.BuildRedisAddr(conn.Host, conn.Port)
	client := pool.RedisGlobal().GetOrCreate(conn.ID, addr, password, 0)

	ctx, cancel := context.WithTimeout(r.Context(), defaultQueryTimeout)
	defer cancel()

	cmd, args := sqlreview.ParseRedisCommand(cmdLine)
	ifaces := make([]interface{}, len(args))
	for i, a := range args {
		ifaces[i] = a
	}

	start := time.Now()
	val, err := client.Do(ctx, append([]interface{}{cmd}, ifaces...)...).Result()
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
		result := &masking.QueryResult{Columns: []string{"value"}}
		for _, item := range v {
			result.Rows = append(result.Rows, []any{fmt.Sprintf("%v", item)})
		}
		return result
	case map[interface{}]interface{}:
		result := &masking.QueryResult{Columns: []string{"field", "value"}}
		for k, fv := range v {
			result.Rows = append(result.Rows, []any{fmt.Sprintf("%v", k), fmt.Sprintf("%v", fv)})
		}
		return result
	case map[string]interface{}:
		result := &masking.QueryResult{Columns: []string{"field", "value"}}
		for k, fv := range v {
			result.Rows = append(result.Rows, []any{k, fmt.Sprintf("%v", fv)})
		}
		return result
	default:
		return &masking.QueryResult{Columns: []string{"result"}, Rows: [][]any{{fmt.Sprintf("%v", v)}}}
	}
}
