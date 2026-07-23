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

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
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

type metadataItem struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Database  string `json:"database,omitempty"`
	Schema    string `json:"schema,omitempty"`
	Engine    string `json:"engine,omitempty"`
	RowCount  int64  `json:"row_count,omitempty"`
	DataSize  int64  `json:"data_size_bytes,omitempty"`
	IndexSize int64  `json:"index_size_bytes,omitempty"`
	Comment   string `json:"comment,omitempty"`
}

type metadataResponse struct {
	DBType   string         `json:"db_type"`
	Level    string         `json:"level"`
	Database string         `json:"database,omitempty"`
	Schema   string         `json:"schema,omitempty"`
	Items    []metadataItem `json:"items"`
}

type metadataSearchIndexResponse struct {
	DBType    string         `json:"db_type"`
	Items     []metadataItem `json:"items"`
	Limit     int            `json:"limit"`
	Truncated bool           `json:"truncated"`
}

type columnInfo struct {
	Name       string `json:"name"`
	DataType   string `json:"data_type"`
	ColumnType string `json:"column_type"`
	IsNullable string `json:"is_nullable"`
	Default    string `json:"default,omitempty"`
	Comment    string `json:"comment"`
}

type definitionResponse struct {
	Database   string `json:"database,omitempty"`
	Schema     string `json:"schema"`
	Table      string `json:"table"`
	Definition string `json:"definition"`
}

type postgresDefinitionColumn struct {
	Name         string
	DataType     string
	DefaultExpr  string
	IsNullable   bool
	IdentityType string
	Generated    string
}

type postgresDefinitionConstraint struct {
	Name       string
	Definition string
}

type postgresDefinitionIndex struct {
	Name       string
	Definition string
}

const metadataTemporaryErrorMessage = "metadata is temporarily unavailable, please try again later"
const postgresReservedDatabaseRDSAdmin = "rdsadmin"
const metadataSearchIndexLimit = 50000

func shouldSkipPostgresMetadataDatabase(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), postgresReservedDatabaseRDSAdmin)
}

func logMetadataQueryError(operation string, conn *model.DBConnection, database string, schema string, table string, err error) {
	if err == nil {
		return
	}

	attrs := []any{
		"operation", operation,
		"err", err,
	}
	if conn != nil {
		attrs = append(attrs,
			"connection_id", conn.ID,
			"connection_name", conn.Name,
			"db_type", conn.DBType,
			"host", conn.Host,
		)
	}
	if database != "" {
		attrs = append(attrs, "database", database)
	}
	if schema != "" {
		attrs = append(attrs, "schema", schema)
	}
	if table != "" {
		attrs = append(attrs, "table", table)
	}

	slog.Warn("metadata query failed", attrs...)
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

	resolvedConn, password, err := h.dbConns.ResolveCredential(conn, model.DBCredentialRoleReadonly)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	selectedDatabase := strings.TrimSpace(r.URL.Query().Get("database"))
	selectedSchema := strings.TrimSpace(r.URL.Query().Get("schema"))

	response, err := h.loadMetadata(ctx, resolvedConn, password, selectedDatabase, selectedSchema)
	if err != nil {
		logMetadataQueryError("tables", resolvedConn, selectedDatabase, selectedSchema, "", err)
		jsonErr(w, http.StatusInternalServerError, metadataTemporaryErrorMessage)
		return
	}

	jsonOK(w, response)
}

// GET /db-connections/{id}/metadata/search-index
// Returns a bounded database/schema/table index for SQL Editor object search.
func (h *MetadataHandler) SearchIndex(w http.ResponseWriter, r *http.Request) {
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

	resolvedConn, password, err := h.dbConns.ResolveCredential(conn, model.DBCredentialRoleReadonly)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	response, err := h.loadSearchIndex(ctx, resolvedConn, password, metadataSearchIndexLimit)
	if err != nil {
		logMetadataQueryError("search_index", resolvedConn, "", "", "", err)
		jsonErr(w, http.StatusInternalServerError, metadataTemporaryErrorMessage)
		return
	}

	jsonOK(w, response)
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

	resolvedConn, password, err := h.dbConns.ResolveCredential(conn, model.DBCredentialRoleReadonly)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	selectedDatabase := strings.TrimSpace(r.URL.Query().Get("database"))

	columns, resolvedDatabase, err := h.loadColumns(ctx, resolvedConn, password, selectedDatabase, schema, table)
	if err != nil {
		logMetadataQueryError("columns", resolvedConn, selectedDatabase, schema, table, err)
		jsonErr(w, http.StatusInternalServerError, metadataTemporaryErrorMessage)
		return
	}

	jsonOK(w, map[string]any{
		"database": resolvedDatabase,
		"schema":   schema,
		"table":    table,
		"columns":  columns,
	})
}

