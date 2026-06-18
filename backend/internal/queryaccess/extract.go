package queryaccess

import (
	"fmt"
	"strings"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/sqlparse"
	pg_query "github.com/pganalyze/pg_query_go/v6"
	tidbast "github.com/pingcap/tidb/pkg/parser/ast"
)

func extractStatementRefs(dialect sqlparse.Dialect, conn *model.DBConnection, stmt sqlparse.ParsedStatement, checkCtx CheckContext) ([]ObjectRef, error) {
	switch dialect {
	case sqlparse.DialectMySQL:
		return extractMySQLStatementRefs(conn, stmt, checkCtx)
	case sqlparse.DialectPostgres:
		return extractPostgresStatementRefs(conn, stmt, checkCtx)
	default:
		return nil, nil
	}
}

func extractMySQLStatementRefs(conn *model.DBConnection, stmt sqlparse.ParsedStatement, checkCtx CheckContext) ([]ObjectRef, error) {
	switch node := stmt.AST.(type) {
	case *tidbast.SelectStmt:
		return collectMySQLSelectRefs(conn, node, checkCtx, map[string]struct{}{})
	case *tidbast.SetOprStmt:
		return collectMySQLSetOprRefs(conn, node, checkCtx, map[string]struct{}{})
	case *tidbast.ExplainStmt:
		switch inner := node.Stmt.(type) {
		case *tidbast.SelectStmt:
			return collectMySQLSelectRefs(conn, inner, checkCtx, map[string]struct{}{})
		case *tidbast.SetOprStmt:
			return collectMySQLSetOprRefs(conn, inner, checkCtx, map[string]struct{}{})
		default:
			return nil, nil
		}
	default:
		return nil, nil
	}
}

func collectMySQLSelectRefs(conn *model.DBConnection, stmt *tidbast.SelectStmt, checkCtx CheckContext, ctes map[string]struct{}) ([]ObjectRef, error) {
	nextCTEs := cloneCTEMap(ctes)
	refs := make([]ObjectRef, 0)
	if stmt.With != nil {
		for _, cte := range stmt.With.CTEs {
			if cte == nil {
				continue
			}
			nextCTEs[strings.ToLower(cte.Name.O)] = struct{}{}
			if cte.Query == nil {
				continue
			}
			cteRefs, err := collectMySQLResultSetRefs(conn, cte.Query.Query, checkCtx, nextCTEs)
			if err != nil {
				return nil, err
			}
			refs = append(refs, cteRefs...)
		}
	}
	fromRefs, err := collectMySQLTableRefsClause(conn, stmt.From, checkCtx, nextCTEs)
	if err != nil {
		return nil, err
	}
	return append(refs, fromRefs...), nil
}

func collectMySQLSetOprRefs(conn *model.DBConnection, stmt *tidbast.SetOprStmt, checkCtx CheckContext, ctes map[string]struct{}) ([]ObjectRef, error) {
	nextCTEs := cloneCTEMap(ctes)
	refs := make([]ObjectRef, 0)
	if stmt.With != nil {
		for _, cte := range stmt.With.CTEs {
			if cte == nil {
				continue
			}
			nextCTEs[strings.ToLower(cte.Name.O)] = struct{}{}
			if cte.Query == nil {
				continue
			}
			cteRefs, err := collectMySQLResultSetRefs(conn, cte.Query.Query, checkCtx, nextCTEs)
			if err != nil {
				return nil, err
			}
			refs = append(refs, cteRefs...)
		}
	}
	if stmt.SelectList == nil {
		return refs, nil
	}
	for _, selectNode := range stmt.SelectList.Selects {
		switch typed := selectNode.(type) {
		case *tidbast.SelectStmt:
			items, err := collectMySQLSelectRefs(conn, typed, checkCtx, nextCTEs)
			if err != nil {
				return nil, err
			}
			refs = append(refs, items...)
		case *tidbast.SetOprSelectList:
			for _, child := range typed.Selects {
				if selectStmt, ok := child.(*tidbast.SelectStmt); ok {
					items, err := collectMySQLSelectRefs(conn, selectStmt, checkCtx, nextCTEs)
					if err != nil {
						return nil, err
					}
					refs = append(refs, items...)
				}
			}
		}
	}
	return refs, nil
}

