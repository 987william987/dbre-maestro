package handler

import (
	"testing"

	"github.com/dbre-maestro/maestro/internal/sqlparse"
)

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

func TestParserUnsupportedRulesRequireErrorSeverity(t *testing.T) {
	for _, name := range []string{"prohibit_trigger", "prohibit_stored_function", "prohibit_event"} {
		if !sqlReviewRuleRequiresError(name) {
			t.Fatalf("expected %s to require error severity", name)
		}
	}
	if sqlReviewRuleRequiresError("prohibit_stored_procedure") {
		t.Fatal("stored procedure is parser-supported and may use warning severity")
	}
}

func TestBuildStaticValidationItemsHonorsConfiguredSeverity(t *testing.T) {
	parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, "CREATE TABLE child (parent_id BIGINT, FOREIGN KEY (parent_id) REFERENCES parent(id))")
	if err != nil {
		t.Fatalf("ParseSQL() error = %v", err)
	}

	items := buildStaticValidationItems(
		parsed.Statements,
		map[string]bool{"prohibit_foreign_key": true},
		map[string]string{"prohibit_foreign_key": "warning"},
	)
	if len(items) != 1 || items[0].Status != "warn" {
		t.Fatalf("warning policy must remain non-blocking, got %#v", items)
	}
}