// GET /db-connections/{id}/metadata/{schema}/{table}/definition
// Returns the table definition for a specific table.
func (h *MetadataHandler) Definition(w http.ResponseWriter, r *http.Request) {
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

	resolvedConn, password, err := h.dbConns.ResolveCredential(conn, model.DBCredentialRoleReadonly)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	selectedDatabase := strings.TrimSpace(r.URL.Query().Get("database"))

	definition, resolvedDatabase, err := h.loadDefinition(ctx, resolvedConn, password, selectedDatabase, schema, table)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonErr(w, http.StatusNotFound, "table definition not found")
			return
		}
		logMetadataQueryError("definition", resolvedConn, selectedDatabase, schema, table, err)
		jsonErr(w, http.StatusInternalServerError, metadataTemporaryErrorMessage)
		return
	}

	jsonOK(w, definitionResponse{
		Database:   resolvedDatabase,
		Schema:     schema,
		Table:      table,
		Definition: definition,
	})
}

func (h *MetadataHandler) loadMetadata(
	ctx context.Context,
	conn *model.DBConnection,
	password string,
	selectedDatabase string,
	selectedSchema string,
) (*metadataResponse, error) {
	switch conn.DBType {
	case "postgres", "postgresql":
		return h.loadPostgresMetadata(ctx, conn, password, selectedDatabase, selectedSchema)
	case "redis":
		return &metadataResponse{
			DBType: "redis",
			Level:  "redis_db",
			Items:  buildRedisMetadataItems(),
		}, nil
	default:
		return h.loadMySQLMetadata(ctx, conn, password, selectedDatabase)
	}
}

func (h *MetadataHandler) loadSearchIndex(
	ctx context.Context,
	conn *model.DBConnection,
	password string,
	limit int,
) (*metadataSearchIndexResponse, error) {
	if limit <= 0 {
		limit = metadataSearchIndexLimit
	}

	switch conn.DBType {
	case "postgres", "postgresql":
		return h.loadPostgresSearchIndex(ctx, conn, password, limit)
	case "redis":
		items := buildRedisMetadataItems()
		if len(items) > limit {
			return &metadataSearchIndexResponse{DBType: "redis", Items: items[:limit], Limit: limit, Truncated: true}, nil
		}
		return &metadataSearchIndexResponse{DBType: "redis", Items: items, Limit: limit}, nil
	default:
		return h.loadMySQLSearchIndex(ctx, conn, password, limit)
	}
}

