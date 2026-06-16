package sqlreview

import (
	"testing"

	"github.com/dbre-maestro/maestro/internal/sqlparse"
)

func TestCheckLikelyMissingStatementDelimiter(t *testing.T) {
	t.Run("passes single create table statement", func(t *testing.T) {
		err := CheckLikelyMissingStatementDelimiter("create table a (id int) comment 'a' charset=utf8mb4")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("blocks concatenated top level statements", func(t *testing.T) {
		err := CheckLikelyMissingStatementDelimiter("create table a (id int) comment 'a' charset=utf8mb4 create table b (id int) comment 'b' charset=utf8mb4")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("ignores keywords inside strings and comments", func(t *testing.T) {
		sql := "create table a (note varchar(32) default 'create table b'); -- update users\ncomment 'keep'"
		err := CheckLikelyMissingStatementDelimiter(sql)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
}

func TestRunStaticChecksParsed(t *testing.T) {
	ruleMap := map[string]bool{
		"dml_no_where":    true,
		"ddl_no_comment":  true,
		"require_utf8mb4": true,
	}

	t.Run("mysql ast catches update without where", func(t *testing.T) {
		parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, "UPDATE users SET name = 'a'")
		if err != nil {
			t.Fatalf("ParseSQL() error = %v", err)
		}
		issues := RunStaticChecksParsed(parsed.Statements[0], ruleMap)
		if len(issues) == 0 {
			t.Fatal("expected issues, got none")
		}
	})

	t.Run("mysql ast catches create table without comment", func(t *testing.T) {
		parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, "CREATE TABLE t (id INT) CHARSET=utf8mb4")
		if err != nil {
			t.Fatalf("ParseSQL() error = %v", err)
		}
		issues := RunStaticChecksParsed(parsed.Statements[0], ruleMap)
		if len(issues) == 0 {
			t.Fatal("expected issues, got none")
		}
	})

	t.Run("mysql ast passes when comment and utf8mb4 exist", func(t *testing.T) {
		parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, "CREATE TABLE t (id INT) COMMENT='a' CHARSET=utf8mb4")
		if err != nil {
			t.Fatalf("ParseSQL() error = %v", err)
		}
		issues := RunStaticChecksParsed(parsed.Statements[0], ruleMap)
		if len(issues) != 0 {
			t.Fatalf("expected no issues, got %#v", issues)
		}
	})

	t.Run("mysql ast skips non-table ddl for utf8mb4 rule", func(t *testing.T) {
		parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, "CREATE DATABASE william_test")
		if err != nil {
			t.Fatalf("ParseSQL() error = %v", err)
		}
		issues := RunStaticChecksParsed(parsed.Statements[0], ruleMap)
		if len(issues) != 0 {
			t.Fatalf("expected no issues, got %#v", issues)
		}
	})

	t.Run("postgres ast catches update without where", func(t *testing.T) {
		parsed, err := sqlparse.ParseSQL(sqlparse.DialectPostgres, "UPDATE users SET name = 'a'")
		if err != nil {
			t.Fatalf("ParseSQL() error = %v", err)
		}
		issues := RunStaticChecksParsed(parsed.Statements[0], ruleMap)
		if len(issues) == 0 {
			t.Fatal("expected issues, got none")
		}
	})

	t.Run("postgres ast catches delete without where", func(t *testing.T) {
		parsed, err := sqlparse.ParseSQL(sqlparse.DialectPostgres, "DELETE FROM users")
		if err != nil {
			t.Fatalf("ParseSQL() error = %v", err)
		}
		issues := RunStaticChecksParsed(parsed.Statements[0], ruleMap)
		if len(issues) == 0 {
			t.Fatal("expected issues, got none")
		}
	})

	t.Run("postgres ast passes update with where", func(t *testing.T) {
		parsed, err := sqlparse.ParseSQL(sqlparse.DialectPostgres, "UPDATE users SET name = 'a' WHERE id = 1")
		if err != nil {
			t.Fatalf("ParseSQL() error = %v", err)
		}
		issues := RunStaticChecksParsed(parsed.Statements[0], ruleMap)
		if len(issues) != 0 {
			t.Fatalf("expected no issues, got %#v", issues)
		}
	})

	t.Run("postgres skips mysql-specific ddl rules", func(t *testing.T) {
		parsed, err := sqlparse.ParseSQL(sqlparse.DialectPostgres, "CREATE TABLE t (id integer)")
		if err != nil {
			t.Fatalf("ParseSQL() error = %v", err)
		}
		issues := RunStaticChecksParsed(parsed.Statements[0], ruleMap)
		if len(issues) != 0 {
			t.Fatalf("expected no issues, got %#v", issues)
		}
	})
}
