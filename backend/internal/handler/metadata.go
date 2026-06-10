package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/pool"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
)

type MetadataHandler struct {
	dbConns *repository.DBConnectionRepo
	users   *repository.UserRepo
}

func NewMetadataHandler(dbConns *repository.DBConnectionRepo, users *repository.UserRepo) *MetadataHandler {
	return &MetadataHandler{dbConns: dbConns, users: users}
}

// checkMetadataAccess returns true if the user may access the given connection.
// Access is derived from the caller's effective DB connection bindings.
func (h *MetadataHandler) checkMetadataAccess(r *http.Request, connID uint64) (bool, error) {
	userID := middleware.UserIDFromCtx(r.Context())
	accessibleIDs, err := h.users.GetEffectiveDBConnectionIDs(r.Context(), userID)
	if err != nil {
		return false, err
	}
	for _, accessibleID := range accessibleIDs {
		if accessibleID == connID {
			return true, nil
		}
	}
	return false, nil
}

type tableInfo struct {
	Schema    string `json:"schema"`
	Name      string `json:"name"`
	Engine    string `json:"engine"`
	RowCount  int64  `json:"row_count"`
	DataSize  int64  `json:"data_size_bytes"`
	IndexSize int64  `json:"index_size_bytes"`
	Comment   string `json:"comment"`
}

type columnInfo struct {
	Name       string `json:"name"`
	DataType   string `json:"data_type"`
	ColumnType string `json:"column_type"`
	IsNullable string `json:"is_nullable"`
	Default    string `json:"default,omitempty"`
	Comment    string `json:"comment"`
}

// GET /db-connections/{id}/metadata
// Returns list of tables with row counts and sizes across all accessible schemas.
func (h *MetadataHandler) Tables(w http.ResponseWriter, r *http.Request) {
	connID, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	ok, err := h.checkMetadataAccess(r, connID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "db scope check failed")
		return
	}
	if !ok {
		jsonErr(w, http.StatusForbidden, "access to this connection is not allowed")
		return
	}

	conn, err := h.dbConns.GetByID(r.Context(), connID)
	if err != nil || conn == nil {
		jsonErr(w, http.StatusNotFound, "connection not found")
		return
	}

	password, err := h.dbConns.DecryptPassword(conn)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	dbName := ""
	if conn.DatabaseName != nil {
		dbName = *conn.DatabaseName
	}
	driver, dsn := pool.BuildDSN(conn, password)
	pools, err := pool.Global().GetOrCreate(conn.ID, driver, dsn)
	if err != nil {
		jsonErr(w, http.StatusServiceUnavailable, "cannot connect: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	query := `
		SELECT
			TABLE_SCHEMA,
			TABLE_NAME,
			IFNULL(ENGINE, '') AS ENGINE,
			IFNULL(TABLE_ROWS, 0) AS TABLE_ROWS,
			IFNULL(DATA_LENGTH, 0) AS DATA_LENGTH,
			IFNULL(INDEX_LENGTH, 0) AS INDEX_LENGTH,
			IFNULL(TABLE_COMMENT, '') AS TABLE_COMMENT
		FROM information_schema.TABLES
		WHERE TABLE_TYPE = 'BASE TABLE'
		  AND TABLE_SCHEMA NOT IN ('information_schema', 'performance_schema', 'mysql', 'sys')`
	if dbName != "" {
		query += ` AND TABLE_SCHEMA = '` + escapeIdentifier(dbName) + `'`
	}
	query += ` ORDER BY TABLE_SCHEMA, TABLE_NAME`

	rows, err := pools.QueryPool.QueryContext(ctx, query)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "query metadata failed: "+err.Error())
		return
	}
	defer rows.Close()

	var tables []tableInfo
	for rows.Next() {
		var t tableInfo
		if err := rows.Scan(&t.Schema, &t.Name, &t.Engine, &t.RowCount, &t.DataSize, &t.IndexSize, &t.Comment); err != nil {
			jsonErr(w, http.StatusInternalServerError, "scan row failed")
			return
		}
		tables = append(tables, t)
	}
	if tables == nil {
		tables = []tableInfo{}
	}

	jsonOK(w, map[string]any{"tables": tables})
}

// GET /db-connections/{id}/metadata/{schema}/{table}/columns
// Returns column definitions for a specific table.
func (h *MetadataHandler) Columns(w http.ResponseWriter, r *http.Request) {
	connID, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	schema := chi.URLParam(r, "schema")
	table := chi.URLParam(r, "table")
	if schema == "" || table == "" {
		jsonErr(w, http.StatusBadRequest, "schema and table are required")
		return
	}

	ok, err := h.checkMetadataAccess(r, connID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "db scope check failed")
		return
	}
	if !ok {
		jsonErr(w, http.StatusForbidden, "access to this connection is not allowed")
		return
	}

	conn, err := h.dbConns.GetByID(r.Context(), connID)
	if err != nil || conn == nil {
		jsonErr(w, http.StatusNotFound, "connection not found")
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
		jsonErr(w, http.StatusServiceUnavailable, "cannot connect: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	rows, err := pools.QueryPool.QueryContext(ctx,
		`SELECT COLUMN_NAME, DATA_TYPE, COLUMN_TYPE, IS_NULLABLE, IFNULL(COLUMN_DEFAULT,''), IFNULL(COLUMN_COMMENT,'')
		 FROM information_schema.COLUMNS
		 WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		 ORDER BY ORDINAL_POSITION`,
		schema, table,
	)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "query columns failed: "+err.Error())
		return
	}
	defer rows.Close()

	var columns []columnInfo
	for rows.Next() {
		var c columnInfo
		if err := rows.Scan(&c.Name, &c.DataType, &c.ColumnType, &c.IsNullable, &c.Default, &c.Comment); err != nil {
			jsonErr(w, http.StatusInternalServerError, "scan column failed")
			return
		}
		columns = append(columns, c)
	}
	if columns == nil {
		columns = []columnInfo{}
	}

	jsonOK(w, map[string]any{
		"schema":  schema,
		"table":   table,
		"columns": columns,
	})
}

// escapeIdentifier does minimal escaping for schema/db names in SQL strings.
// Only allow alphanumeric + underscore to prevent injection.
func escapeIdentifier(s string) string {
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return ""
		}
	}
	return s
}