func (h *MetadataHandler) loadMySQLSearchIndex(
	ctx context.Context,
	conn *model.DBConnection,
	password string,
	limit int,
) (*metadataSearchIndexResponse, error) {
	queryDB, cleanup, err := h.openQueryDB(conn, password, "")
	if err != nil {
		return nil, err
	}
	defer cleanup()

	schemaRows, err := queryDB.QueryContext(ctx,
		`SELECT SCHEMA_NAME
		 FROM information_schema.SCHEMATA
		 WHERE SCHEMA_NAME NOT IN ('information_schema', 'performance_schema', 'mysql', 'sys')
		 ORDER BY SCHEMA_NAME`)
	if err != nil {
		return nil, err
	}
	defer schemaRows.Close()

	items := make([]metadataItem, 0)
	seenDatabases := make(map[string]bool)
	truncated := false
	for schemaRows.Next() {
		var database string
		if err := schemaRows.Scan(&database); err != nil {
			return nil, err
		}
		if len(items) >= limit {
			truncated = true
			break
		}
		items = append(items, metadataItem{Kind: "database", Name: database, Schema: database})
		seenDatabases[database] = true
	}
	if err := schemaRows.Err(); err != nil {
		return nil, err
	}
	if truncated {
		return &metadataSearchIndexResponse{DBType: "mysql", Items: items, Limit: limit, Truncated: truncated}, nil
	}

	rows, err := queryDB.QueryContext(ctx,
		`SELECT
			TABLE_SCHEMA,
			TABLE_NAME,
			IFNULL(ENGINE, '') AS ENGINE,
			IFNULL(TABLE_ROWS, 0) AS TABLE_ROWS,
			IFNULL(DATA_LENGTH, 0) AS DATA_LENGTH,
			IFNULL(INDEX_LENGTH, 0) AS INDEX_LENGTH,
			IFNULL(TABLE_COMMENT, '') AS TABLE_COMMENT
		FROM information_schema.TABLES
		WHERE TABLE_TYPE = 'BASE TABLE'
		  AND TABLE_SCHEMA NOT IN ('information_schema', 'performance_schema', 'mysql', 'sys')
		ORDER BY TABLE_SCHEMA, TABLE_NAME
		LIMIT ?`,
		limit+1,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var table metadataItem
		if err := rows.Scan(&table.Schema, &table.Name, &table.Engine, &table.RowCount, &table.DataSize, &table.IndexSize, &table.Comment); err != nil {
			return nil, err
		}
		if len(items) >= limit {
			truncated = true
			break
		}
		if !seenDatabases[table.Schema] {
			items = append(items, metadataItem{Kind: "database", Name: table.Schema, Schema: table.Schema})
			seenDatabases[table.Schema] = true
		}
		if len(items) >= limit {
			truncated = true
			break
		}
		table.Kind = "table"
		table.Database = table.Schema
		items = append(items, table)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &metadataSearchIndexResponse{DBType: "mysql", Items: items, Limit: limit, Truncated: truncated}, nil
}

func (h *MetadataHandler) loadPostgresSearchIndex(
	ctx context.Context,
	conn *model.DBConnection,
	password string,
	limit int,
) (*metadataSearchIndexResponse, error) {
	baseDB, cleanup, err := h.openQueryDB(conn, password, connectionDatabaseName(conn))
	if err != nil {
		return nil, err
	}
	defer cleanup()

	dbRows, err := baseDB.QueryContext(ctx,
		`SELECT datname
		 FROM pg_database
		 WHERE datistemplate = false
		   AND datallowconn = true
		 ORDER BY datname`)
	if err != nil {
		return nil, err
	}
	defer dbRows.Close()

	databases := make([]string, 0)
	for dbRows.Next() {
		var name string
		if err := dbRows.Scan(&name); err != nil {
			return nil, err
		}
		if shouldSkipPostgresMetadataDatabase(name) {
			continue
		}
		databases = append(databases, name)
	}
	if err := dbRows.Err(); err != nil {
		return nil, err
	}

	items := make([]metadataItem, 0)
	truncated := false
	for _, database := range databases {
		if len(items) >= limit {
			truncated = true
			break
		}
		items = append(items, metadataItem{Kind: "database", Name: database})

		queryDB, cleanup, err := h.openQueryDB(conn, password, database)
		if err != nil {
			return nil, err
		}

		schemaRows, err := queryDB.QueryContext(ctx,
			`SELECT schema_name
			 FROM information_schema.schemata
			 WHERE schema_name NOT IN ('information_schema', 'pg_catalog')
			 ORDER BY CASE WHEN schema_name = 'public' THEN 0 ELSE 1 END, schema_name`)
		if err != nil {
			cleanup()
			return nil, err
		}
		seenSchemas := make(map[string]bool)
		for schemaRows.Next() {
			var schema string
			if err := schemaRows.Scan(&schema); err != nil {
				schemaRows.Close()
				cleanup()
				return nil, err
			}
			if len(items) >= limit {
				truncated = true
				break
			}
			items = append(items, metadataItem{Kind: "schema", Name: schema, Database: database, Schema: schema})
			seenSchemas[schema] = true
		}
		if err := schemaRows.Err(); err != nil {
			schemaRows.Close()
			cleanup()
			return nil, err
		}
		schemaRows.Close()
		if truncated {
			cleanup()
			break
		}

		rows, err := queryDB.QueryContext(ctx,
			`SELECT
				n.nspname AS table_schema,
				c.relname AS table_name
			 FROM pg_class c
			 JOIN pg_namespace n ON n.oid = c.relnamespace
			 WHERE n.nspname NOT IN ('information_schema', 'pg_catalog')
			   AND c.relkind IN ('r', 'p')
			 ORDER BY n.nspname, c.relname`)
		if err != nil {
			cleanup()
			return nil, err
		}

		for rows.Next() {
			var schema string
			var table string
			if err := rows.Scan(&schema, &table); err != nil {
				rows.Close()
				cleanup()
				return nil, err
			}
			if !seenSchemas[schema] {
				if len(items) >= limit {
					truncated = true
					break
				}
				items = append(items, metadataItem{Kind: "schema", Name: schema, Database: database, Schema: schema})
				seenSchemas[schema] = true
			}
			if len(items) >= limit {
				truncated = true
				break
			}
			items = append(items, metadataItem{Kind: "table", Name: table, Database: database, Schema: schema})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			cleanup()
			return nil, err
		}
		rows.Close()
		cleanup()
		if truncated {
			break
		}
	}

	return &metadataSearchIndexResponse{DBType: "postgres", Items: items, Limit: limit, Truncated: truncated}, nil
}

func (h *MetadataHandler) loadMySQLMetadata(
	ctx context.Context,
	conn *model.DBConnection,
	password string,
	selectedDatabase string,
) (*metadataResponse, error) {
	queryDB, cleanup, err := h.openQueryDB(conn, password, "")
	if err != nil {
		return nil, err
	}
	defer cleanup()

	if selectedDatabase == "" {
		rows, err := queryDB.QueryContext(ctx,
			`SELECT SCHEMA_NAME
			 FROM information_schema.SCHEMATA
			 WHERE SCHEMA_NAME NOT IN ('information_schema', 'performance_schema', 'mysql', 'sys')
			 ORDER BY SCHEMA_NAME`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		items := make([]metadataItem, 0)
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return nil, err
			}
			items = append(items, metadataItem{Kind: "database", Name: name, Schema: name})
		}
		return &metadataResponse{
			DBType: "mysql",
			Level:  "database",
			Items:  items,
		}, rows.Err()
	}

	rows, err := queryDB.QueryContext(ctx,
		`SELECT
			TABLE_SCHEMA,
			TABLE_NAME,
			IFNULL(ENGINE, '') AS ENGINE,
			IFNULL(TABLE_ROWS, 0) AS TABLE_ROWS,
			IFNULL(DATA_LENGTH, 0) AS DATA_LENGTH,
			IFNULL(INDEX_LENGTH, 0) AS INDEX_LENGTH,
			IFNULL(TABLE_COMMENT, '') AS TABLE_COMMENT
		FROM information_schema.TABLES
		WHERE TABLE_TYPE = 'BASE TABLE'
		  AND TABLE_SCHEMA = ?
		ORDER BY TABLE_NAME`,
		selectedDatabase,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]metadataItem, 0)
	for rows.Next() {
		var item metadataItem
		if err := rows.Scan(&item.Schema, &item.Name, &item.Engine, &item.RowCount, &item.DataSize, &item.IndexSize, &item.Comment); err != nil {
			return nil, err
		}
		item.Kind = "table"
		item.Database = selectedDatabase
		items = append(items, item)
	}

	return &metadataResponse{
		DBType:   "mysql",
		Level:    "table",
		Database: selectedDatabase,
		Items:    items,
	}, rows.Err()
}

