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
	Columns []string
	Rows    [][]any
}

// MaskResult applies masking rules to a query result in-place.
// rules is the list of applicable rules for this query's tables/columns.
// T4: any error returns immediately — we never return partially-masked data.
func (e *Engine) MaskResult(result *QueryResult, rules []Rule) error {
	if len(rules) == 0 {
		return nil
	}

	// Build a lookup: lowercase column name → rule
	colRules := make(map[string]Rule, len(rules))
	for _, r := range rules {
		colRules[strings.ToLower(r.Column)] = r
	}

	for rowIdx, row := range result.Rows {
		for colIdx, colName := range result.Columns {
			rule, ok := colRules[strings.ToLower(colName)]
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

// ErrMaskingFailed is returned when masking cannot be completed safely.
// Callers must return HTTP 422 — never fall back to returning unmasked data.
type ErrMaskingFailed struct {
	Cause error
}

func (e *ErrMaskingFailed) Error() string {
	return fmt.Sprintf("masking failed (Fail Closed): %v", e.Cause)
}

func (e *ErrMaskingFailed) Unwrap() error { return e.Cause }
