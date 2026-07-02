package handler

import (
	"context"
	"encoding/json"
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

func TestMaskingRuntimeApplyResultMasksPostgresColumns(t *testing.T) {
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
	conn := &model.DBConnection{ID: 7, DBType: "postgres"}
	result := &masking.QueryResult{
		Columns: []string{"email"},
		Origins: []masking.ColumnOrigin{{Database: "app", Schema: "public", Table: "users", Column: "email"}},
		Rows:    [][]any{{"alice@example.com"}},
	}

	mock.ExpectQuery(`SELECT id, column_name, match_type, mask_mode, COALESCE\(mask_config, JSON_OBJECT\(\)\) AS mask_config, created_by, created_at`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "column_name", "match_type", "mask_mode", "mask_config", "created_by", "created_at"}).
			AddRow(1, "email", "exact", "full", []byte(`{}`), 1, time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)))
	mock.ExpectQuery(`SELECT COUNT\(\*\)\s+FROM masking_whitelist`).
		WithArgs(uint64(7), "app", "users", "email").
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

func TestMaskingRuntimeApplyResultFailsClosedWhenEngineMissing(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	runtime := newMaskingRuntime(nil, repository.NewMaskingRuleRepo(sqlxDB), repository.NewMaskingWhitelistRepo(sqlxDB), nil, nil)
	conn := &model.DBConnection{ID: 7, DBType: "postgres"}
	result := &masking.QueryResult{
		Columns: []string{"email"},
		Origins: []masking.ColumnOrigin{{Database: "app", Schema: "public", Table: "users", Column: "email"}},
		Rows:    [][]any{{"alice@example.com"}},
	}

	mock.ExpectQuery(`SELECT id, column_name, match_type, mask_mode, COALESCE\(mask_config, JSON_OBJECT\(\)\) AS mask_config, created_by, created_at`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "column_name", "match_type", "mask_mode", "mask_config", "created_by", "created_at"}).
			AddRow(1, "email", "exact", "full", []byte(`{}`), 1, time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)))
	mock.ExpectQuery(`SELECT COUNT\(\*\)\s+FROM masking_whitelist`).
		WithArgs(uint64(7), "app", "users", "email").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	_, _, err = runtime.applyResult(context.Background(), conn, 0, result)
	if err == nil {
		t.Fatal("applyResult() expected fail-closed error when engine is missing")
	}
	if got := result.Rows[0][0]; got != "alice@example.com" {
		t.Fatalf("value should remain unmodified on error, got %v", got)
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

func TestDecideMaskRuleForResultColumnFallsBackToFullMaskOnMixedModes(t *testing.T) {
	partialConfig, _ := json.Marshal(map[string]any{"keep_prefix": 1, "keep_suffix": 1})
	rule, ok := decideMaskRuleForResultColumn("profile", []matchedMaskRule{
		{
			Origin: masking.ColumnOrigin{Database: "analytics", Table: "users", Column: "user_name"},
			Rule: model.MaskingRule{
				MaskMode:   "partial",
				MaskConfig: partialConfig,
			},
		},
		{
			Origin: masking.ColumnOrigin{Database: "analytics", Table: "users", Column: "email"},
			Rule: model.MaskingRule{
				MaskMode: "email",
			},
		},
	})
	if !ok {
		t.Fatal("expected rule, got none")
	}
	if rule.Mode != masking.MaskModeFull {
		t.Fatalf("rule.Mode = %s, want %s", rule.Mode, masking.MaskModeFull)
	}
}

func TestDecideMaskRuleForResultColumnKeepsModeWhenAllDependenciesAgree(t *testing.T) {
	partialConfig, _ := json.Marshal(map[string]any{"keep_prefix": 1, "keep_suffix": 1})
	rule, ok := decideMaskRuleForResultColumn("profile", []matchedMaskRule{
		{
			Origin: masking.ColumnOrigin{Database: "analytics", Table: "users", Column: "first_name"},
			Rule: model.MaskingRule{
				MaskMode:   "partial",
				MaskConfig: partialConfig,
			},
		},
		{
			Origin: masking.ColumnOrigin{Database: "analytics", Table: "users", Column: "last_name"},
			Rule: model.MaskingRule{
				MaskMode:   "partial",
				MaskConfig: partialConfig,
			},
		},
	})
	if !ok {
		t.Fatal("expected rule, got none")
	}
	if rule.Mode != masking.MaskModePartial {
		t.Fatalf("rule.Mode = %s, want %s", rule.Mode, masking.MaskModePartial)
	}
	if string(rule.Config) != string(partialConfig) {
		t.Fatalf("rule.Config = %s, want %s", string(rule.Config), string(partialConfig))
	}
}