func (h *MetadataHandler) loadPostgresMetadata(
	ctx context.Context,
	conn *model.DBConnection,
	password string,
	selectedDatabase string,
	selectedSchema string,
) (*metadataResponse, error) {
	if selectedDatabase == "" {
		queryDB, cleanup, err := h.openQueryDB(conn, password, connectionDatabaseName(conn))
		if err != nil {
			return nil, err
		}
		defer cleanup()

		rows, err := queryDB.QueryContext(ctx,
			`SELECT datname
			 FROM pg_database
			 WHERE datistemplate = false
			   AND datallowconn = true
			 ORDER BY datname`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		items := make([]metadataItem, 0)
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return nil, err
			}
			if shouldSkipPostgresMetadataDatabase(name) {
				continue
			}
			items = append(items, metadataItem{Kind: "database", Name: name})
		}
		return &metadataResponse{
			DBType: "postgres",
			Level:  "database",
			Items:  items,
		}, rows.Err()
	}

	queryDB, cleanup, err := h.openQueryDB(conn, password, selectedDatabase)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	if selectedSchema == "" {
		rows, err := queryDB.QueryContext(ctx,
			`SELECT schema_name
			 FROM information_schema.schemata
			 WHERE schema_name NOT IN ('information_schema', 'pg_catalog')
			 ORDER BY CASE WHEN schema_name = 'public' THEN 0 ELSE 1 END, schema_name`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		items := make([]metadataItem, 0)
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return nil, err
			}
			items = append(items, metadataItem{
				Kind:     "schema",
				Name:     name,
				Database: selectedDatabase,
				Schema:   name,
			})
		}
		return &metadataResponse{
			DBType:   "postgres",
			Level:    "schema",
			Database: selectedDatabase,
			Items:    items,
		}, rows.Err()
	}

	rows, err := queryDB.QueryContext(ctx,
		`SELECT
			n.nspname AS table_schema,
			c.relname AS table_name
		 FROM pg_class c
		 JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = $1
		   AND c.relkind IN ('r', 'p')
		 ORDER BY c.relname`,
		selectedSchema,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]metadataItem, 0)
	for rows.Next() {
		var schemaName string
		var tableName string
		if err := rows.Scan(&schemaName, &tableName); err != nil {
			return nil, err
		}
		items = append(items, metadataItem{
			Kind:     "table",
			Name:     tableName,
			Database: selectedDatabase,
			Schema:   schemaName,
		})
	}

	return &metadataResponse{
		DBType:   "postgres",
		Level:    "table",
		Database: selectedDatabase,
		Schema:   selectedSchema,
		Items:    items,
	}, rows.Err()
}