func collectMySQLResultSetRefs(conn *model.DBConnection, node tidbast.ResultSetNode, checkCtx CheckContext, ctes map[string]struct{}) ([]ObjectRef, error) {
	switch typed := node.(type) {
	case *tidbast.SelectStmt:
		return collectMySQLSelectRefs(conn, typed, checkCtx, ctes)
	case *tidbast.SetOprStmt:
		return collectMySQLSetOprRefs(conn, typed, checkCtx, ctes)
	default:
		return nil, nil
	}
}

func collectMySQLTableRefsClause(conn *model.DBConnection, from *tidbast.TableRefsClause, checkCtx CheckContext, ctes map[string]struct{}) ([]ObjectRef, error) {
	if from == nil || from.TableRefs == nil {
		return nil, nil
	}
	return collectMySQLJoinRefs(conn, from.TableRefs, checkCtx, ctes)
}

func collectMySQLJoinRefs(conn *model.DBConnection, join *tidbast.Join, checkCtx CheckContext, ctes map[string]struct{}) ([]ObjectRef, error) {
	if join == nil {
		return nil, nil
	}
	refs := make([]ObjectRef, 0)
	left, err := collectMySQLSourceNodeRefs(conn, join.Left, checkCtx, ctes)
	if err != nil {
		return nil, err
	}
	refs = append(refs, left...)
	right, err := collectMySQLSourceNodeRefs(conn, join.Right, checkCtx, ctes)
	if err != nil {
		return nil, err
	}
	refs = append(refs, right...)
	return refs, nil
}

func collectMySQLSourceNodeRefs(conn *model.DBConnection, node tidbast.ResultSetNode, checkCtx CheckContext, ctes map[string]struct{}) ([]ObjectRef, error) {
	switch typed := node.(type) {
	case *tidbast.TableSource:
		return collectMySQLTableSourceRefs(conn, typed, checkCtx, ctes)
	case *tidbast.Join:
		return collectMySQLJoinRefs(conn, typed, checkCtx, ctes)
	default:
		return nil, nil
	}
}

func collectMySQLTableSourceRefs(conn *model.DBConnection, source *tidbast.TableSource, checkCtx CheckContext, ctes map[string]struct{}) ([]ObjectRef, error) {
	switch typed := source.Source.(type) {
	case *tidbast.TableName:
		if typed.Schema.O == "" {
			if _, ok := ctes[strings.ToLower(typed.Name.O)]; ok {
				return nil, nil
			}
		}
		databaseName := strings.TrimSpace(typed.Schema.O)
		if databaseName == "" {
			databaseName = effectiveDatabaseName(conn, checkCtx)
		}
		return []ObjectRef{{
			ConnectionID: conn.ID,
			DatabaseName: databaseName,
			TableName:    typed.Name.O,
		}}, nil
	case *tidbast.SelectStmt:
		return collectMySQLSelectRefs(conn, typed, checkCtx, ctes)
	case *tidbast.SetOprStmt:
		return collectMySQLSetOprRefs(conn, typed, checkCtx, ctes)
	case *tidbast.Join:
		return collectMySQLJoinRefs(conn, typed, checkCtx, ctes)
	default:
		return nil, nil
	}
}

func extractPostgresStatementRefs(conn *model.DBConnection, stmt sqlparse.ParsedStatement, checkCtx CheckContext) ([]ObjectRef, error) {
	switch node := stmt.AST.(*pg_query.Node); {
	case node == nil:
		return nil, nil
	case node.GetSelectStmt() != nil:
		return collectPostgresSelectRefs(conn, node.GetSelectStmt(), checkCtx, map[string]struct{}{})
	case node.GetExplainStmt() != nil && node.GetExplainStmt().GetQuery() != nil:
		query := node.GetExplainStmt().GetQuery()
		if query.GetSelectStmt() != nil {
			return collectPostgresSelectRefs(conn, query.GetSelectStmt(), checkCtx, map[string]struct{}{})
		}
		return nil, nil
	default:
		return nil, nil
	}
}

