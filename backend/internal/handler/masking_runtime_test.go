package handler

import (
	"context"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/masking"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/jmoiron/sqlx"
)

func TestMaskingRuntimeApplyResultMasksMySQLColumns(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	engine, err := masking.NewEngine([]byte("0123456789abcdef0123456789abcdef"), masking.GlobalCache())
	if err != nil {
		t.Fatalf("masking.NewEngine: %v", err)
	}

	runtime := newMaskingRuntime(nil, repository.NewMaskingRuleRepo(sqlxDB), repository.NewMaskingWhitelistRepo(sqlxDB), nil, engine)
	conn := &model.DBConnection{ID: 7, DBType: "mysql"}
	result := &masking.QueryResult{
		Columns: []string{"email"},
		Origins: []masking.ColumnOrigin{{Database: "analytics", Table: "users", Column: "email"}},
		Rows:    [][]any{{"alice@example.com"}},
	}

	mock.ExpectQuery(`SELECT id, column_name, match_type, mask_mode, COALESCE\(mask_config, JSON_OBJECT\(\)\) AS mask_config, created_by, created_at`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "column_name", "match_type", "mask_mode", "mask_config", "created_by", "created_at"}).
			AddRow(1, "email", "exact", "full", []byte(`{}`), 1, time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)))
	mock.ExpectQuery(`SELECT COUNT\(\*\)\s+FROM masking_whitelist`).
		WithArgs(uint64(7), "analytics", "users", "email").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	override, sensitiveIndexes, err := runtime.applyResult(context.Background(), conn, 0, result)
	if err != nil {
		t.Fatalf("applyResult() error = %v", err)
	}
	if override {
		t.Fatalf("override = true, want false")
	}
	if len(sensitiveIndexes) != 1 || sensitiveIndexes[0] != 0 {
		t.Fatalf("sensitiveIndexes = %v, want [0]", sensitiveIndexes)
	}
	if got := result.Rows[0][0]; got != "****" {
		t.Fatalf("masked value = %v, want %q", got, "****")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestMaskingRuntimeApplyResultSkipsWhitelistMatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	engine, err := masking.NewEngine([]byte("0123456789abcdef0123456789abcdef"), masking.GlobalCache())
	if err != nil {
		t.Fatalf("masking.NewEngine: %v", err)
	}

	runtime := newMaskingRuntime(nil, repository.NewMaskingRuleRepo(sqlxDB), repository.NewMaskingWhitelistRepo(sqlxDB), nil, engine)
	conn := &model.DBConnection{ID: 7, DBType: "mysql"}
	result := &masking.QueryResult{
		Columns: []string{"email"},
		Origins: []masking.ColumnOrigin{{Database: "analytics", Table: "users", Column: "email"}},
		Rows:    [][]any{{"alice@example.com"}},
	}

	mock.ExpectQuery(`SELECT id, column_name, match_type, mask_mode, COALESCE\(mask_config, JSON_OBJECT\(\)\) AS mask_config, created_by, created_at`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "column_name", "match_type", "mask_mode", "mask_config", "created_by", "created_at"}).
			AddRow(1, "email", "exact", "full", []byte(`{}`), 1, time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)))
	mock.ExpectQuery(`SELECT COUNT\(\*\)\s+FROM masking_whitelist`).
		WithArgs(uint64(7), "analytics", "users", "email").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	override, sensitiveIndexes, err := runtime.applyResult(context.Background(), conn, 0, result)
	if err != nil {
		t.Fatalf("applyResult() error = %v", err)
	}
	if override {
		t.Fatalf("override = true, want false")
	}
	if len(sensitiveIndexes) != 0 {
		t.Fatalf("sensitiveIndexes = %v, want empty", sensitiveIndexes)
	}
	if got := result.Rows[0][0]; got != "alice@example.com" {
		t.Fatalf("masked value = %v, want original value", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