func (h *MetadataHandler) loadColumns(
	ctx context.Context,
	conn *model.DBConnection,
	password string,
	selectedDatabase string,
	schema string,
	table string,
) ([]columnInfo, string, error) {
	switch conn.DBType {
	case "postgres", "postgresql":
		if selectedDatabase == "" {
			selectedDatabase = connectionDatabaseName(conn)
		}
		queryDB, cleanup, err := h.openQueryDB(conn, password, selectedDatabase)
		if err != nil {
			return nil, "", err
		}
		defer cleanup()

		rows, err := queryDB.QueryContext(ctx,
			`SELECT column_name, data_type, udt_name, is_nullable, COALESCE(column_default,''), ''
			 FROM information_schema.columns
			 WHERE table_schema = $1 AND table_name = $2
			 ORDER BY ordinal_position`,
			schema, table,
		)
		if err != nil {
			return nil, "", err
		}
		defer rows.Close()

		columns := make([]columnInfo, 0)
		for rows.Next() {
			var c columnInfo
			if err := rows.Scan(&c.Name, &c.DataType, &c.ColumnType, &c.IsNullable, &c.Default, &c.Comment); err != nil {
				return nil, "", err
			}
			columns = append(columns, c)
		}
		return columns, selectedDatabase, rows.Err()
	case "redis":
		return []columnInfo{}, "", nil
	default:
		queryDB, cleanup, err := h.openQueryDB(conn, password, "")
		if err != nil {
			return nil, "", err
		}
		defer cleanup()

		targetSchema := schema
		if selectedDatabase != "" {
			targetSchema = selectedDatabase
		}

		rows, err := queryDB.QueryContext(ctx,
			`SELECT COLUMN_NAME, DATA_TYPE, COLUMN_TYPE, IS_NULLABLE, IFNULL(COLUMN_DEFAULT,''), IFNULL(COLUMN_COMMENT,'')
			 FROM information_schema.COLUMNS
			 WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
			 ORDER BY ORDINAL_POSITION`,
			targetSchema, table,
		)
		if err != nil {
			return nil, "", err
		}
		defer rows.Close()

		columns := make([]columnInfo, 0)
		for rows.Next() {
			var c columnInfo
			if err := rows.Scan(&c.Name, &c.DataType, &c.ColumnType, &c.IsNullable, &c.Default, &c.Comment); err != nil {
				return nil, "", err
			}
			columns = append(columns, c)
		}
		return columns, targetSchema, rows.Err()
	}
}

