package handler

import (
	"strings"
	"testing"

	"github.com/dbre-maestro/maestro/internal/sqlparse"
	"github.com/dbre-maestro/maestro/internal/sqlreview"
)

func TestReadOnlySQLErrorMessageDistinguishesUnsupportedParserSyntax(t *testing.T) {
	err := sqlreview.CheckReadOnly(
		sqlparse.DialectMySQL,
		`SELECT * FROM JSON_TABLE('["BTCUSDC"]', '$[*]' COLUMNS (name VARCHAR(64) PATH '$')) AS jt`,
	)
	if err == nil {
		t.Fatal("CheckReadOnly() error = nil, want unsupported parser syntax error")
	}

	got := readOnlySQLErrorMessage(err)
	want := "SQL statement 1 could not be parsed. It may use syntax that is not supported by the platform parser."
	if got != want {
		t.Fatalf("readOnlySQLErrorMessage() = %q, want %q", got, want)
	}
	if strings.Contains(got, "BTCUSDC") || strings.Contains(got, "only read-only SQL") {
		t.Fatalf("parser compatibility error should not expose SQL or report a read-only violation: %q", got)
	}
}

func TestReadOnlySQLErrorMessageKeepsReadOnlyPolicyViolation(t *testing.T) {
	err := sqlreview.CheckReadOnly(sqlparse.DialectMySQL, `DELETE FROM users WHERE id = 1`)
	if err == nil {
		t.Fatal("CheckReadOnly() error = nil, want read-only policy violation")
	}

	got := readOnlySQLErrorMessage(err)
	if !strings.HasPrefix(got, "only read-only SQL is allowed:") {
		t.Fatalf("readOnlySQLErrorMessage() = %q, want read-only policy message", got)
	}
}
