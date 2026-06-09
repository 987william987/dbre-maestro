package sqlreview_test

import (
	"testing"

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
		// Multiple allowed statements
		`SELECT 1; SELECT 2`,
		// Comments should be stripped
		`-- get all users
SELECT * FROM users`,
		`/* block comment */ SELECT id FROM users`,
	}
	for _, sql := range cases {
		if err := sqlreview.CheckReadOnly(sql); err != nil {
			t.Errorf("expected allowed, got error for %q: %v", sql, err)
		}
	}
}

func TestBlockedStatements(t *testing.T) {
	cases := []struct {
		sql     string
		wantKW  string
	}{
		{`INSERT INTO users VALUES (1)`, "INSERT"},
		{`UPDATE users SET name='x'`, "UPDATE"},
		{`DELETE FROM users`, "DELETE"},
		{`DROP TABLE users`, "DROP"},
		{`CREATE TABLE t (id INT)`, "CREATE"},
		{`ALTER TABLE users ADD COLUMN foo INT`, "ALTER"},
		{`TRUNCATE users`, "TRUNCATE"},
		// T1: SET @x = (SELECT ...) must be rejected
		{`SET @x = (SELECT 1)`, "SET"},
		{`SET NAMES utf8mb4`, "SET"},
		// WITH + non-SELECT main body must be rejected
		{`WITH cte AS (SELECT 1) INSERT INTO t SELECT * FROM cte`, "WITH+INSERT"},
		{`WITH cte AS (SELECT 1) UPDATE t SET x=1`, "WITH+UPDATE"},
		// Blocked statement mixed with allowed
		{`SELECT 1; DELETE FROM users`, "DELETE"},
	}
	for _, c := range cases {
		err := sqlreview.CheckReadOnly(c.sql)
		if err == nil {
			t.Errorf("expected error for %q, got nil", c.sql)
			continue
		}
		v, ok := err.(*sqlreview.ErrReadOnlyViolation)
		if !ok {
			t.Errorf("expected ErrReadOnlyViolation for %q, got %T", c.sql, err)
			continue
		}
		if v.Keyword != c.wantKW {
			t.Errorf("for %q: got keyword %q, want %q", c.sql, v.Keyword, c.wantKW)
		}
	}
}