func collectPostgresSelectRefs(conn *model.DBConnection, stmt *pg_query.SelectStmt, checkCtx CheckContext, ctes map[string]struct{}) ([]ObjectRef, error) {
	nextCTEs := cloneCTEMap(ctes)
	refs := make([]ObjectRef, 0)
	if withClause := stmt.GetWithClause(); withClause != nil {
		for _, cteNode := range withClause.GetCtes() {
			cte := cteNode.GetCommonTableExpr()
			if cte == nil {
				continue
			}
			nextCTEs[strings.ToLower(cte.GetCtename())] = struct{}{}
			query := cte.GetCtequery()
			if query == nil {
				continue
			}
			if query.GetSelectStmt() != nil {
				items, err := collectPostgresSelectRefs(conn, query.GetSelectStmt(), checkCtx, nextCTEs)
				if err != nil {
					return nil, err
				}
				refs = append(refs, items...)
			}
		}
	}

	for _, fromNode := range stmt.GetFromClause() {
		items, err := collectPostgresFromNodeRefs(conn, fromNode, checkCtx, nextCTEs)
		if err != nil {
			return nil, err
		}
		refs = append(refs, items...)
	}
	if stmt.GetLarg() != nil {
		items, err := collectPostgresSelectRefs(conn, stmt.GetLarg(), checkCtx, nextCTEs)
		if err != nil {
			return nil, err
		}
		refs = append(refs, items...)
	}
	if stmt.GetRarg() != nil {
		items, err := collectPostgresSelectRefs(conn, stmt.GetRarg(), checkCtx, nextCTEs)
		if err != nil {
			return nil, err
		}
		refs = append(refs, items...)
	}
	return refs, nil
}

func collectPostgresFromNodeRefs(conn *model.DBConnection, node *pg_query.Node, checkCtx CheckContext, ctes map[string]struct{}) ([]ObjectRef, error) {
	switch {
	case node == nil:
		return nil, nil
	case node.GetRangeVar() != nil:
		rangeVar := node.GetRangeVar()
		if _, ok := ctes[strings.ToLower(rangeVar.GetRelname())]; ok && strings.TrimSpace(rangeVar.GetSchemaname()) == "" {
			return nil, nil
		}
		schemaName := strings.TrimSpace(rangeVar.GetSchemaname())
		databaseName := effectiveDatabaseName(conn, checkCtx)
		if schemaName == "" {
			schemaName = strings.TrimSpace(checkCtx.SchemaName)
		}
		return []ObjectRef{{
			ConnectionID: conn.ID,
			DatabaseName: databaseName,
			SchemaName:   schemaName,
			TableName:    rangeVar.GetRelname(),
		}}, nil
	case node.GetJoinExpr() != nil:
		join := node.GetJoinExpr()
		left, err := collectPostgresFromNodeRefs(conn, join.GetLarg(), checkCtx, ctes)
		if err != nil {
			return nil, err
		}
		right, err := collectPostgresFromNodeRefs(conn, join.GetRarg(), checkCtx, ctes)
		if err != nil {
			return nil, err
		}
		return append(left, right...), nil
	case node.GetRangeSubselect() != nil:
		sub := node.GetRangeSubselect()
		if sub.GetSubquery() == nil || sub.GetSubquery().GetSelectStmt() == nil {
			return nil, nil
		}
		return collectPostgresSelectRefs(conn, sub.GetSubquery().GetSelectStmt(), checkCtx, ctes)
	default:
		return nil, nil
	}
}

func cloneCTEMap(src map[string]struct{}) map[string]struct{} {
	if len(src) == 0 {
		return make(map[string]struct{})
	}
	dst := make(map[string]struct{}, len(src))
	for key := range src {
		dst[key] = struct{}{}
	}
	return dst
}

func effectiveDatabaseName(conn *model.DBConnection, checkCtx CheckContext) string {
	if strings.TrimSpace(checkCtx.DatabaseName) != "" {
		return strings.TrimSpace(checkCtx.DatabaseName)
	}
	if conn == nil {
		return ""
	}
	if conn.DatabaseName == nil {
		return ""
	}
	return strings.TrimSpace(*conn.DatabaseName)
}

func debugUnsupported(msg string, args ...any) error {
	return fmt.Errorf(msg, args...)
}
