package handler

import "testing"

func TestSQLReviewRuleSupportsThreshold(t *testing.T) {
	if !sqlReviewRuleSupportsThreshold("high_row_count") {
		t.Fatal("expected high_row_count to support threshold")
	}

	for _, name := range []string{"ddl_no_comment", "dml_no_where", "full_table_scan", "require_utf8mb4"} {
		if sqlReviewRuleSupportsThreshold(name) {
			t.Fatalf("expected %s not to support threshold", name)
		}
	}
}
