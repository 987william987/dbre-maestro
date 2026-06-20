package db

import (
	"database/sql"
	"fmt"
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
