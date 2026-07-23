package sqlparse

import (
	"reflect"
	"strings"

	tidbparser "github.com/pingcap/tidb/pkg/parser"
	tidbast "github.com/pingcap/tidb/pkg/parser/ast"
	tidbformat "github.com/pingcap/tidb/pkg/parser/format"
	_ "github.com/pingcap/tidb/pkg/parser/test_driver"
)

func parseMySQL(sql string) (*ParseResult, error) {
	p := tidbparser.New()
	stmts, _, err := p.Parse(sql, "", "")
	if err != nil {
		return nil, &SyntaxError{
			StatementSeq: estimateStatementSeq(sql),
			Message:      err.Error(),
		}
	}

	result := &ParseResult{Statements: make([]ParsedStatement, 0, len(stmts))}
	for index, stmt := range stmts {
		if stmt == nil {
			continue
		}
		raw := strings.TrimSpace(stmt.Text())
		if raw == "" {
			raw = restoreNode(stmt)
		}
		normalized := restoreNode(stmt)
		if normalized == "" {
			normalized = raw
		}
		result.Statements = append(result.Statements, ParsedStatement{
			Seq:           index + 1,
			RawSQL:        raw,
			NormalizedSQL: normalized,
			Kind:          classifyMySQLStatement(stmt),
			AST:           stmt,
		})
	}
	return result, nil
}

func classifyMySQLStatement(stmt tidbast.StmtNode) StatementKind {
	switch stmt.(type) {
	case *tidbast.SelectStmt, *tidbast.SetOprStmt:
		return StatementKindSelect
	case *tidbast.ShowStmt:
		return StatementKindShow
	case *tidbast.ExplainStmt:
		return StatementKindExplain
	case *tidbast.InsertStmt:
		return StatementKindInsert
	case *tidbast.UpdateStmt:
		return StatementKindUpdate
	case *tidbast.DeleteStmt:
		return StatementKindDelete
	case *tidbast.SetStmt:
		return StatementKindSet
	case *tidbast.TruncateTableStmt:
		return StatementKindTruncate
	}

	typeName := reflect.TypeOf(stmt).String()
	switch {
	case strings.Contains(typeName, ".Create"):
		return StatementKindCreate
	case strings.Contains(typeName, ".Alter"), strings.Contains(typeName, ".Rename"):
		return StatementKindAlter
	case strings.Contains(typeName, ".Drop"):
		return StatementKindDrop
	default:
		return StatementKindUnknown
	}
}

func restoreNode(stmt tidbast.StmtNode) string {
	var sb strings.Builder
	restoreCtx := tidbformat.NewRestoreCtx(tidbformat.DefaultRestoreFlags, &sb)
	if err := stmt.Restore(restoreCtx); err != nil {
		return ""
	}
	return strings.TrimSpace(sb.String())
}

func estimateStatementSeq(sql string) int {
	count := 1
	depth := 0
	var quote rune
	for _, ch := range sql {
		switch {
		case quote != 0:
			if ch == quote {
				quote = 0
			}
		case ch == '\'' || ch == '"' || ch == '`':
			quote = ch
		case ch == '(':
			depth++
		case ch == ')':
			if depth > 0 {
				depth--
			}
		case ch == ';' && depth == 0:
			count++
		}
	}
	return max(1, count)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
