package sqlparse

import (
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

func parsePostgres(sql string) (*ParseResult, error) {
	rawStatements, err := pg_query.SplitWithParser(sql, true)
	if err != nil {
		return nil, &SyntaxError{StatementSeq: estimateStatementSeq(sql), Message: err.Error()}
	}

	result := &ParseResult{Statements: make([]ParsedStatement, 0, len(rawStatements))}
	for index, raw := range rawStatements {
		tree, err := pg_query.Parse(raw)
		if err != nil {
			return nil, &SyntaxError{StatementSeq: index + 1, Message: err.Error()}
		}
		if len(tree.Stmts) == 0 {
			continue
		}

		normalized, err := pg_query.Deparse(tree)
		if err != nil {
			normalized = strings.TrimSpace(raw)
		}
		stmtNode := tree.Stmts[0].Stmt
		result.Statements = append(result.Statements, ParsedStatement{
			Seq:           index + 1,
			RawSQL:        strings.TrimSpace(raw),
			NormalizedSQL: strings.TrimSpace(normalized),
			Kind:          classifyPostgresStatement(stmtNode),
			AST:           stmtNode,
		})
	}
	return result, nil
}

func classifyPostgresStatement(node *pg_query.Node) StatementKind {
	switch {
	case node == nil:
		return StatementKindUnknown
	case node.GetSelectStmt() != nil:
		return StatementKindSelect
	case node.GetVariableSetStmt() != nil:
		return StatementKindUnknown
	case node.GetExplainStmt() != nil:
		return StatementKindExplain
	case node.GetInsertStmt() != nil:
		return StatementKindInsert
	case node.GetUpdateStmt() != nil:
		return StatementKindUpdate
	case node.GetDeleteStmt() != nil:
		return StatementKindDelete
	case node.GetCreateStmt() != nil:
		return StatementKindCreate
	case node.GetAlterTableStmt() != nil:
		return StatementKindAlter
	case node.GetDropStmt() != nil:
		return StatementKindDrop
	case node.GetTruncateStmt() != nil:
		return StatementKindTruncate
	default:
		return StatementKindUnknown
	}
}
