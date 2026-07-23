package sqlparse

import "testing"

func TestParseMySQLStatements(t *testing.T) {
	parsed, err := ParseSQL(DialectMySQL, "CREATE TABLE a (id INT); ALTER TABLE a ADD COLUMN name VARCHAR(10);")
	if err != nil {
		t.Fatalf("ParseSQL() error = %v", err)
	}
	if len(parsed.Statements) != 2 {
		t.Fatalf("len(statements) = %d, want 2", len(parsed.Statements))
	}
	if parsed.Statements[0].Kind != StatementKindCreate {
		t.Fatalf("statement 1 kind = %s, want %s", parsed.Statements[0].Kind, StatementKindCreate)
	}
	if parsed.Statements[1].Kind != StatementKindAlter {
		t.Fatalf("statement 2 kind = %s, want %s", parsed.Statements[1].Kind, StatementKindAlter)
	}
}

func TestParseMySQLSetUserVariableStatement(t *testing.T) {
	parsed, err := ParseSQL(DialectMySQL, "SET @banner_menu_id := (SELECT id FROM sys_menu LIMIT 1); INSERT INTO sys_menu (id, pid) VALUES (1, @banner_menu_id);")
	if err != nil {
		t.Fatalf("ParseSQL() error = %v", err)
	}
	if len(parsed.Statements) != 2 {
		t.Fatalf("len(statements) = %d, want 2", len(parsed.Statements))
	}
	if parsed.Statements[0].Kind != StatementKindSet {
		t.Fatalf("statement 1 kind = %s, want %s", parsed.Statements[0].Kind, StatementKindSet)
	}
	if parsed.Statements[1].Kind != StatementKindInsert {
		t.Fatalf("statement 2 kind = %s, want %s", parsed.Statements[1].Kind, StatementKindInsert)
	}
}

func TestParseMySQLRejectsMissingDelimiter(t *testing.T) {
	_, err := ParseSQL(DialectMySQL, "CREATE TABLE a (id INT) CREATE TABLE b (id INT);")
	if err == nil {
		t.Fatal("expected syntax error, got nil")
	}
	if _, ok := err.(*SyntaxError); !ok {
		t.Fatalf("expected SyntaxError, got %T", err)
	}
}

func TestParseMySQLIgnoresKeywordsInsideCommentsAndStrings(t *testing.T) {
	parsed, err := ParseSQL(DialectMySQL, "SELECT 'CREATE TABLE x'; /* UPDATE users */")
	if err != nil {
		t.Fatalf("ParseSQL() error = %v", err)
	}
	if len(parsed.Statements) != 1 {
		t.Fatalf("len(statements) = %d, want 1", len(parsed.Statements))
	}
	if parsed.Statements[0].Kind != StatementKindSelect {
		t.Fatalf("statement kind = %s, want %s", parsed.Statements[0].Kind, StatementKindSelect)
	}
}

func TestParsePostgresStatements(t *testing.T) {
	parsed, err := ParseSQL(DialectPostgres, "SELECT 1; UPDATE users SET name = 'a' WHERE id = 1;")
	if err != nil {
		t.Fatalf("ParseSQL() error = %v", err)
	}
	if len(parsed.Statements) != 2 {
		t.Fatalf("len(statements) = %d, want 2", len(parsed.Statements))
	}
	if parsed.Statements[0].Kind != StatementKindSelect {
		t.Fatalf("statement 1 kind = %s, want %s", parsed.Statements[0].Kind, StatementKindSelect)
	}
	if parsed.Statements[1].Kind != StatementKindUpdate {
		t.Fatalf("statement 2 kind = %s, want %s", parsed.Statements[1].Kind, StatementKindUpdate)
	}
}

func TestParsePostgresRejectsSyntaxError(t *testing.T) {
	_, err := ParseSQL(DialectPostgres, "SELECT FROM")
	if err == nil {
		t.Fatal("expected syntax error, got nil")
	}
	if _, ok := err.(*SyntaxError); !ok {
		t.Fatalf("expected SyntaxError, got %T", err)
	}
}

func TestRewriteSelectLimitMySQLSelect(t *testing.T) {
	rewritten, changed, err := RewriteSelectLimit(DialectMySQL, "SELECT * FROM t_user;", 200)
	if err != nil {
		t.Fatalf("RewriteSelectLimit() error = %v", err)
	}
	if !changed {
		t.Fatal("expected mysql select limit rewrite to report changed")
	}
	if rewritten != "SELECT * FROM `t_user` LIMIT 200" {
		t.Fatalf("rewritten = %q", rewritten)
	}
}

func TestRewriteSelectLimitMySQLCTESelect(t *testing.T) {
	rewritten, changed, err := RewriteSelectLimit(DialectMySQL, "WITH cte AS (SELECT id FROM users) SELECT * FROM cte;", 200)
	if err != nil {
		t.Fatalf("RewriteSelectLimit() error = %v", err)
	}
	if !changed {
		t.Fatal("expected mysql cte select limit rewrite to report changed")
	}
	if rewritten != "WITH `cte` AS (SELECT `id` FROM `users`) SELECT * FROM `cte` LIMIT 200" {
		t.Fatalf("rewritten = %q", rewritten)
	}
}

func TestRewriteSelectLimitMySQLPreservesExistingLimit(t *testing.T) {
	rewritten, changed, err := RewriteSelectLimit(DialectMySQL, "SELECT * FROM t_user LIMIT 10;", 200)
	if err != nil {
		t.Fatalf("RewriteSelectLimit() error = %v", err)
	}
	if changed {
		t.Fatal("expected mysql select with existing limit to remain unchanged")
	}
	if rewritten != "SELECT * FROM `t_user` LIMIT 10" {
		t.Fatalf("rewritten = %q", rewritten)
	}
}

func TestRewriteSelectLimitPostgresSelect(t *testing.T) {
	rewritten, changed, err := RewriteSelectLimit(DialectPostgres, "SELECT * FROM t_user;", 200)
	if err != nil {
		t.Fatalf("RewriteSelectLimit() error = %v", err)
	}
	if !changed {
		t.Fatal("expected postgres select limit rewrite to report changed")
	}
	if rewritten != "SELECT * FROM t_user LIMIT 200" {
		t.Fatalf("rewritten = %q", rewritten)
	}
}

func TestRewriteSelectLimitPostgresCTESelect(t *testing.T) {
	rewritten, changed, err := RewriteSelectLimit(DialectPostgres, "WITH cte AS (SELECT id FROM users) SELECT * FROM cte;", 200)
	if err != nil {
		t.Fatalf("RewriteSelectLimit() error = %v", err)
	}
	if !changed {
		t.Fatal("expected postgres cte select limit rewrite to report changed")
	}
	if rewritten != "WITH cte AS (SELECT id FROM users) SELECT * FROM cte LIMIT 200" {
		t.Fatalf("rewritten = %q", rewritten)
	}
}

func TestRewriteSelectLimitPostgresPreservesExistingLimit(t *testing.T) {
	rewritten, changed, err := RewriteSelectLimit(DialectPostgres, "SELECT * FROM t_user LIMIT 10;", 200)
	if err != nil {
		t.Fatalf("RewriteSelectLimit() error = %v", err)
	}
	if changed {
		t.Fatal("expected postgres select with existing limit to remain unchanged")
	}
	if rewritten != "SELECT * FROM t_user LIMIT 10" {
		t.Fatalf("rewritten = %q", rewritten)
	}
}
