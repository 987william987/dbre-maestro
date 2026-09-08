package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/pool"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/dbre-maestro/maestro/internal/sqlreview"
)

type adminQueryRequest struct {
	DBConnectionID uint64 `json:"db_connection_id"`
	SQL            string `json:"sql"`
	Database       string `json:"database"`
	Schema         string `json:"schema"`
	RedisDBIndex   *int   `json:"redis_db_index"`
}

func (h *QueryHandler) ActivateAdminMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DBConnectionID uint64 `json:"db_connection_id"`
	}
	if err := bindJSON(r, &req); err != nil || req.DBConnectionID == 0 {
		jsonErr(w, http.StatusBadRequest, "db_connection_id is required")
		return
	}
	conn, ok := h.adminConnection(w, r, req.DBConnectionID)
	if !ok {
		return
	}
	h.logAdminAudit(r, conn, "sql_editor_admin_mode_enabled", true, "", 0, 0)
	jsonOK(w, map[string]any{"enabled": true, "endpoint": fmt.Sprintf("%s:%d", conn.EffectiveReadwriteHost(), conn.EffectiveReadwritePort()), "credential_role": model.DBCredentialRoleReadwrite})
}

func (h *QueryHandler) ExecuteAdmin(w http.ResponseWriter, r *http.Request) {
	var req adminQueryRequest
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.SQL = strings.TrimSpace(req.SQL)
	if req.DBConnectionID == 0 || req.SQL == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "db_connection_id and sql are required")
		return
	}
	conn, ok := h.adminConnection(w, r, req.DBConnectionID)
	if !ok {
		return
	}
	if statements := splitSQLStatementsForLimit(req.SQL); len(statements) != 1 {
		h.logAdminAudit(r, conn, "sql_editor_admin_execute_failed", false, req.SQL, 0, 0)
		jsonErr(w, http.StatusUnprocessableEntity, "admin mode requires exactly one statement")
		return
	}
	start := time.Now()
	resolved, password, err := h.dbConns.ResolveCredential(conn, model.DBCredentialRoleReadwrite)
	if err != nil {
		h.logAdminAudit(r, conn, "sql_editor_admin_execute_failed", false, req.SQL, time.Since(start).Milliseconds(), 0)
		jsonErr(w, http.StatusUnprocessableEntity, "readwrite credential is not configured")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.loadSQLEditorTimeoutSettings(r.Context()).AppTimeout)
	defer cancel()
	response, err := h.runAdminStatement(ctx, resolved, password, req)
	duration := time.Since(start).Milliseconds()
	if err != nil {
		h.logAdminAudit(r, conn, "sql_editor_admin_execute_failed", false, req.SQL, duration, 0)
		writeQueryExecutionError(w, err, "admin command", h.loadSQLEditorTimeoutSettings(r.Context()).AppTimeout)
		return
	}
	response["duration_ms"] = duration
	h.logAdminAudit(r, conn, "sql_editor_admin_execute", true, req.SQL, duration, response["affected_rows"].(int64))
	jsonOK(w, response)
}

func (h *QueryHandler) adminConnection(w http.ResponseWriter, r *http.Request, id uint64) (*model.DBConnection, bool) {
	conn, err := h.dbConns.GetByID(r.Context(), id)
	if err != nil || conn == nil {
		jsonErr(w, http.StatusNotFound, "db connection not found")
		return nil, false
	}
	ok, err := h.userCanAccessConnection(r.Context(), middleware.UserIDFromCtx(r.Context()), id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "db scope check failed")
		return nil, false
	}
	if !ok {
		jsonErr(w, http.StatusForbidden, "access to this connection is not allowed")
		return nil, false
	}
	return conn, true
}

