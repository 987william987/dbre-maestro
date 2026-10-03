package tableschema

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/pool"
)

type CredentialResolver interface {
	ResolveCredential(*model.DBConnection, string) (*model.DBConnection, string, error)
}

type Queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Snapshot struct {
	Database string          `json:"database"`
	Tables   []TableSnapshot `json:"tables"`
}

type TableSnapshot struct {
	Name      string       `json:"name"`
	CreateSQL string       `json:"create_sql"`
	Options   TableOptions `json:"options"`
}

func OpenReadonly(ctx context.Context, resolver CredentialResolver, connection *model.DBConnection) (*sql.DB, error) {
	return openRole(ctx, resolver, connection, model.DBCredentialRoleReadonly, pool.Open)
}

func openReadonly(ctx context.Context, resolver CredentialResolver, connection *model.DBConnection, opener func(string, string, pool.Profile) (*sql.DB, error)) (*sql.DB, error) {
	return openRole(ctx, resolver, connection, model.DBCredentialRoleReadonly, opener)
}

func OpenReadwrite(ctx context.Context, resolver CredentialResolver, connection *model.DBConnection) (*sql.DB, error) {
	return openRole(ctx, resolver, connection, model.DBCredentialRoleReadwrite, pool.Open)
}

func openRole(ctx context.Context, resolver CredentialResolver, connection *model.DBConnection, role string, opener func(string, string, pool.Profile) (*sql.DB, error)) (*sql.DB, error) {
	if connection == nil || !strings.EqualFold(connection.DBType, "mysql") {
		return nil, &StableError{Code: ErrorUnsupportedEngine, Message: "table schemas require a MySQL connection"}
	}
	resolved, password, err := resolver.ResolveCredential(connection, role)
	if err != nil {
		return nil, fmt.Errorf("resolve readonly credential: %w", err)
	}
	driver, dsn := pool.BuildDSN(resolved, password)
	db, err := opener(driver, dsn, pool.ProfileMetadata)
	if err != nil {
		return nil, fmt.Errorf("open readonly schema connection: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping readonly schema connection: %w", err)
	}
	return db, nil
}

func LoadSnapshot(ctx context.Context, db Queryer, database string, tables []string) (*Snapshot, error) {
	if err := validateIdentifier(database); err != nil {
		return nil, &StableError{Code: ErrorInvalidTableSelection, Message: "invalid source database"}
	}
	if len(tables) == 0 || len(tables) > MaxTables {
		return nil, &StableError{Code: ErrorInvalidTableSelection, Message: fmt.Sprintf("select between 1 and %d tables", MaxTables)}
	}
	names := append([]string(nil), tables...)
	sort.Strings(names)
	for index, table := range names {
		if err := validateIdentifier(table); err != nil || (index > 0 && table == names[index-1]) {
			return nil, &StableError{Code: ErrorInvalidTableSelection, Message: "table names must be unique valid MySQL identifiers"}
		}
	}
	result := &Snapshot{Database: database, Tables: make([]TableSnapshot, 0, len(names))}
	totalBytes := 0
	for _, table := range names {
		query := fmt.Sprintf("SHOW CREATE TABLE %s.%s", QuoteIdentifier(database), QuoteIdentifier(table))
		var returnedName, createSQL string
		if err := db.QueryRowContext(ctx, query).Scan(&returnedName, &createSQL); err != nil {
			return nil, fmt.Errorf("load create table for %s: %w", table, err)
		}
		if returnedName != table {
			return nil, fmt.Errorf("show create table returned %q for requested table %q", returnedName, table)
		}
		if len(createSQL) > MaxTableDDLBytes || totalBytes+len(createSQL) > MaxExportBytes {
			return nil, &StableError{Code: ErrorExportTooLarge, Message: "source table DDL exceeds the export size limit"}
		}
		totalBytes += len(createSQL)
		parsed, err := ParseCreateTable(createSQL)
		if err != nil {
			return nil, fmt.Errorf("parse create table for %s: %w", table, err)
		}
		result.Tables = append(result.Tables, TableSnapshot{Name: table, CreateSQL: createSQL, Options: parsed.Options})
	}
	return result, nil
}

func LoadCapabilities(ctx context.Context, db Queryer) (Capabilities, error) {
	capabilities := Capabilities{Engines: map[string]bool{}, Charsets: map[string]map[string]bool{}}
	rows, err := db.QueryContext(ctx, "SHOW ENGINES")
	if err != nil {
		return Capabilities{}, fmt.Errorf("show engines: %w", err)
	}
	for rows.Next() {
		var engine, support, comment string
		var transactions, xa, savepoints sql.NullString
		if err := rows.Scan(&engine, &support, &comment, &transactions, &xa, &savepoints); err != nil {
			_ = rows.Close()
			return Capabilities{}, fmt.Errorf("scan engine capability: %w", err)
		}
		capabilities.Engines[strings.ToLower(engine)] = !strings.EqualFold(support, "NO")
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return Capabilities{}, fmt.Errorf("iterate engine capabilities: %w", err)
	}
	if err := rows.Close(); err != nil {
		return Capabilities{}, err
	}
	rows, err = db.QueryContext(ctx, `SELECT CHARACTER_SET_NAME, COLLATION_NAME FROM information_schema.COLLATIONS ORDER BY CHARACTER_SET_NAME, COLLATION_NAME`)
	if err != nil {
		return Capabilities{}, fmt.Errorf("load charset capabilities: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var charset, collation string
		if err := rows.Scan(&charset, &collation); err != nil {
			return Capabilities{}, fmt.Errorf("scan charset capability: %w", err)
		}
		key := strings.ToLower(charset)
		if capabilities.Charsets[key] == nil {
			capabilities.Charsets[key] = map[string]bool{}
		}
		capabilities.Charsets[key][strings.ToLower(collation)] = true
	}
	return capabilities, rows.Err()
}

func QuoteIdentifier(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}

func validateIdentifier(value string) error {
	if value == "" || len([]byte(value)) > 64 || strings.ContainsRune(value, 0) {
		return fmt.Errorf("invalid MySQL identifier")
	}
	return nil
}
