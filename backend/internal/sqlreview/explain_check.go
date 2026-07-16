package sqlreview

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strconv"
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

type ExplainResult struct {
	Issues  []ExplainIssue
	MaxRows int64
}

// CheckExplain runs EXPLAIN on sqlStr against the provided query_pool connection
// and returns findings. Issues are Warnings — callers decide whether to block.
func CheckExplain(ctx context.Context, db *sql.DB, sqlStr string, rowThreshold int64) ([]ExplainIssue, error) {
	result, err := CheckExplainWithStats(ctx, db, sqlStr, rowThreshold)
	if err != nil {
		return nil, err
	}
	return result.Issues, nil
}

func CheckExplainWithStats(ctx context.Context, db *sql.DB, sqlStr string, rowThreshold int64) (ExplainResult, error) {
	rows, err := db.QueryContext(ctx, "EXPLAIN "+sqlStr)
	if err != nil {
		return ExplainResult{}, fmt.Errorf("EXPLAIN failed: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return ExplainResult{}, err
	}

	idx := buildColIndex(cols)
	result := ExplainResult{}

	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return ExplainResult{}, err
		}

		tableName := explainStrVal(vals, idx, "table")
		scanType := explainStrVal(vals, idx, "type")
		rowsEst, rowsOK := explainIntVal(vals, idx, "rows")
		if !rowsOK {
			continue
		}
		if rowsEst > result.MaxRows {
			result.MaxRows = rowsEst
		}

		if scanType == "ALL" {
			result.Issues = append(result.Issues, ExplainIssue{
				Table: tableName,
				Kind:  "full_table_scan",
				Rows:  rowsEst,
				Msg:   fmt.Sprintf("table %q uses full table scan (type=ALL, ~%d rows)", tableName, rowsEst),
			})
		} else if rowsEst > rowThreshold {
			result.Issues = append(result.Issues, ExplainIssue{
				Table: tableName,
				Kind:  "high_row_count",
				Rows:  rowsEst,
				Msg:   fmt.Sprintf("table %q estimated rows %d exceeds threshold %d", tableName, rowsEst, rowThreshold),
			})
		}
	}
	return result, rows.Err()
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

func explainIntVal(vals []interface{}, idx map[string]int, col string) (int64, bool) {
	i, ok := idx[col]
	if !ok || vals[i] == nil {
		return 0, false
	}
	switch v := vals[i].(type) {
	case int64:
		return v, true
	case int16:
		return int64(v), true
	case int8:
		return int64(v), true
	case int32:
		return int64(v), true
	case int:
		return int64(v), true
	case uint64:
		if v > math.MaxInt64 {
			return 0, false
		}
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint8:
		return int64(v), true
	case uint:
		if uint64(v) > math.MaxInt64 {
			return 0, false
		}
		return int64(v), true
	case float64:
		return int64(v), true
	case sql.NullInt64:
		return v.Int64, v.Valid
	case string:
		return parseExplainIntString(v)
	case []byte:
		return parseExplainIntString(string(v))
	}
	return 0, false
}

func parseExplainIntString(value string) (int64, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "null") {
		return 0, false
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err == nil {
		return n, true
	}
	u, err := strconv.ParseUint(value, 10, 64)
	if err != nil || u > math.MaxInt64 {
		return 0, false
	}
	return int64(u), true
}
