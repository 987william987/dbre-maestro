package sqlreview

import (
	"fmt"
	"strings"
)

var statementRootKeywords = map[string]struct{}{
	"ALTER":    {},
	"CREATE":   {},
	"DELETE":   {},
	"DROP":     {},
	"INSERT":   {},
	"MERGE":    {},
	"REPLACE":  {},
	"TRUNCATE": {},
	"UPDATE":   {},
}

// CheckDMLNoWhere returns an error if an UPDATE or DELETE statement lacks a WHERE clause.
// This is a string-heuristic check; DDL and other statement types are skipped.
func CheckDMLNoWhere(sqlStr string) error {
	upper := strings.ToUpper(strings.TrimSpace(sqlStr))
	if !strings.HasPrefix(upper, "UPDATE") && !strings.HasPrefix(upper, "DELETE") {
		return nil
	}
	// Accept both " WHERE " and trailing "WHERE" (unlikely but safe)
	if strings.Contains(upper, " WHERE ") || strings.HasSuffix(upper, " WHERE") {
		return nil
	}
	return fmt.Errorf("UPDATE/DELETE 缺少 WHERE 條件")
}

// CheckDDLTableComment returns an error if a CREATE TABLE statement lacks a COMMENT clause.
func CheckDDLTableComment(sqlStr string) error {
	upper := strings.ToUpper(strings.TrimSpace(sqlStr))
	if !strings.Contains(upper, "CREATE") || !strings.Contains(upper, "TABLE") {
		return nil
	}
	if strings.Contains(upper, "COMMENT") {
		return nil
	}
	return fmt.Errorf("CREATE TABLE 缺少表備注 (COMMENT)")
}

// CheckRequireUTF8MB4 returns an error if a CREATE/ALTER TABLE statement
// does not specify utf8mb4 as the character set.
func CheckRequireUTF8MB4(sqlStr string) error {
	upper := strings.ToUpper(strings.TrimSpace(sqlStr))
	isTable := (strings.Contains(upper, "CREATE") || strings.Contains(upper, "ALTER")) &&
		strings.Contains(upper, "TABLE")
	if !isTable {
		return nil
	}
	if strings.Contains(upper, "UTF8MB4") {
		return nil
	}
	return fmt.Errorf("CREATE/ALTER TABLE 必須使用 utf8mb4 字符集")
}

// CheckLikelyMissingStatementDelimiter detects multiple top-level statement roots
// inside a single SQL fragment. This usually means a missing semicolon caused
// multiple statements to be concatenated together.
func CheckLikelyMissingStatementDelimiter(sqlStr string) error {
	roots := collectTopLevelStatementRoots(sqlStr)
	if len(roots) <= 1 {
		return nil
	}
	return fmt.Errorf("疑似缺少分號，單一語句內檢測到多個頂層指令: %s", strings.Join(roots, ", "))
}

// RunStaticChecks runs all enabled static rules against a single SQL statement.
// ruleMap keys: "dml_no_where", "ddl_no_comment", "require_utf8mb4"
func RunStaticChecks(sqlStr string, ruleMap map[string]bool) []string {
	var issues []string
	if ruleMap["dml_no_where"] {
		if err := CheckDMLNoWhere(sqlStr); err != nil {
			issues = append(issues, err.Error())
		}
	}
	if ruleMap["ddl_no_comment"] {
		if err := CheckDDLTableComment(sqlStr); err != nil {
			issues = append(issues, err.Error())
		}
	}
	if ruleMap["require_utf8mb4"] {
		if err := CheckRequireUTF8MB4(sqlStr); err != nil {
			issues = append(issues, err.Error())
		}
	}
	return issues
}

func collectTopLevelStatementRoots(sqlStr string) []string {
	roots := make([]string, 0, 2)
	var token strings.Builder
	depth := 0
	quote := byte(0)
	inLineComment := false
	inBlockComment := false

	flushToken := func() {
		if token.Len() == 0 || depth != 0 {
			token.Reset()
			return
		}
		word := strings.ToUpper(token.String())
		token.Reset()
		if _, ok := statementRootKeywords[word]; !ok {
			return
		}
		roots = append(roots, word)
	}

	for i := 0; i < len(sqlStr); i++ {
		ch := sqlStr[i]
		next := byte(0)
		if i+1 < len(sqlStr) {
			next = sqlStr[i+1]
		}

		switch {
		case inLineComment:
			if ch == '\n' {
				inLineComment = false
			}
			continue
		case inBlockComment:
			if ch == '*' && next == '/' {
				inBlockComment = false
				i++
			}
			continue
		case quote != 0:
			if ch == quote {
				escaped := i > 0 && sqlStr[i-1] == '\\'
				if !escaped {
					quote = 0
				}
			}
			continue
		case ch == '-' && next == '-':
			flushToken()
			inLineComment = true
			i++
			continue
		case ch == '#':
			flushToken()
			inLineComment = true
			continue
		case ch == '/' && next == '*':
			flushToken()
			inBlockComment = true
			i++
			continue
		case ch == '\'' || ch == '"' || ch == '`':
			flushToken()
			quote = ch
			continue
		case ch == '(':
			flushToken()
			depth++
			continue
		case ch == ')':
			flushToken()
			if depth > 0 {
				depth--
			}
			continue
		}

		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || ch == '_' {
			token.WriteByte(ch)
			continue
		}
		flushToken()
	}

	flushToken()
	return roots
}
