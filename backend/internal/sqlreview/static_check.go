package sqlreview

import (
	"fmt"
	"strings"

	"github.com/dbre-maestro/maestro/internal/sqlparse"
	pg_query "github.com/pganalyze/pg_query_go/v6"
	tidbast "github.com/pingcap/tidb/pkg/parser/ast"
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

// CheckRequireUTF8MB4 returns an error if a CREATE TABLE statement does not
// specify utf8mb4 as the character set.
func CheckRequireUTF8MB4(sqlStr string) error {
	upper := strings.ToUpper(strings.TrimSpace(sqlStr))
	isTable := strings.Contains(upper, "CREATE") && strings.Contains(upper, "TABLE")
	if !isTable {
		return nil
	}
	if strings.Contains(upper, "UTF8MB4") {
		return nil
	}
	return fmt.Errorf("CREATE TABLE 必須使用 utf8mb4 字符集")
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

func RunStaticChecksParsed(stmt sqlparse.ParsedStatement, ruleMap map[string]bool) []string {
	if stmt.AST == nil {
		return RunStaticChecks(stmt.RawSQL, ruleMap)
	}

	switch astNode := stmt.AST.(type) {
	case tidbast.StmtNode:
		return runMySQLStaticChecks(stmt, astNode, ruleMap)
	case *pg_query.Node:
		return runPostgresStaticChecks(astNode, ruleMap)
	default:
		return RunStaticChecks(stmt.RawSQL, ruleMap)
	}
}

func runMySQLStaticChecks(stmt sqlparse.ParsedStatement, astNode tidbast.StmtNode, ruleMap map[string]bool) []string {
	var issues []string
	if ruleMap["dml_no_where"] {
		if err := checkDMLNoWhereAST(astNode); err != nil {
			issues = append(issues, err.Error())
		}
	}
	if ruleMap["ddl_no_comment"] {
		if err := checkDDLTableCommentAST(astNode); err != nil {
			issues = append(issues, err.Error())
		}
	}
	if ruleMap["require_utf8mb4"] {
		if err := checkRequireUTF8MB4AST(astNode); err != nil {
			issues = append(issues, err.Error())
		}
	}
	if len(issues) == 0 && stmt.RawSQL != "" {
		return issues
	}
	return issues
}

func runPostgresStaticChecks(astNode *pg_query.Node, ruleMap map[string]bool) []string {
	var issues []string
	if ruleMap["dml_no_where"] {
		if err := checkDMLNoWherePostgresAST(astNode); err != nil {
			issues = append(issues, err.Error())
		}
	}
	if ruleMap["ddl_no_comment"] {
		if err := checkDDLTableCommentPostgresAST(astNode); err != nil {
			issues = append(issues, err.Error())
		}
	}
	if ruleMap["require_utf8mb4"] {
		if err := checkRequireUTF8MB4PostgresAST(astNode); err != nil {
			issues = append(issues, err.Error())
		}
	}
	return issues
}

func checkDMLNoWhereAST(stmt tidbast.StmtNode) error {
	switch s := stmt.(type) {
	case *tidbast.UpdateStmt:
		if s.Where == nil {
			return fmt.Errorf("UPDATE/DELETE 缺少 WHERE 條件")
		}
	case *tidbast.DeleteStmt:
		if s.Where == nil {
			return fmt.Errorf("UPDATE/DELETE 缺少 WHERE 條件")
		}
	}
	return nil
}

func checkDMLNoWherePostgresAST(node *pg_query.Node) error {
	switch {
	case node == nil:
		return nil
	case node.GetUpdateStmt() != nil:
		if node.GetUpdateStmt().WhereClause == nil {
			return fmt.Errorf("UPDATE/DELETE 缺少 WHERE 條件")
		}
	case node.GetDeleteStmt() != nil:
		if node.GetDeleteStmt().WhereClause == nil {
			return fmt.Errorf("UPDATE/DELETE 缺少 WHERE 條件")
		}
	}
	return nil
}

func checkDDLTableCommentAST(stmt tidbast.StmtNode) error {
	createStmt, ok := stmt.(*tidbast.CreateTableStmt)
	if !ok {
		return nil
	}
	for _, option := range createStmt.Options {
		if option != nil && option.Tp == tidbast.TableOptionComment && strings.TrimSpace(option.StrValue) != "" {
			return nil
		}
	}
	return fmt.Errorf("CREATE TABLE 缺少表備注 (COMMENT)")
}

func checkDDLTableCommentPostgresAST(_ *pg_query.Node) error {
	// PostgreSQL table comments are expressed via separate COMMENT ON statements
	// rather than inline CREATE/ALTER TABLE options, so this MySQL-specific rule
	// is treated as not applicable for PostgreSQL.
	return nil
}

func checkRequireUTF8MB4AST(stmt tidbast.StmtNode) error {
	switch s := stmt.(type) {
	case *tidbast.CreateTableStmt:
		if hasUTF8MB4Option(s.Options) {
			return nil
		}
		return fmt.Errorf("CREATE TABLE 必須使用 utf8mb4 字符集")
	default:
		return nil
	}
}

func checkRequireUTF8MB4PostgresAST(_ *pg_query.Node) error {
	// PostgreSQL does not support per-table utf8mb4 charset declarations.
	return nil
}

func hasUTF8MB4Option(options []*tidbast.TableOption) bool {
	for _, option := range options {
		if option == nil {
			continue
		}
		switch option.Tp {
		case tidbast.TableOptionCharset, tidbast.TableOptionCollate:
			if strings.Contains(strings.ToUpper(option.StrValue), "UTF8MB4") {
				return true
			}
		}
	}
	return false
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
