package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jmoiron/sqlx"
)

func Open(dsn string) (*sqlx.DB, error) {
	db, err := sqlx.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)
	return db, nil
}

func Ping(db *sqlx.DB) error {
	return db.Ping()
}

// addDSNParam appends a query parameter to a MySQL DSN.
func addDSNParam(dsn, param string) string {
	if strings.Contains(dsn, "?") {
		return dsn + "&" + param
	}
	return dsn + "?" + param
}

func RunMigrations(dsn string, migrationsPath string) error {
	// multiStatements=true is required for migration files with multiple statements.
	// Only used for the migration connection, not the regular app connection.
	migrationDSN := addDSNParam(dsn, "multiStatements=true")
	rawDB, err := sql.Open("mysql", migrationDSN)
	if err != nil {
		return fmt.Errorf("open migration db: %w", err)
	}
	defer rawDB.Close()

	state, err := readMigrationState(rawDB)
	if err != nil {
		return fmt.Errorf("read migration state: %w", err)
	}
	logMigrationPlan(migrationsPath, state)

	// Clear version-0 state directly via SQL rather than using golang-migrate's
	// Force(), which fails when the target version has no file.
	if err := resetMigrationState(rawDB); err != nil {
		return fmt.Errorf("reset migration state: %w", err)
	}

	driver, err := mysql.WithInstance(rawDB, &mysql.Config{})
	if err != nil {
		return fmt.Errorf("migration driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		"file://"+migrationsPath,
		"mysql",
		driver,
	)
	if err != nil {
		return fmt.Errorf("migration init: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migration up: %w", err)
	}
	return nil
}

type migrationState struct {
	Exists  bool
	Version int
	Dirty   bool
}

type migrationFile struct {
	Version int
	Name    string
	Path    string
}

func readMigrationState(db *sql.DB) (migrationState, error) {
	var state migrationState
	var exists int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'schema_migrations'
	`).Scan(&exists)
	if err != nil || exists == 0 {
		return state, err
	}

	state.Exists = true
	err = db.QueryRow(`SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&state.Version, &state.Dirty)
	if err == sql.ErrNoRows {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	return state, nil
}

func logMigrationPlan(migrationsPath string, state migrationState) {
	files, err := listUpMigrationFiles(migrationsPath)
	if err != nil {
		slog.Warn("migration plan unavailable", "migrations_path", migrationsPath, "err", err)
		return
	}

	version := 0
	if state.Exists {
		version = state.Version
	}
	slog.Info("migration state loaded", "version", version, "dirty", state.Dirty, "migrations_path", migrationsPath)

	if state.Dirty {
		file := migrationFileForVersion(files, state.Version)
		if file == nil {
			slog.Warn("migration dirty state has no matching local file", "version", state.Version)
			return
		}
		slog.Error(
			"migration dirty state detected",
			"version", state.Version,
			"file", file.Name,
			"sql", migrationSQLPreview(file.Path),
		)
		return
	}

	pending := make([]string, 0)
	for _, file := range files {
		if file.Version > version {
			pending = append(pending, file.Name)
		}
	}
	slog.Info("migration pending files", "count", len(pending), "files", pending)
}

var migrationUpFilePattern = regexp.MustCompile(`^([0-9]+)_.+\.up\.sql$`)

func listUpMigrationFiles(migrationsPath string) ([]migrationFile, error) {
	entries, err := os.ReadDir(migrationsPath)
	if err != nil {
		return nil, err
	}
	files := make([]migrationFile, 0, len(entries)/2)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		matches := migrationUpFilePattern.FindStringSubmatch(name)
		if matches == nil {
			continue
		}
		version, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}
		files = append(files, migrationFile{
			Version: version,
			Name:    name,
			Path:    filepath.Join(migrationsPath, name),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Version < files[j].Version
	})
	return files, nil
}

func migrationFileForVersion(files []migrationFile, version int) *migrationFile {
	for i := range files {
		if files[i].Version == version {
			return &files[i]
		}
	}
	return nil
}

func migrationSQLPreview(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("unavailable: %v", err)
	}
	const limit = 4000
	sql := strings.TrimSpace(string(content))
	if len(sql) <= limit {
		return sql
	}
	return sql[:limit] + "\n...<truncated>"
}

// resetMigrationState clears version=0 rows from schema_migrations so
// golang-migrate can run from version 1. Dirty states must fail loudly because
// MySQL DDL is not transactional and old migrations are not fully idempotent.
func resetMigrationState(db *sql.DB) error {
	var exists int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'schema_migrations'
	`).Scan(&exists)
	if err != nil || exists == 0 {
		return nil
	}

	var version int
	var dirty bool
	err = db.QueryRow(`SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&version, &dirty)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}

	if dirty {
		return fmt.Errorf("dirty migration state at version %d", version)
	}
	if version == 0 {
		_, err = db.Exec(`DELETE FROM schema_migrations`)
		return err
	}
	return nil
}