func (h *MetadataHandler) loadDefinition(
	ctx context.Context,
	conn *model.DBConnection,
	password string,
	selectedDatabase string,
	schema string,
	table string,
) (string, string, error) {
	switch conn.DBType {
	case "postgres", "postgresql":
		if selectedDatabase == "" {
			selectedDatabase = connectionDatabaseName(conn)
		}
		queryDB, cleanup, err := h.openQueryDB(conn, password, selectedDatabase)
		if err != nil {
			return "", "", err
		}
		defer cleanup()

		definition, err := loadPostgresTableDefinition(ctx, queryDB, schema, table)
		if err != nil {
			return "", "", err
		}
		return definition, selectedDatabase, nil
	case "redis":
		return "", "", fmt.Errorf("redis does not support table definitions")
	default:
		queryDB, cleanup, err := h.openQueryDB(conn, password, "")
		if err != nil {
			return "", "", err
		}
		defer cleanup()

		targetSchema := schema
		if selectedDatabase != "" {
			targetSchema = selectedDatabase
		}

		query := fmt.Sprintf("SHOW CREATE TABLE %s.%s", quoteMySQLIdentifier(targetSchema), quoteMySQLIdentifier(table))
		row := queryDB.QueryRowContext(ctx, query)

		var tableName string
		var definition string
		if err := row.Scan(&tableName, &definition); err != nil {
			return "", "", err
		}
		return definition, targetSchema, nil
	}
}

func (h *MetadataHandler) openQueryDB(
	conn *model.DBConnection,
	password string,
	databaseOverride string,
) (*sql.DB, func(), error) {
	switch conn.DBType {
	case "postgres", "postgresql":
		targetDatabase := databaseOverride
		if targetDatabase == "" {
			targetDatabase = connectionDatabaseName(conn)
		}
		if targetDatabase == connectionDatabaseName(conn) {
			driver, dsn := pool.BuildDSN(conn, password)
			pools, err := pool.Global().GetOrCreate(conn.ID, driver, dsn)
			if err != nil {
				return nil, nil, fmt.Errorf("cannot connect: %w", err)
			}
			return pools.QueryPool, func() {}, nil
		}

		dbName := targetDatabase
		dsn := pool.BuildPostgresDSN(conn.Host, conn.Port, conn.Username, password, &dbName, conn.SSLMode)
		db, err := pool.Open("pgx", dsn, pool.ProfileScopedPGQuery)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot connect: %w", err)
		}
		return db, func() { _ = db.Close() }, nil
	case "redis":
		return nil, nil, fmt.Errorf("redis does not support sql metadata pools")
	default:
		driver, dsn := pool.BuildDSN(conn, password)
		pools, err := pool.Global().GetOrCreate(conn.ID, driver, dsn)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot connect: %w", err)
		}
		return pools.QueryPool, func() {}, nil
	}
}

func connectionDatabaseName(conn *model.DBConnection) string {
	if conn.DatabaseName == nil {
		return ""
	}
	return strings.TrimSpace(*conn.DatabaseName)
}

func quotePostgresIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func loadPostgresTableDefinition(ctx context.Context, db *sql.DB, schema string, table string) (string, error) {
	const columnsQuery = `SELECT
		a.attname,
		pg_catalog.format_type(a.atttypid, a.atttypmod) AS data_type,
		COALESCE(pg_get_expr(ad.adbin, ad.adrelid), '') AS default_expr,
		NOT a.attnotnull AS is_nullable,
		COALESCE(a.attidentity, '') AS identity_type,
		COALESCE(a.attgenerated, '') AS generated
	FROM pg_attribute a
	JOIN pg_class c ON c.oid = a.attrelid
	JOIN pg_namespace n ON n.oid = c.relnamespace
	LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
	WHERE n.nspname = $1
	  AND c.relname = $2
	  AND c.relkind IN ('r', 'p')
	  AND a.attnum > 0
	  AND NOT a.attisdropped
	ORDER BY a.attnum`

	columnRows, err := db.QueryContext(ctx, columnsQuery, schema, table)
	if err != nil {
		return "", err
	}
	defer columnRows.Close()

	columns := make([]postgresDefinitionColumn, 0)
	for columnRows.Next() {
		var column postgresDefinitionColumn
		if err := columnRows.Scan(
			&column.Name,
			&column.DataType,
			&column.DefaultExpr,
			&column.IsNullable,
			&column.IdentityType,
			&column.Generated,
		); err != nil {
			return "", err
		}
		columns = append(columns, column)
	}
	if err := columnRows.Err(); err != nil {
		return "", err
	}
	if len(columns) == 0 {
		return "", sql.ErrNoRows
	}

	regclass := fmt.Sprintf("%s.%s", quotePostgresIdentifier(schema), quotePostgresIdentifier(table))
	constraintsRows, err := db.QueryContext(ctx, `SELECT
		conname,
		pg_get_constraintdef(oid, true) AS definition
	FROM pg_constraint
	WHERE conrelid = $1::regclass
	ORDER BY CASE contype
		WHEN 'p' THEN 0
		WHEN 'u' THEN 1
		WHEN 'f' THEN 2
		WHEN 'c' THEN 3
		ELSE 4
	END, conname`, regclass)
	if err != nil {
		return "", err
	}
	defer constraintsRows.Close()

	constraints := make([]postgresDefinitionConstraint, 0)
	for constraintsRows.Next() {
		var constraint postgresDefinitionConstraint
		if err := constraintsRows.Scan(&constraint.Name, &constraint.Definition); err != nil {
			return "", err
		}
		constraints = append(constraints, constraint)
	}
	if err := constraintsRows.Err(); err != nil {
		return "", err
	}

	indexRows, err := db.QueryContext(ctx, `SELECT
		i.relname AS index_name,
		pg_get_indexdef(i.oid) AS definition
	FROM pg_class t
	JOIN pg_namespace n ON n.oid = t.relnamespace
	JOIN pg_index ix ON ix.indrelid = t.oid
	JOIN pg_class i ON i.oid = ix.indexrelid
	LEFT JOIN pg_constraint c ON c.conindid = i.oid
	WHERE n.nspname = $1
	  AND t.relname = $2
	  AND c.oid IS NULL
	ORDER BY i.relname`, schema, table)
	if err != nil {
		return "", err
	}
	defer indexRows.Close()

	indexes := make([]postgresDefinitionIndex, 0)
	for indexRows.Next() {
		var index postgresDefinitionIndex
		if err := indexRows.Scan(&index.Name, &index.Definition); err != nil {
			return "", err
		}
		indexes = append(indexes, index)
	}
	if err := indexRows.Err(); err != nil {
		return "", err
	}

	return buildPostgresTableDefinition(schema, table, columns, constraints, indexes), nil
}

func buildPostgresTableDefinition(
	schema string,
	table string,
	columns []postgresDefinitionColumn,
	constraints []postgresDefinitionConstraint,
	indexes []postgresDefinitionIndex,
) string {
	lines := make([]string, 0, len(columns)+len(constraints))
	for _, column := range columns {
		parts := []string{
			fmt.Sprintf("    %s %s", quotePostgresIdentifier(column.Name), column.DataType),
		}
		if column.Generated == "s" && column.DefaultExpr != "" {
			parts = append(parts, fmt.Sprintf("GENERATED ALWAYS AS (%s) STORED", column.DefaultExpr))
		} else if column.IdentityType == "a" {
			parts = append(parts, "GENERATED ALWAYS AS IDENTITY")
		} else if column.IdentityType == "d" {
			parts = append(parts, "GENERATED BY DEFAULT AS IDENTITY")
		} else if strings.TrimSpace(column.DefaultExpr) != "" {
			parts = append(parts, fmt.Sprintf("DEFAULT %s", column.DefaultExpr))
		}
		if !column.IsNullable {
			parts = append(parts, "NOT NULL")
		}
		lines = append(lines, strings.Join(parts, " "))
	}

	for _, constraint := range constraints {
		lines = append(lines, fmt.Sprintf("    CONSTRAINT %s %s", quotePostgresIdentifier(constraint.Name), constraint.Definition))
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("CREATE TABLE %s.%s (\n", quotePostgresIdentifier(schema), quotePostgresIdentifier(table)))
	builder.WriteString(strings.Join(lines, ",\n"))
	builder.WriteString("\n);")

	if len(indexes) > 0 {
		builder.WriteString("\n\n")
		for indexPosition, index := range indexes {
			if indexPosition > 0 {
				builder.WriteString("\n")
			}
			builder.WriteString(index.Definition)
			builder.WriteString(";")
		}
	}

	return builder.String()
}

func buildRedisMetadataItems() []metadataItem {
	items := make([]metadataItem, 0, 16)
	for i := range 16 {
		name := strconv.Itoa(i)
		items = append(items, metadataItem{
			Kind:     "redis_db",
			Name:     name,
			Database: name,
			Schema:   name,
		})
	}
	return items
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
