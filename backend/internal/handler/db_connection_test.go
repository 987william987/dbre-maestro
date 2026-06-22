package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/jmoiron/sqlx"
)

func dbConnectionRows(databaseName any) *sqlmock.Rows {
	now := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	return sqlmock.NewRows([]string{
		"id",
		"name",
		"db_type",
		"host",
		"port",
		"readonly_host",
		"readonly_port",
		"readwrite_host",
		"readwrite_port",
		"database_name",
		"username",
		"password_encrypted",
		"encryption_key_version",
		"ssl_mode",
		"extra_params",
		"last_test_status",
		"last_test_error",
		"last_tested_at",
		"created_by",
		"created_at",
		"updated_at",
	}).
		AddRow(5, "analytics", "mysql", "db.internal", 3306, "db.internal", 3306, "db-write.internal", 3307, databaseName, "readonly", []byte("cipher"), 1, "prefer", nil, nil, nil, nil, 1, now, now)
}

func TestDBConnectionHandlerPatchAllowsClearingDatabaseName(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewDBConnectionHandler(
		repository.NewDBConnectionRepo(sqlxDB, []byte("01234567890123456789012345678901")),
		repository.NewUserRepo(sqlxDB),
		repository.NewAuthGroupRepo(sqlxDB),
		repository.NewAuditRepo(sqlxDB),
	)

	now := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT \* FROM db_connections WHERE id = \?`).
		WithArgs(uint64(5)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"name",
			"db_type",
			"host",
			"port",
			"readonly_host",
			"readonly_port",
			"readwrite_host",
			"readwrite_port",
			"database_name",
			"username",
			"password_encrypted",
			"encryption_key_version",
			"ssl_mode",
			"extra_params",
			"last_test_status",
			"last_test_error",
			"last_tested_at",
			"created_by",
			"created_at",
			"updated_at",
		}).AddRow(5, "analytics", "mysql", "db.internal", 3306, "db.internal", 3306, "db-write.internal", 3307, "analytics", "readonly", []byte(nil), 1, "prefer", nil, nil, nil, nil, 1, now, now))
	mock.ExpectQuery(`SELECT \* FROM db_connection_credentials WHERE db_connection_id = \?`).
		WithArgs(uint64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "db_connection_id", "credential_role", "username", "password_encrypted", "encryption_key_version", "created_at", "updated_at"}))
	mock.ExpectExec(`UPDATE db_connections`).
		WithArgs("analytics", "mysql", "db.internal", uint16(3306), "db.internal", uint16(3306), "db-write.internal", uint16(3307), nil, "readonly", "prefer", sqlmock.AnyArg(), uint64(5)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO audit_logs`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`SELECT \* FROM db_connections WHERE id = \?`).
		WithArgs(uint64(5)).
		WillReturnRows(dbConnectionRows(nil))
	mock.ExpectQuery(`SELECT \* FROM db_connection_credentials WHERE db_connection_id = \?`).
		WithArgs(uint64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "db_connection_id", "credential_role", "username", "password_encrypted", "encryption_key_version", "created_at", "updated_at"}))

	req := withURLParam(httptest.NewRequest(http.MethodPatch, "/db-connections/5", strings.NewReader(`{"database_name":null}`)), "id", "5")
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, uint64(99))
	ctx = context.WithValue(ctx, middleware.CtxUsername, "operator")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.Patch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"analytics"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"database_name":"analytics"`) {
		t.Fatalf("body = %s, want database_name cleared", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestDBConnectionHandlerCreateRedisAllowsEmptyUsername(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewDBConnectionHandler(
		repository.NewDBConnectionRepo(sqlxDB, []byte("01234567890123456789012345678901")),
		repository.NewUserRepo(sqlxDB),
		repository.NewAuthGroupRepo(sqlxDB),
		repository.NewAuditRepo(sqlxDB),
	)

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO db_connections`).
		WithArgs("cache-redis", "redis", "redis.internal", uint16(6379), "redis.internal", uint16(6379), "redis.internal", uint16(6379), nil, "", sqlmock.AnyArg(), "prefer", uint64(99), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(7, 1))
	mock.ExpectQuery(`SELECT \* FROM db_connection_credentials WHERE db_connection_id = \?`).
		WithArgs(uint64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "db_connection_id", "credential_role", "username", "password_encrypted", "encryption_key_version", "created_at", "updated_at"}))
	mock.ExpectExec(`DELETE FROM db_connection_credentials WHERE db_connection_id = \?`).
		WithArgs(uint64(7)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT \* FROM db_connections WHERE id = \?`).
		WithArgs(uint64(7)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"name",
			"db_type",
			"host",
			"port",
			"readonly_host",
			"readonly_port",
			"readwrite_host",
			"readwrite_port",
			"database_name",
			"username",
			"password_encrypted",
			"encryption_key_version",
			"ssl_mode",
			"extra_params",
			"last_test_status",
			"last_test_error",
			"last_tested_at",
			"created_by",
			"created_at",
			"updated_at",
		}).AddRow(7, "cache-redis", "redis", "redis.internal", 6379, "redis.internal", 6379, "redis.internal", 6379, nil, "", []byte("cipher"), 1, "prefer", nil, nil, nil, nil, 99, time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)))
	mock.ExpectQuery(`SELECT \* FROM db_connection_credentials WHERE db_connection_id = \?`).
		WithArgs(uint64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "db_connection_id", "credential_role", "username", "password_encrypted", "encryption_key_version", "created_at", "updated_at"}))
	mock.ExpectExec(`INSERT INTO audit_logs`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	req := httptest.NewRequest(http.MethodPost, "/db-connections", strings.NewReader(`{"name":"cache-redis","db_type":"redis","host":"redis.internal","port":6379,"username":"","password":"secret","ssl_mode":"prefer","database_name":null}`))
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, uint64(99))
	ctx = context.WithValue(ctx, middleware.CtxUsername, "operator")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var got map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["username"] != "" {
		t.Fatalf("username = %#v, want empty string", got["username"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestDBConnectionHandlerCreatePostgresDefaultsDatabaseNameToPostgres(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewDBConnectionHandler(
		repository.NewDBConnectionRepo(sqlxDB, []byte("01234567890123456789012345678901")),
		repository.NewUserRepo(sqlxDB),
		repository.NewAuthGroupRepo(sqlxDB),
		repository.NewAuditRepo(sqlxDB),
	)

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO db_connections`).
		WithArgs("analytics-pg", "postgres", "pg.internal", uint16(5432), "pg.internal", uint16(5432), "pg.internal", uint16(5432), "postgres", "postgres", sqlmock.AnyArg(), "prefer", uint64(99), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(8, 1))
	mock.ExpectQuery(`SELECT \* FROM db_connection_credentials WHERE db_connection_id = \?`).
		WithArgs(uint64(8)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "db_connection_id", "credential_role", "username", "password_encrypted", "encryption_key_version", "created_at", "updated_at"}))
	mock.ExpectExec(`DELETE FROM db_connection_credentials WHERE db_connection_id = \?`).
		WithArgs(uint64(8)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT \* FROM db_connections WHERE id = \?`).
		WithArgs(uint64(8)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"name",
			"db_type",
			"host",
			"port",
			"readonly_host",
			"readonly_port",
			"readwrite_host",
			"readwrite_port",
			"database_name",
			"username",
			"password_encrypted",
			"encryption_key_version",
			"ssl_mode",
			"extra_params",
			"last_test_status",
			"last_test_error",
			"last_tested_at",
			"created_by",
			"created_at",
			"updated_at",
		}).AddRow(8, "analytics-pg", "postgres", "pg.internal", 5432, "pg.internal", 5432, "pg.internal", 5432, "postgres", "postgres", []byte("cipher"), 1, "prefer", nil, nil, nil, nil, 99, time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)))
	mock.ExpectQuery(`SELECT \* FROM db_connection_credentials WHERE db_connection_id = \?`).
		WithArgs(uint64(8)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "db_connection_id", "credential_role", "username", "password_encrypted", "encryption_key_version", "created_at", "updated_at"}))
	mock.ExpectExec(`INSERT INTO audit_logs`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	req := httptest.NewRequest(http.MethodPost, "/db-connections", strings.NewReader(`{"name":"analytics-pg","db_type":"postgres","host":"pg.internal","port":5432,"username":"postgres","password":"secret","ssl_mode":"prefer","database_name":null}`))
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, uint64(99))
	ctx = context.WithValue(ctx, middleware.CtxUsername, "operator")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestTicketHandlerCreateRejectsSQLEditorTicketTypes(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewTicketHandler(
		nil,
		nil,
		nil,
		repository.NewAuditRepo(sqlxDB),
		nil,
		repository.NewDBConnectionRepo(sqlxDB, []byte("01234567890123456789012345678901")),
		repository.NewUserRepo(sqlxDB),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		"",
	)

	req := httptest.NewRequest(http.MethodPost, "/tickets", strings.NewReader(`{
		"title":"Export users",
		"sql_content":"SELECT * FROM users",
		"ticket_type":"sql_export",
		"db_connection_id":2
	}`))
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, uint64(99))
	ctx = context.WithValue(ctx, middleware.CtxUsername, "alan")
	ctx = context.WithValue(ctx, middleware.CtxPermissions, []string{"tickets.apply"})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.Create(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "must be created from SQL Editor") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
