package sqlreview

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const DefaultRowThreshold int64 = 10000

// ExplainIssue is a Warning-level finding from EXPLAIN analysis.
type ExplainIssue struct {
	Table string
	Kind  string // "full_table_scan" | "high_row_count"
	Rows  int64
	Msg   string
}

// CheckExplain runs EXPLAIN on sqlStr against the provided query_pool connection
// and returns findings. Issues are Warnings — callers decide whether to block.
func CheckExplain(ctx context.Context, db *sql.DB, sqlStr string, rowThreshold int64) ([]ExplainIssue, error) {
	rows, err := db.QueryContext(ctx, "EXPLAIN "+sqlStr)
	if err != nil {
		return nil, fmt.Errorf("EXPLAIN failed: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	idx := buildColIndex(cols)
	var issues []ExplainIssue

	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}

		tableName := explainStrVal(vals, idx, "table")
		scanType := explainStrVal(vals, idx, "type")
		rowsEst := explainIntVal(vals, idx, "rows")

		if scanType == "ALL" {
			issues = append(issues, ExplainIssue{
				Table: tableName,
				Kind:  "full_table_scan",
				Rows:  rowsEst,
				Msg:   fmt.Sprintf("table %q uses full table scan (type=ALL, ~%d rows)", tableName, rowsEst),
			})
		} else if rowsEst > rowThreshold {
			issues = append(issues, ExplainIssue{
				Table: tableName,
				Kind:  "high_row_count",
				Rows:  rowsEst,
				Msg:   fmt.Sprintf("table %q estimated rows %d exceeds threshold %d", tableName, rowsEst, rowThreshold),
			})
		}
	}
	return issues, rows.Err()
}

func buildColIndex(cols []string) map[string]int {
	m := make(map[string]int, len(cols))
	for i, c := range cols {
		m[strings.ToLower(c)] = i
	}
	return m
}

func explainStrVal(vals []interface{}, idx map[string]int, col string) string {
	i, ok := idx[col]
	if !ok || vals[i] == nil {
		return ""
	}
	switch v := vals[i].(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func explainIntVal(vals []interface{}, idx map[string]int, col string) int64 {
	i, ok := idx[col]
	if !ok || vals[i] == nil {
		return 0
	}
	switch v := vals[i].(type) {
	case int64:
		return v
	case int32:
		return int64(v)
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		var n int64
		fmt.Sscanf(v, "%d", &n)
		return n
	case []byte:
		var n int64
		fmt.Sscanf(string(v), "%d", &n)
		return n
	}
	return 0
}
