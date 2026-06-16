package sqlparse

import (
	"fmt"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
	tidbast "github.com/pingcap/tidb/pkg/parser/ast"
)

// RewriteSelectLimit normalizes a single statement and injects a top-level LIMIT
// for SELECT statements when the statement does not already define one.
func RewriteSelectLimit(dialect Dialect, sql string, limit int) (string, bool, error) {
	parsed, err := ParseSQL(dialect, sql)
	if err != nil {
		return "", false, err
	}
	if len(parsed.Statements) != 1 {
		return sql, false, nil
	}

	stmt := parsed.Statements[0]
	normalized := strings.TrimSpace(strings.TrimSuffix(stmt.RawSQL, ";"))
	if normalized == "" {
		return sql, false, nil
	}
	if stmt.Kind != StatementKindSelect {
		return normalized, false, nil
	}
	if limit <= 0 {
		return normalized, false, nil
	}

	switch dialect {
	case DialectMySQL:
		return rewriteMySQLSelectLimit(stmt, limit)
	case DialectPostgres:
		return rewritePostgresSelectLimit(normalized, limit)
	default:
		return normalized, false, nil
	}
}

func rewriteMySQLSelectLimit(stmt ParsedStatement, limit int) (string, bool, error) {
	limitNode := &tidbast.Limit{
		Count: tidbast.NewValueExpr(int64(limit), "", ""),
	}

	switch node := stmt.AST.(type) {
	case *tidbast.SelectStmt:
		if node.Limit != nil {
			return restoreNode(node), false, nil
		}
		node.Limit = limitNode
		return restoreNode(node), true, nil
	case *tidbast.SetOprStmt:
		if node.Limit != nil {
			return restoreNode(node), false, nil
		}
		node.Limit = limitNode
		return restoreNode(node), true, nil
	default:
		return "", false, fmt.Errorf("mysql select AST type %T is not supported for limit rewrite", stmt.AST)
	}
}

func rewritePostgresSelectLimit(sql string, limit int) (string, bool, error) {
	tree, err := pg_query.Parse(sql)
	if err != nil {
		return "", false, err
	}
	if len(tree.Stmts) != 1 || tree.Stmts[0] == nil || tree.Stmts[0].Stmt == nil {
		return sql, false, nil
	}

	selectStmt := tree.Stmts[0].Stmt.GetSelectStmt()
	if selectStmt == nil {
		return strings.TrimSpace(strings.TrimSuffix(sql, ";")), false, nil
	}
	if selectStmt.LimitCount != nil || selectStmt.LimitOption == pg_query.LimitOption_LIMIT_OPTION_WITH_TIES {
		normalized, deparseErr := pg_query.Deparse(tree)
		if deparseErr != nil {
			return strings.TrimSpace(strings.TrimSuffix(sql, ";")), false, nil
		}
		return strings.TrimSpace(normalized), false, nil
	}

	selectStmt.LimitCount = pg_query.MakeAConstIntNode(int64(limit), -1)
	selectStmt.LimitOption = pg_query.LimitOption_LIMIT_OPTION_COUNT

	normalized, err := pg_query.Deparse(tree)
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(normalized), true, nil
}