func (h *QueryHandler) runAdminStatement(ctx context.Context, conn *model.DBConnection, password string, req adminQueryRequest) (map[string]any, error) {
	if conn.DBType == "redis" {
		cmd, args, err := sqlreview.ParseRedisCommand(req.SQL)
		if err != nil {
			return nil, err
		}
		dbIndex := 0
		if req.RedisDBIndex != nil {
			dbIndex = *req.RedisDBIndex
		}
		values := make([]any, 0, len(args)+1)
		values = append(values, cmd)
		for _, arg := range args {
			values = append(values, arg)
		}
		value, err := pool.RedisGlobal().DoInDB(ctx, pool.RedisConnOptions{ConnID: conn.ID, Host: conn.Host, Port: conn.Port, Username: conn.Username, Password: password, DB: dbIndex, SSLMode: conn.SSLMode}, values...)
		if err != nil {
			return nil, err
		}
		result := redisResultToQueryResult(value)
		return map[string]any{"columns": result.Columns, "rows": result.Rows, "row_count": len(result.Rows), "affected_rows": int64(0), "command": cmd}, nil
	}
	driver, dsn := pool.BuildDSN(conn, password)
	db, err := pool.Open(driver, dsn, pool.ProfileExec)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	pinned, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer pinned.Close()
	if req.Database != "" && conn.DBType == "mysql" {
		if _, err := pinned.ExecContext(ctx, "USE "+quoteMySQLIdentifier(req.Database)); err != nil {
			return nil, err
		}
	}
	if req.Schema != "" && (conn.DBType == "postgres" || conn.DBType == "postgresql") {
		if _, err := pinned.ExecContext(ctx, fmt.Sprintf(`SET search_path TO "%s"`, strings.ReplaceAll(req.Schema, `"`, `""`))); err != nil {
			return nil, err
		}
	}
	if adminStatementReturnsRows(req.SQL) {
		rows, err := pinned.QueryContext(ctx, req.SQL)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		columns, err := rows.Columns()
		if err != nil {
			return nil, err
		}
		columnTypes, _ := rows.ColumnTypes()
		databaseTypes := make([]string, len(columns))
		for i := range columns {
			if i < len(columnTypes) {
				databaseTypes[i] = columnTypes[i].DatabaseTypeName()
			}
		}
		items := make([][]any, 0)
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				return nil, err
			}
			for i, value := range values {
				values[i] = queryResultCellValueForDatabaseType(value, databaseTypes[i])
			}
			items = append(items, values)
		}
		return map[string]any{"columns": columns, "rows": items, "row_count": len(items), "affected_rows": int64(0), "command": "QUERY"}, rows.Err()
	}
	result, err := pinned.ExecContext(ctx, req.SQL)
	if err != nil {
		return nil, err
	}
	affected, _ := result.RowsAffected()
	return map[string]any{"columns": []string{}, "rows": [][]any{}, "row_count": 0, "affected_rows": affected, "command": strings.ToUpper(strings.Fields(req.SQL)[0])}, nil
}

func adminStatementReturnsRows(statement string) bool {
	upper := strings.ToUpper(strings.TrimSpace(statement))
	fields := strings.Fields(upper)
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "SELECT", "SHOW", "EXPLAIN", "DESC", "DESCRIBE", "WITH":
		return true
	case "INSERT", "UPDATE", "DELETE":
		for _, field := range fields[1:] {
			if field == "RETURNING" {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func (h *QueryHandler) logAdminAudit(r *http.Request, conn *model.DBConnection, action string, success bool, statement string, duration, affected int64) {
	userID := middleware.UserIDFromCtx(r.Context())
	details := map[string]any{"success": success, "credential_role": "readwrite", "endpoint": fmt.Sprintf("%s:%d", conn.EffectiveReadwriteHost(), conn.EffectiveReadwritePort())}
	if statement != "" {
		details["sql"] = adminAuditStatement(statement)
		details["duration_ms"] = duration
		details["affected_rows"] = affected
	}
	_ = h.audit.Log(context.WithoutCancel(r.Context()), repository.AuditEntry{ActorID: &userID, ActorName: middleware.UsernameFromCtx(r.Context()), ActionType: action, ResourceType: "db_connection", ResourceID: &conn.ID, Details: details, IPAddress: clientIP(r)})
}

func adminAuditStatement(statement string) string {
	upper := strings.ToUpper(statement)
	if strings.Contains(upper, "IDENTIFIED BY") || strings.Contains(upper, "PASSWORD") {
		return "[REDACTED SENSITIVE ADMIN STATEMENT]"
	}
	return truncate(statement, 500)
}
