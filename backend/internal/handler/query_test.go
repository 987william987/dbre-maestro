package handler

import (
	"context"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/model"
)

func TestInjectLimitStripsTrailingSemicolon(t *testing.T) {
	got := injectLimit("SELECT * FROM t_user;   ", 200, "mysql")
	want := "SELECT * FROM t_user LIMIT 200"
	if got != want {
		t.Fatalf("injectLimit() = %q, want %q", got, want)
	}
}

func TestInjectLimitPreservesExistingLimit(t *testing.T) {
	got := injectLimit("SELECT * FROM t_user LIMIT 10;", 200, "mysql")
	want := "SELECT * FROM t_user LIMIT 10"
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

func TestExecuteSQLQueryUsesDatabaseOnPinnedConnection(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("USE `analytics`").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT \\* FROM t_user LIMIT 200").
		WillReturnRows(sqlmock.NewRows([]string{"t_user.id"}).AddRow(1))

	result, err := executeSQLQuery(
		context.Background(),
		&model.DBConnection{DBType: "mysql"},
		db,
		"SELECT * FROM t_user LIMIT 200",
		queryExecutionContext{DatabaseName: "analytics"},
	)
	if err != nil {
		t.Fatalf("executeSQLQuery() error = %v", err)
	}
	if len(result.Rows) != 1 || len(result.Columns) != 1 {
		t.Fatalf("unexpected result = %#v", result)
	}
	if result.Origins[0].Database != "analytics" {
		t.Fatalf("origin database = %q, want %q", result.Origins[0].Database, "analytics")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
