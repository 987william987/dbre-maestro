package sqlreview_test

import (
	"testing"

	"github.com/dbre-maestro/maestro/internal/sqlparse"
	"github.com/dbre-maestro/maestro/internal/sqlpolicy"
	"github.com/dbre-maestro/maestro/internal/sqlreview"
)

func TestAllowedStatements(t *testing.T) {
	cases := []string{
		`SELECT 1`,
		`SELECT * FROM users WHERE id = 1`,
		`SHOW TABLES`,
		`SHOW CREATE TABLE users`,
		`EXPLAIN SELECT * FROM users`,
		`DESC users`,
		`DESCRIBE users`,
		`WITH cte AS (SELECT id FROM users) SELECT * FROM cte`,
		`WITH RECURSIVE r AS (SELECT 1 UNION ALL SELECT n+1 FROM r WHERE n < 5) SELECT * FROM r`,
		`-- get all users
SELECT * FROM users`,
		`/* block comment */ SELECT id FROM users`,
	}
	for _, sql := range cases {
		if err := sqlreview.CheckReadOnly(sqlparse.DialectMySQL, sql); err != nil {
			t.Errorf("expected allowed, got error for %q: %v", sql, err)
		}
	}
}

func TestBlockedStatements(t *testing.T) {
	cases := []struct {
		sql        string
		wantErrTyp string
	}{
		{`INSERT INTO users VALUES (1)`, "violation"},
		{`UPDATE users SET name='x'`, "violation"},
		{`DELETE FROM users`, "violation"},
		{`DROP TABLE users`, "violation"},
		{`CREATE TABLE t (id INT)`, "violation"},
		{`ALTER TABLE users ADD COLUMN foo INT`, "violation"},
		{`TRUNCATE users`, "violation"},
		{`SET @x = (SELECT 1)`, "violation"},
		{`SET NAMES utf8mb4`, "violation"},
		{`WITH cte AS (SELECT 1) INSERT INTO t SELECT * FROM cte`, "syntax"},
		{`WITH cte AS (SELECT 1) UPDATE t SET x=1`, "violation"},
		{`SELECT 1; SELECT 2`, "count"},
		{`SELECT 1; DELETE FROM users`, "count"},
	}
	for _, c := range cases {
		err := sqlreview.CheckReadOnly(sqlparse.DialectMySQL, c.sql)
		if err == nil {
			t.Errorf("expected error for %q, got nil", c.sql)
			continue
		}
		switch c.wantErrTyp {
		case "count":
			if _, ok := err.(*sqlpolicy.ErrReadOnlyStatementCount); !ok {
				t.Errorf("expected ErrReadOnlyStatementCount for %q, got %T", c.sql, err)
			}
		case "syntax":
			if _, ok := err.(*sqlparse.SyntaxError); !ok {
				t.Errorf("expected SyntaxError for %q, got %T", c.sql, err)
			}
		default:
			if _, ok := err.(*sqlreview.ErrReadOnlyViolation); !ok {
				t.Errorf("expected ErrReadOnlyViolation for %q, got %T", c.sql, err)
			}
		}
	}
}
