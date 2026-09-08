package handler

import "testing"

func TestAdminStatementReturnsRows(t *testing.T) {
	tests := []struct {
		name      string
		statement string
		want      bool
	}{
		{name: "select", statement: "SELECT * FROM users", want: true},
		{name: "show", statement: "SHOW GRANTS FOR app", want: true},
		{name: "cte", statement: "WITH active AS (SELECT 1) SELECT * FROM active", want: true},
		{name: "update", statement: "UPDATE users SET active = 1", want: false},
		{name: "update returning on new line", statement: "UPDATE users SET active = 1\nRETURNING id", want: true},
		{name: "ddl", statement: "ALTER TABLE users ADD COLUMN note text", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := adminStatementReturnsRows(tt.statement); got != tt.want {
				t.Fatalf("adminStatementReturnsRows(%q) = %v, want %v", tt.statement, got, tt.want)
			}
		})
	}
}

func TestAdminModeRequiresExactlyOneStatement(t *testing.T) {
	if got := len(splitSQLStatementsForLimit("SELECT 1; SELECT 2")); got != 2 {
		t.Fatalf("statement count = %d, want 2", got)
	}
	if got := len(splitSQLStatementsForLimit("SELECT ';' AS separator;")); got != 1 {
		t.Fatalf("quoted semicolon statement count = %d, want 1", got)
	}
}

func TestAdminAuditStatementRedactsCredentials(t *testing.T) {
	for _, statement := range []string{
		"CREATE USER app IDENTIFIED BY 'secret'",
		"ALTER ROLE app PASSWORD 'secret'",
	} {
		if got := adminAuditStatement(statement); got != "[REDACTED SENSITIVE ADMIN STATEMENT]" {
			t.Fatalf("adminAuditStatement(%q) = %q", statement, got)
		}
	}

	if got := adminAuditStatement("UPDATE users SET active = 1"); got != "UPDATE users SET active = 1" {
		t.Fatalf("non-sensitive statement = %q", got)
	}
}
