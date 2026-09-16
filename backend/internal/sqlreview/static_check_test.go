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

	t.Run("mysql ast skips alter table for utf8mb4 rule", func(t *testing.T) {
		parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, "ALTER TABLE t_notify_template MODIFY COLUMN jump_config JSON")
		if err != nil {
			t.Fatalf("ParseSQL() error = %v", err)
		}
		issues := RunStaticChecksParsed(parsed.Statements[0], ruleMap)
		if len(issues) != 0 {
			t.Fatalf("expected no issues, got %#v", issues)
		}
	})

	t.Run("mysql heuristic skips alter table for utf8mb4 rule", func(t *testing.T) {
		issues := RunStaticChecks("ALTER TABLE t_notify_template MODIFY COLUMN jump_config JSON", ruleMap)
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

func TestMySQLPolicyRulesMatchingCompanyStandards(t *testing.T) {
	tests := []struct {
		name string
		rule string
		sql  string
	}{
		{name: "requires InnoDB", rule: "require_innodb", sql: "CREATE TABLE t (id BIGINT PRIMARY KEY) ENGINE=MyISAM"},
		{name: "requires primary key", rule: "require_primary_key", sql: "CREATE TABLE t (name VARCHAR(20)) ENGINE=InnoDB"},
		{name: "prohibits foreign key", rule: "prohibit_foreign_key", sql: "CREATE TABLE t (parent_id BIGINT, FOREIGN KEY (parent_id) REFERENCES parent(id)) ENGINE=InnoDB"},
		{name: "prohibits stored procedure", rule: "prohibit_stored_procedure", sql: "CREATE PROCEDURE p() SELECT 1"},
		{name: "prohibits view", rule: "prohibit_view", sql: "CREATE VIEW active_users AS SELECT id FROM users WHERE active = 1"},
		{name: "prohibits reserved column name", rule: "prohibit_reserved_column_name", sql: "CREATE TABLE t (`select` INT) ENGINE=InnoDB"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, test.sql)
			if err != nil {
				t.Fatalf("ParseSQL() error = %v", err)
			}
			issues := RunStaticChecksParsed(parsed.Statements[0], map[string]bool{test.rule: true})
			if len(issues) != 1 {
				t.Fatalf("expected one policy issue, got %#v", issues)
			}
		})
	}
}

func TestUnsupportedMySQLObjectRulesUseLeadingKeywords(t *testing.T) {
	rules := map[string]bool{"prohibit_trigger": true, "prohibit_stored_function": true, "prohibit_event": true}
	for _, sql := range []string{
		"/* migration */ CREATE TRIGGER trg BEFORE INSERT ON t FOR EACH ROW SET @x = 1",
		"CREATE FUNCTION f() RETURNS INT RETURN 1",
		"CREATE EVENT cleanup ON SCHEDULE EVERY 1 DAY DO DELETE FROM t",
	} {
		if issues := RunStaticChecks(sql, rules); len(issues) != 1 {
			t.Fatalf("expected one prohibited-object issue for %q, got %#v", sql, issues)
		}
	}

	for _, sql := range []string{
		"SELECT 'CREATE TRIGGER trg'",
		"-- CREATE EVENT ignored\nSELECT 1",
		"CREATE TABLE function_log (id INT)",
	} {
		if issues := RunStaticChecks(sql, rules); len(issues) != 0 {
			t.Fatalf("expected no issue for %q, got %#v", sql, issues)
		}
	}
}

func TestUnsupportedMySQLObjectRuleIgnoresCommentsAndLiterals(t *testing.T) {
	for _, test := range []struct {
		sql, want string
	}{
		{sql: "CREATE TRIGGER trg BEFORE INSERT ON t FOR EACH ROW SET @x = 1", want: "prohibit_trigger"},
		{sql: "SELECT 'CREATE EVENT cleanup'", want: ""},
		{sql: "-- CREATE FUNCTION f\nSELECT 1", want: ""},
	} {
		if got := UnsupportedMySQLObjectRule(test.sql); got != test.want {
			t.Fatalf("UnsupportedMySQLObjectRule(%q) = %q, want %q", test.sql, got, test.want)
		}
	}
}

func TestReservedColumnRuleAllowsOrdinaryColumnNames(t *testing.T) {
	parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, "CREATE TABLE t (id BIGINT, display_name VARCHAR(64)) ENGINE=InnoDB")
	if err != nil {
		t.Fatalf("ParseSQL() error = %v", err)
	}
	issues := RunStaticChecksParsed(parsed.Statements[0], map[string]bool{"prohibit_reserved_column_name": true})
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %#v", issues)
	}
}
