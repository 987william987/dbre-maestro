package sqlreview_test

import (
	"context"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/sqlreview"
)

// explainCols matches MySQL EXPLAIN output columns.
var explainCols = []string{
	"id", "select_type", "table", "partitions",
	"type", "possible_keys", "key", "key_len",
	"ref", "rows", "filtered", "Extra",
}

func TestCheckExplain_FullTableScan(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows(explainCols).AddRow(
		int64(1), "SIMPLE", "users", nil,
		"ALL", nil, nil, nil,
		nil, int64(1500), "100.00", nil,
	)
	mock.ExpectQuery("EXPLAIN").WillReturnRows(rows)

	issues, err := sqlreview.CheckExplain(context.Background(), db, "SELECT * FROM users", sqlreview.DefaultRowThreshold)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Kind != "full_table_scan" {
		t.Errorf("expected full_table_scan, got %q", issues[0].Kind)
	}
	if issues[0].Table != "users" {
		t.Errorf("expected table 'users', got %q", issues[0].Table)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestCheckExplain_IndexedQueryNoIssue(t *testing.T) {
	// TE4: WHERE + index → EXPLAIN type=ref, low rows → no warnings
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows(explainCols).AddRow(
		int64(1), "SIMPLE", "users", nil,
		"ref", "idx_email", "idx_email", "202",
		"const", int64(1), "100.00", "Using index condition",
	)
	mock.ExpectQuery("EXPLAIN").WillReturnRows(rows)

	issues, err := sqlreview.CheckExplain(context.Background(), db, "SELECT * FROM users WHERE email = 'x@y.com'", sqlreview.DefaultRowThreshold)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected 0 issues for indexed query, got %d: %+v", len(issues), issues)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestCheckExplain_HighRowCount(t *testing.T) {
	// TE4: range scan but estimate exceeds threshold → high_row_count warning
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows(explainCols).AddRow(
		int64(1), "SIMPLE", "events", nil,
		"range", "idx_ts", "idx_ts", "5",
		nil, int64(50000), "100.00", "Using index condition",
	)
	mock.ExpectQuery("EXPLAIN").WillReturnRows(rows)

	issues, err := sqlreview.CheckExplain(context.Background(), db, "SELECT * FROM events WHERE ts > '2020-01-01'", int64(10000))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Kind != "high_row_count" {
		t.Errorf("expected high_row_count, got %q", issues[0].Kind)
	}
	if issues[0].Rows != 50000 {
		t.Errorf("expected 50000 rows, got %d", issues[0].Rows)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestCheckExplain_MultiTableJoin_OnlyFullScanFlagged(t *testing.T) {
	// TE4: JOIN where one table is ALL (full scan) and one is eq_ref (indexed)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows(explainCols).
		AddRow(int64(1), "SIMPLE", "orders", nil, "ALL", nil, nil, nil, nil, int64(5000), "100.00", nil).
		AddRow(int64(1), "SIMPLE", "users", nil, "eq_ref", "PRIMARY", "PRIMARY", "8", "orders.user_id", int64(1), "100.00", nil)
	mock.ExpectQuery("EXPLAIN").WillReturnRows(rows)

	issues, err := sqlreview.CheckExplain(context.Background(), db,
		"SELECT * FROM orders JOIN users ON orders.user_id = users.id", sqlreview.DefaultRowThreshold)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue (orders full scan only), got %d: %+v", len(issues), issues)
	}
	if issues[0].Table != "orders" || issues[0].Kind != "full_table_scan" {
		t.Errorf("unexpected issue: %+v", issues[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestCheckExplain_InsertSelectIgnoresInsertPseudoRowWithNullRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows(explainCols).
		AddRow(int64(1), "INSERT", "sys_menu", nil, "ALL", nil, nil, nil, nil, nil, nil, nil).
		AddRow(int64(1), "SIMPLE", "sys_menu", nil, "const", "PRIMARY", "PRIMARY", "8", "const", int64(1), "100.00", "Using index")
	mock.ExpectQuery("EXPLAIN").WillReturnRows(rows)

	issues, err := sqlreview.CheckExplain(context.Background(), db,
		"INSERT INTO sys_menu SELECT * FROM sys_menu WHERE id = 1", sqlreview.DefaultRowThreshold)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no issue for indexed INSERT ... SELECT, got %d: %+v", len(issues), issues)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestCheckExplainWithStats_ReturnsMaxRowsWhenNoIssues(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows(explainCols).
		AddRow(int64(1), "INSERT", "sys_menu", nil, "ALL", nil, nil, nil, nil, nil, nil, nil).
		AddRow(int64(1), "SIMPLE", "sys_menu", nil, "const", "PRIMARY", "PRIMARY", "8", "const", int64(1), "100.00", "Using index")
	mock.ExpectQuery("EXPLAIN").WillReturnRows(rows)

	result, err := sqlreview.CheckExplainWithStats(context.Background(), db,
		"INSERT INTO sys_menu SELECT * FROM sys_menu WHERE id = 1", sqlreview.DefaultRowThreshold)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Issues) != 0 {
		t.Fatalf("expected no issues, got %+v", result.Issues)
	}
	if result.MaxRows != 1 {
		t.Fatalf("MaxRows = %d, want 1", result.MaxRows)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestCheckExplainWithStats_ParsesMySQLByteRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows(explainCols).
		AddRow([]byte("1"), []byte("INSERT"), []byte("sys_menu"), nil, []byte("ALL"), nil, nil, nil, nil, nil, nil, nil).
		AddRow([]byte("1"), []byte("SIMPLE"), []byte("sys_menu"), nil, []byte("const"), []byte("PRIMARY"), []byte("PRIMARY"), []byte("8"), []byte("const"), []byte("1"), []byte("100.00"), []byte("Using index"))
	mock.ExpectQuery("EXPLAIN").WillReturnRows(rows)

	result, err := sqlreview.CheckExplainWithStats(context.Background(), db,
		"INSERT INTO sys_menu SELECT * FROM sys_menu WHERE id = 1", sqlreview.DefaultRowThreshold)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Issues) != 0 {
		t.Fatalf("expected no issues, got %+v", result.Issues)
	}
	if result.MaxRows != 1 {
		t.Fatalf("MaxRows = %d, want 1", result.MaxRows)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestCheckExplain_FullTableScanAboveThresholdFlagsBothRules(t *testing.T) {
	// A full table scan whose estimated rows also exceed the high_row_count
	// threshold must be flagged for both rules independently, so that disabling
	// full_table_scan alone doesn't silently let high_row_count go unchecked.
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows(explainCols).AddRow(
		int64(1), "DELETE", "member_trade_spot", nil,
		"ALL", "idx_user_id", nil, nil,
		nil, int64(6497228), "100.00", "Using where",
	)
	mock.ExpectQuery("EXPLAIN").WillReturnRows(rows)

	issues, err := sqlreview.CheckExplain(context.Background(), db,
		"DELETE FROM member_trade_spot WHERE user_id IN (1,2,3)", int64(30000))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues (full_table_scan + high_row_count), got %d: %+v", len(issues), issues)
	}
	kinds := map[string]bool{issues[0].Kind: true, issues[1].Kind: true}
	if !kinds["full_table_scan"] || !kinds["high_row_count"] {
		t.Errorf("expected both full_table_scan and high_row_count, got %+v", issues)
	}
}

func TestCheckExplain_BelowThresholdNoIssue(t *testing.T) {
	// Rows below threshold with non-ALL type → no issue
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows(explainCols).AddRow(
		int64(1), "SIMPLE", "orders", nil,
		"range", "idx_user_id", "idx_user_id", "8",
		nil, int64(9999), "100.00", nil,
	)
	mock.ExpectQuery("EXPLAIN").WillReturnRows(rows)

	issues, err := sqlreview.CheckExplain(context.Background(), db, "SELECT * FROM orders WHERE user_id > 0", sqlreview.DefaultRowThreshold)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected 0 issues (rows just under threshold), got %d", len(issues))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
