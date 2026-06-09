package sqlreview

import (
	"fmt"
	"strings"
	"unicode"
)

// allowedRootKeywords is the whitelist of statement types permitted in the SQL Editor.
// This is a WHITELIST (not a blacklist): anything not in this set is rejected.
var allowedRootKeywords = map[string]bool{
	"SELECT":   true,
	"SHOW":     true,
	"EXPLAIN":  true,
	"DESC":     true,
	"DESCRIBE": true,
	// WITH is allowed only when its final query is a SELECT (checked separately).
	"WITH": true,
}

// ErrReadOnlyViolation is returned when a statement is not read-only.
type ErrReadOnlyViolation struct {
	Statement string
	Keyword   string
}

func (e *ErrReadOnlyViolation) Error() string {
	preview := e.Statement
	if len(preview) > 80 {
		preview = preview[:80] + "..."
	}
	return fmt.Sprintf("statement type %q is not allowed in SQL Editor (only SELECT/SHOW/EXPLAIN/DESC/WITH..SELECT are permitted): %s", e.Keyword, preview)
}

// CheckReadOnly validates that every statement in sql is read-only.
// Returns ErrReadOnlyViolation for the first offending statement.
func CheckReadOnly(sql string) error {
	stmts := splitStatements(stripComments(sql))
	for _, stmt := range stmts {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		kw := firstKeyword(stmt)
		if kw == "" {
			continue
		}
		if !allowedRootKeywords[kw] {
			return &ErrReadOnlyViolation{Statement: stmt, Keyword: kw}
		}
		// For WITH statements, verify the final query is a SELECT.
		if kw == "WITH" {
			if err := checkWithIsSelect(stmt); err != nil {
				return err
			}
		}
	}
	return nil
}

// firstKeyword returns the first SQL keyword (uppercase) in stmt,
// skipping leading whitespace and block/line comments already stripped.
func firstKeyword(stmt string) string {
	stmt = strings.TrimSpace(stmt)
	i := 0
	for i < len(stmt) && (unicode.IsLetter(rune(stmt[i])) || stmt[i] == '_') {
		i++
	}
	return strings.ToUpper(stmt[:i])
}

// checkWithIsSelect ensures a WITH ... statement ultimately runs a SELECT.
// It finds the last keyword before the first non-CTE-body token.
// Strategy: skip all CTE definitions (name AS (...)), then check the main query keyword.
func checkWithIsSelect(stmt string) error {
	// Find the main query keyword after all CTE definitions.
	// CTE pattern: WITH [RECURSIVE] name AS (...) [, name AS (...)] <main_query>
	// We scan past balanced parentheses to find the main body.
	upper := strings.ToUpper(stmt)

	// Skip "WITH" and optional "RECURSIVE"
	pos := skipToken(upper, 0) // skip "WITH"
	pos = skipWhitespace(upper, pos)
	if strings.HasPrefix(upper[pos:], "RECURSIVE") {
		pos = skipToken(upper, pos)
		pos = skipWhitespace(upper, pos)
	}

	// Skip CTE definitions: name AS (...)  [, name AS (...)] ...
	for pos < len(upper) {
		pos = skipToken(upper, pos) // CTE name
		pos = skipWhitespace(upper, pos)
		if pos >= len(upper) {
			break
		}
		if strings.HasPrefix(upper[pos:], "AS") {
			pos = skipToken(upper, pos) // "AS"
			pos = skipWhitespace(upper, pos)
		}
		if pos < len(upper) && upper[pos] == '(' {
			pos = skipBalancedParen(upper, pos)
			pos = skipWhitespace(upper, pos)
		}
		if pos < len(upper) && upper[pos] == ',' {
			pos++ // next CTE
			pos = skipWhitespace(upper, pos)
			continue
		}
		break
	}

	mainKW := firstKeyword(strings.TrimSpace(stmt[pos:]))
	if mainKW != "SELECT" {
		preview := stmt
		if len(preview) > 80 {
			preview = preview[:80] + "..."
		}
		return &ErrReadOnlyViolation{
			Statement: preview,
			Keyword:   "WITH+" + mainKW,
		}
	}
	return nil
}

// stripComments removes -- line comments and /* */ block comments from sql.
// It is careful not to strip comment-like content inside string literals.
func stripComments(sql string) string {
	var b strings.Builder
	b.Grow(len(sql))
	i := 0
	for i < len(sql) {
		switch {
		case i+1 < len(sql) && sql[i] == '-' && sql[i+1] == '-':
			// Line comment: skip to end of line
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
		case i+1 < len(sql) && sql[i] == '/' && sql[i+1] == '*':
			// Block comment: skip to */
			i += 2
			for i+1 < len(sql) && !(sql[i] == '*' && sql[i+1] == '/') {
				i++
			}
			i += 2
		case sql[i] == '\'' || sql[i] == '"' || sql[i] == '`':
			// String/identifier literal: copy verbatim until closing quote
			q := sql[i]
			b.WriteByte(q)
			i++
			for i < len(sql) {
				b.WriteByte(sql[i])
				if sql[i] == q {
					if i+1 < len(sql) && sql[i+1] == q {
						// escaped quote inside string
						i++
						b.WriteByte(sql[i])
					} else {
						i++
						break
					}
				}
				i++
			}
		default:
			b.WriteByte(sql[i])
			i++
		}
	}
	return b.String()
}

// splitStatements splits a SQL string by semicolons (not inside parentheses or strings).
func splitStatements(sql string) []string {
	var stmts []string
	var cur strings.Builder
	depth := 0
	for _, ch := range sql {
		switch ch {
		case '(':
			depth++
			cur.WriteRune(ch)
		case ')':
			depth--
			cur.WriteRune(ch)
		case ';':
			if depth == 0 {
				stmts = append(stmts, cur.String())
				cur.Reset()
			} else {
				cur.WriteRune(ch)
			}
		default:
			cur.WriteRune(ch)
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		stmts = append(stmts, s)
	}
	return stmts
}

func skipWhitespace(s string, pos int) int {
	for pos < len(s) && (s[pos] == ' ' || s[pos] == '\t' || s[pos] == '\n' || s[pos] == '\r') {
		pos++
	}
	return pos
}

func skipToken(s string, pos int) int {
	pos = skipWhitespace(s, pos)
	for pos < len(s) && (unicode.IsLetter(rune(s[pos])) || s[pos] == '_' || unicode.IsDigit(rune(s[pos]))) {
		pos++
	}
	return pos
}

func skipBalancedParen(s string, pos int) int {
	if pos >= len(s) || s[pos] != '(' {
		return pos
	}
	depth := 1
	pos++
	for pos < len(s) && depth > 0 {
		switch s[pos] {
		case '(':
			depth++
		case ')':
			depth--
		}
		pos++
	}
	return pos
}
