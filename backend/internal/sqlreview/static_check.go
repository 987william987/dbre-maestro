package sqlreview

import (
	"fmt"
	"strings"
)

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
