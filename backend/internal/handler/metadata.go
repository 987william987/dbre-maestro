package handler

import (
	"context"
	"database/sql"
	"fmt"
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
		jsonErr(w, http.StatusInternalServerError, "query metadata failed: "+err.Error())
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
		jsonErr(w, http.StatusInternalServerError, "query columns failed: "+err.Error())
		return
	}

	jsonOK(w, map[string]any{
		"database": resolvedDatabase,
		"schema":   schema,
		"table":    table,
		"columns":  columns,
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
			table_schema,
			table_name
		 FROM information_schema.tables
		 WHERE table_type = 'BASE TABLE'
		   AND table_schema = $1
		 ORDER BY table_name`,
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
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot connect: %w", err)
		}
		db.SetMaxOpenConns(2)
		db.SetMaxIdleConns(1)
		db.SetConnMaxLifetime(2 * time.Minute)
		db.SetConnMaxIdleTime(1 * time.Minute)
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
