package masking

import (
	"fmt"
	"strings"
)

// Engine applies masking rules to query result rows.
// T4: Fail Closed — if parsing or rule application fails, the engine returns
// a 422 error rather than leaking unmasked data.
type Engine struct {
	pepper []byte
	cache  *RuleCache
}

func NewEngine(encryptionKey []byte, cache *RuleCache) (*Engine, error) {
	pepper, err := DeriveHashPepper(encryptionKey)
	if err != nil {
		return nil, err
	}
	return &Engine{pepper: pepper, cache: cache}, nil
}

// QueryResult holds the columns and rows returned by a SQL Editor query.
type QueryResult struct {
	Columns    []string
	RawColumns []string
	Origins    []ColumnOrigin
	Rows       [][]any
}

type ColumnOrigin struct {
	Database string
	Schema   string
	Table    string
	Column   string
}

// MaskResult applies masking rules to a query result in-place.
// rules is the list of applicable rules for this query's tables/columns.
// T4: any error returns immediately — we never return partially-masked data.
func (e *Engine) MaskResult(result *QueryResult, rules []Rule) error {
	if len(rules) == 0 {
		return nil
	}

	columnLabels := result.columnLabelsForMatching()
	for rowIdx, row := range result.Rows {
		for colIdx, colName := range columnLabels {
			rule, ok := e.matchRule(result, colIdx, colName, rules)
			if !ok {
				continue
			}
			if row[colIdx] == nil {
				continue
			}
			strVal := fmt.Sprintf("%v", row[colIdx])
			masked, err := rule.Apply(strVal, e.pepper)
			if err != nil {
				// T4: Fail Closed — return 422-worthy error, never silently pass
				return fmt.Errorf("mask column %q row %d: %w", colName, rowIdx, err)
			}
			result.Rows[rowIdx][colIdx] = masked
		}
	}
	return nil
}

func (r *QueryResult) columnLabelsForMatching() []string {
	if len(r.RawColumns) == len(r.Columns) {
		return r.RawColumns
	}
	return r.Columns
}

func (e *Engine) matchRule(result *QueryResult, colIdx int, colName string, rules []Rule) (Rule, bool) {
	for _, rule := range rules {
		if !equalFold(rule.Column, colName) {
			origin := ColumnOrigin{}
			if colIdx < len(result.Origins) {
				origin = result.Origins[colIdx]
			}
			if matchesOrigin(rule, origin) || matchesColumnLabel(rule, colName) {
				return rule, true
			}
			continue
		}

		origin := ColumnOrigin{}
		if colIdx < len(result.Origins) {
			origin = result.Origins[colIdx]
		}
		if matchesOrigin(rule, origin) || matchesColumnLabel(rule, colName) {
			return rule, true
		}
	}
	return Rule{}, false
}

func matchesOrigin(rule Rule, origin ColumnOrigin) bool {
	if origin.Column == "" {
		return false
	}
	if !equalFold(rule.Column, origin.Column) {
		return false
	}
	if rule.Database != "" && !equalFold(rule.Database, origin.Database) {
		return false
	}
	if rule.Schema != "" && !equalFold(rule.Schema, origin.Schema) {
		return false
	}
	if rule.Table != "" && !equalFold(rule.Table, origin.Table) {
		return false
	}
	return true
}

func matchesColumnLabel(rule Rule, columnLabel string) bool {
	if rule.Database != "" || rule.Schema != "" {
		return false
	}

	labelParts := strings.Split(columnLabel, ".")
	switch len(labelParts) {
	case 1:
		return rule.Table == "" && equalFold(rule.Column, labelParts[0])
	default:
		tableName := labelParts[len(labelParts)-2]
		columnName := labelParts[len(labelParts)-1]
		return equalFold(rule.Table, tableName) && equalFold(rule.Column, columnName)
	}
}

func equalFold(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

// ErrMaskingFailed is returned when masking cannot be completed safely.
// Callers must return HTTP 422 — never fall back to returning unmasked data.
type ErrMaskingFailed struct {
	Cause error
}

func (e *ErrMaskingFailed) Error() string {
	return fmt.Sprintf("masking failed (Fail Closed): %v", e.Cause)
}

func (e *ErrMaskingFailed) Unwrap() error { return e.Cause }
