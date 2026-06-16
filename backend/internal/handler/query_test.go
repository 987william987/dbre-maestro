package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/masking"
	"github.com/dbre-maestro/maestro/internal/model"
)

func TestWriteQueryExecutionErrorTimeout(t *testing.T) {
	recorder := httptest.NewRecorder()

	writeQueryExecutionError(recorder, context.DeadlineExceeded, "query", defaultQueryTimeout)

	if recorder.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusGatewayTimeout)
	}

	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error"] != "query timed out after 30s" {
		t.Fatalf("error = %q, want %q", body["error"], "query timed out after 30s")
	}
}

func TestWriteQueryExecutionErrorCanceled(t *testing.T) {
	recorder := httptest.NewRecorder()

	writeQueryExecutionError(recorder, context.Canceled, "query", defaultQueryTimeout)

	if recorder.Code != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusRequestTimeout)
	}

	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error"] != "query was cancelled" {
		t.Fatalf("error = %q, want %q", body["error"], "query was cancelled")
	}
}

func TestInjectLimitStripsTrailingSemicolon(t *testing.T) {
	got := injectLimit("SELECT * FROM t_user;   ", 200, "mysql")
	want := "SELECT * FROM `t_user` LIMIT 200"
	if got != want {
		t.Fatalf("injectLimit() = %q, want %q", got, want)
	}
}

func TestInjectLimitPreservesExistingLimit(t *testing.T) {
	got := injectLimit("SELECT * FROM t_user LIMIT 10;", 200, "mysql")
	want := "SELECT * FROM `t_user` LIMIT 10"
	if got != want {
		t.Fatalf("injectLimit() = %q, want %q", got, want)
	}
}

func TestInjectLimitSkipsRedis(t *testing.T) {
	got := injectLimit("GET user:1", 200, "redis")
	want := "GET user:1"
	if got != want {
		t.Fatalf("injectLimit() = %q, want %q", got, want)
	}
}

func TestInjectLimitSkipsShowStatements(t *testing.T) {
	got := injectLimit("SHOW GRANTS FOR dev;", 200, "mysql")
	want := "SHOW GRANTS FOR dev"
	if got != want {
		t.Fatalf("injectLimit() = %q, want %q", got, want)
	}
}

func TestInjectLimitLeavesMultiStatementSQLUntouched(t *testing.T) {
	got := injectLimit("SELECT 1;\nSHOW GRANTS FOR dev;", 200, "mysql")
	want := "SELECT 1;\nSHOW GRANTS FOR dev;"
	if got != want {
		t.Fatalf("injectLimit() = %q, want %q", got, want)
	}
}

func TestInjectLimitSupportsCTESelect(t *testing.T) {
	got := injectLimit("WITH cte AS (SELECT id FROM users) SELECT * FROM cte;", 200, "mysql")
	want := "WITH `cte` AS (SELECT `id` FROM `users`) SELECT * FROM `cte` LIMIT 200"
	if got != want {
		t.Fatalf("injectLimit() = %q, want %q", got, want)
	}
}

func TestInjectLimitPreservesTopLevelLimitForCTESelect(t *testing.T) {
	got := injectLimit("WITH cte AS (SELECT id FROM users LIMIT 5) SELECT * FROM cte LIMIT 10;", 200, "mysql")
	want := "WITH `cte` AS (SELECT `id` FROM `users` LIMIT 5) SELECT * FROM `cte` LIMIT 10"
	if got != want {
		t.Fatalf("injectLimit() = %q, want %q", got, want)
	}
}

func TestInjectLimitLeavesCTEMultiStatementSQLUntouched(t *testing.T) {
	got := injectLimit("WITH cte AS (SELECT 1) SELECT * FROM cte;\nSHOW GRANTS FOR dev;", 200, "mysql")
	want := "WITH cte AS (SELECT 1) SELECT * FROM cte;\nSHOW GRANTS FOR dev;"
	if got != want {
		t.Fatalf("injectLimit() = %q, want %q", got, want)
	}
}

func TestExecuteSQLQueryUsesDatabaseOnPinnedConnection(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("USE `analytics`").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SET SESSION max_execution_time = 25000").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT \\* FROM t_user LIMIT 200").
		WillReturnRows(sqlmock.NewRows([]string{"t_user.id"}).AddRow(1))

	result, err := executeSQLQuery(
		context.Background(),
		&model.DBConnection{DBType: "mysql"},
		db,
		"SELECT * FROM t_user LIMIT 200",
		queryExecutionContext{DatabaseName: "analytics"},
		defaultSQLEditorTimeoutSettings(),
	)
	if err != nil {
		t.Fatalf("executeSQLQuery() error = %v", err)
	}
	if len(result.Rows) != 1 || len(result.Columns) != 1 {
		t.Fatalf("unexpected result = %#v", result)
	}
	if result.Columns[0] != "id" {
		t.Fatalf("display column = %q, want %q", result.Columns[0], "id")
	}
	if len(result.RawColumns) != 1 || result.RawColumns[0] != "t_user.id" {
		t.Fatalf("raw columns = %#v, want [t_user.id]", result.RawColumns)
	}
	if result.Origins[0].Database != "analytics" {
		t.Fatalf("origin database = %q, want %q", result.Origins[0].Database, "analytics")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestExecuteSQLQueryRunsMultiStatementsSequentially(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("USE `analytics`").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SET SESSION max_execution_time = 25000").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT 1 LIMIT 200").
		WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
	mock.ExpectQuery("SELECT \\* FROM t_user LIMIT 200").
		WillReturnRows(sqlmock.NewRows([]string{"t_user.id"}).AddRow(2))

	result, err := executeSQLQuery(
		context.Background(),
		&model.DBConnection{DBType: "mysql"},
		db,
		"SELECT 1 LIMIT 200; SELECT * FROM t_user LIMIT 200",
		queryExecutionContext{DatabaseName: "analytics"},
		defaultSQLEditorTimeoutSettings(),
	)
	if err != nil {
		t.Fatalf("executeSQLQuery() error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != int64(2) {
		t.Fatalf("unexpected result rows = %#v", result.Rows)
	}
	if len(result.Columns) != 1 || result.Columns[0] != "id" {
		t.Fatalf("unexpected result columns = %#v", result.Columns)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestBuildDisplayColumnsUsesOriginColumnName(t *testing.T) {
	rawColumns := []string{"t_deposit.id", "t_deposit.user_id", "t_deposit.account_id"}
	origins := []struct {
		database string
		table    string
		column   string
	}{
		{database: "analytics", table: "t_deposit", column: "id"},
		{database: "analytics", table: "t_deposit", column: "user_id"},
		{database: "analytics", table: "t_deposit", column: "account_id"},
	}

	display := buildDisplayColumns(rawColumns, []masking.ColumnOrigin{
		{Database: origins[0].database, Table: origins[0].table, Column: origins[0].column},
		{Database: origins[1].database, Table: origins[1].table, Column: origins[1].column},
		{Database: origins[2].database, Table: origins[2].table, Column: origins[2].column},
	})

	want := []string{"id", "user_id", "account_id"}
	for i := range want {
		if display[i] != want[i] {
			t.Fatalf("display[%d] = %q, want %q", i, display[i], want[i])
		}
	}
}
