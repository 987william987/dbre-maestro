package sqlreview

import "testing"

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
