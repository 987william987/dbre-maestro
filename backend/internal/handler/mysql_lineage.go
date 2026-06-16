package handler

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/dbre-maestro/maestro/internal/masking"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/sqlparse"
	tidbast "github.com/pingcap/tidb/pkg/parser/ast"
	tidbformat "github.com/pingcap/tidb/pkg/parser/format"
)

type mysqlSchemaProvider interface {
	LoadColumns(ctx context.Context, databaseName, tableName string) ([]string, error)
}

type mysqlInfoSchemaProvider struct {
	conn  *sql.Conn
	cache map[string][]string
}

type mysqlLineageColumn struct {
	Name         string
	Origin       masking.ColumnOrigin
	Dependencies []masking.ColumnOrigin
}

type mysqlLineageSource struct {
	Alias    string
	Table    string
	Database string
	Columns  []mysqlLineageColumn
}

type mysqlLineageResolver struct {
	defaultDatabase string
	provider        mysqlSchemaProvider
}

func resolveMySQLLineageForStatement(
	ctx context.Context,
	conn *model.DBConnection,
	pinnedConn *sql.Conn,
	statement string,
	rawColumns []string,
	queryCtx queryExecutionContext,
) ([]mysqlLineageColumn, error) {
	parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, statement)
	if err != nil || len(parsed.Statements) != 1 {
		return nil, err
	}

	provider := &mysqlInfoSchemaProvider{
		conn:  pinnedConn,
		cache: make(map[string][]string),
	}
	resolver := &mysqlLineageResolver{
		defaultDatabase: effectiveQueryDatabaseName(conn, queryCtx),
		provider:        provider,
	}

	columns, err := resolver.resolveStatementColumns(ctx, parsed.Statements[0].AST)
	if err != nil {
		return nil, err
	}
	if len(columns) != len(rawColumns) {
		return nil, fmt.Errorf("mysql lineage column count mismatch: resolved=%d actual=%d", len(columns), len(rawColumns))
	}
	return columns, nil
}

func shouldResolveMySQLOrigins(rawColumns []string) bool {
	for _, column := range rawColumns {
		parts := strings.Split(strings.TrimSpace(column), ".")
		if len(parts) != 2 {
			return true
		}
	}
	return false
}

func (r *mysqlLineageResolver) resolveStatementColumns(ctx context.Context, astNode any) ([]mysqlLineageColumn, error) {
	switch node := astNode.(type) {
	case *tidbast.SelectStmt:
		return r.resolveSelectStmt(ctx, node, nil)
	case *tidbast.SetOprStmt:
		return r.resolveSetOprStmt(ctx, node, nil)
	default:
		return nil, fmt.Errorf("unsupported mysql lineage AST type %T", astNode)
	}
}

func (r *mysqlLineageResolver) resolveSelectStmt(
	ctx context.Context,
	stmt *tidbast.SelectStmt,
	parentCTEs map[string][]mysqlLineageColumn,
) ([]mysqlLineageColumn, error) {
	ctes := cloneLineageMap(parentCTEs)
	if stmt.With != nil {
		for _, cte := range stmt.With.CTEs {
			if cte == nil || cte.Query == nil {
				continue
			}
			queryColumns, err := r.resolveResultSetNode(ctx, cte.Query.Query, ctes)
			if err != nil {
				return nil, fmt.Errorf("resolve cte %s: %w", cte.Name.O, err)
			}
			queryColumns = applyColumnAliases(queryColumns, cte.ColNameList)
			ctes[strings.ToLower(cte.Name.O)] = queryColumns
		}
	}

	sources, err := r.resolveTableRefs(ctx, stmt.From, ctes)
	if err != nil {
		return nil, err
	}

	if stmt.Fields == nil {
		return nil, nil
	}

	result := make([]mysqlLineageColumn, 0, len(stmt.Fields.Fields))
	for _, field := range stmt.Fields.Fields {
		if field == nil || field.Auxiliary {
			continue
		}
		if field.WildCard != nil {
			expanded, err := expandWildcardField(field.WildCard, sources)
			if err != nil {
				return nil, err
			}
			result = append(result, expanded...)
			continue
		}

		lineage := mysqlLineageColumn{Name: field.AsName.O}
		if lineage.Name == "" {
			if colExpr, ok := field.Expr.(*tidbast.ColumnNameExpr); ok && colExpr.Name != nil {
				lineage.Name = colExpr.Name.Name.O
			} else {
				lineage.Name = strings.TrimSpace(restoreMySQLExpr(field.Expr))
			}
		}
		deps, origin, err := resolveExprLineage(field.Expr, sources)
		if err != nil {
			return nil, err
		}
		lineage.Origin = origin
		lineage.Dependencies = deps
		if lineage.Name == "" && origin.Column != "" {
			lineage.Name = origin.Column
		}
		result = append(result, lineage)
	}
	return result, nil
}

func (r *mysqlLineageResolver) resolveSetOprStmt(
	ctx context.Context,
	stmt *tidbast.SetOprStmt,
	parentCTEs map[string][]mysqlLineageColumn,
) ([]mysqlLineageColumn, error) {
	ctes := cloneLineageMap(parentCTEs)
	if stmt.With != nil {
		for _, cte := range stmt.With.CTEs {
			if cte == nil || cte.Query == nil {
				continue
			}
			queryColumns, err := r.resolveResultSetNode(ctx, cte.Query.Query, ctes)
			if err != nil {
				return nil, fmt.Errorf("resolve cte %s: %w", cte.Name.O, err)
			}
			queryColumns = applyColumnAliases(queryColumns, cte.ColNameList)
			ctes[strings.ToLower(cte.Name.O)] = queryColumns
		}
	}

	if stmt.SelectList == nil || len(stmt.SelectList.Selects) == 0 {
		return nil, nil
	}

	var merged []mysqlLineageColumn
	for idx, selectNode := range stmt.SelectList.Selects {
		cols, err := r.resolveSetOperand(ctx, selectNode, ctes)
		if err != nil {
			return nil, err
		}
		if idx == 0 {
			merged = cols
			continue
		}
		merged = mergeSetOperationColumns(merged, cols)
	}
	return merged, nil
}

func (r *mysqlLineageResolver) resolveSetOperand(
	ctx context.Context,
	node tidbast.Node,
	ctes map[string][]mysqlLineageColumn,
) ([]mysqlLineageColumn, error) {
	switch typed := node.(type) {
	case *tidbast.SelectStmt:
		return r.resolveSelectStmt(ctx, typed, ctes)
	case *tidbast.SetOprSelectList:
		var merged []mysqlLineageColumn
		for idx, child := range typed.Selects {
			cols, err := r.resolveSetOperand(ctx, child, ctes)
			if err != nil {
				return nil, err
			}
			if idx == 0 {
				merged = cols
				continue
			}
			merged = mergeSetOperationColumns(merged, cols)
		}
		return merged, nil
	default:
		return nil, fmt.Errorf("unsupported mysql set operand %T", node)
	}
}

func (r *mysqlLineageResolver) resolveResultSetNode(
	ctx context.Context,
	node tidbast.ResultSetNode,
	ctes map[string][]mysqlLineageColumn,
) ([]mysqlLineageColumn, error) {
	switch typed := node.(type) {
	case *tidbast.SelectStmt:
		return r.resolveSelectStmt(ctx, typed, ctes)
	case *tidbast.SetOprStmt:
		return r.resolveSetOprStmt(ctx, typed, ctes)
	default:
		return nil, fmt.Errorf("unsupported result set node %T", node)
	}
}

func (r *mysqlLineageResolver) resolveTableRefs(
	ctx context.Context,
	from *tidbast.TableRefsClause,
	ctes map[string][]mysqlLineageColumn,
) ([]mysqlLineageSource, error) {
	if from == nil || from.TableRefs == nil {
		return nil, nil
	}
	return r.resolveJoinSources(ctx, from.TableRefs, ctes)
}

func (r *mysqlLineageResolver) resolveJoinSources(
	ctx context.Context,
	join *tidbast.Join,
	ctes map[string][]mysqlLineageColumn,
) ([]mysqlLineageSource, error) {
	sources := make([]mysqlLineageSource, 0, 2)
	if join == nil {
		return sources, nil
	}

	leftSources, err := r.resolveResultSetSources(ctx, join.Left, ctes)
	if err != nil {
		return nil, err
	}
	sources = append(sources, leftSources...)

	rightSources, err := r.resolveResultSetSources(ctx, join.Right, ctes)
	if err != nil {
		return nil, err
	}
	sources = append(sources, rightSources...)
	return sources, nil
}

func (r *mysqlLineageResolver) resolveResultSetSources(
	ctx context.Context,
	node tidbast.ResultSetNode,
	ctes map[string][]mysqlLineageColumn,
) ([]mysqlLineageSource, error) {
	if node == nil {
		return nil, nil
	}
	switch typed := node.(type) {
	case *tidbast.TableSource:
		source, err := r.resolveTableSource(ctx, typed, ctes)
		if err != nil {
			return nil, err
		}
		return []mysqlLineageSource{source}, nil
	case *tidbast.Join:
		return r.resolveJoinSources(ctx, typed, ctes)
	default:
		return nil, fmt.Errorf("unsupported mysql source node %T", node)
	}
}

func (r *mysqlLineageResolver) resolveTableSource(
	ctx context.Context,
	source *tidbast.TableSource,
	ctes map[string][]mysqlLineageColumn,
) (mysqlLineageSource, error) {
	switch typed := source.Source.(type) {
	case *tidbast.TableName:
		if typed.Schema.O == "" {
			if cteColumns, ok := ctes[strings.ToLower(typed.Name.O)]; ok {
				alias := source.AsName.O
				if alias == "" {
					alias = typed.Name.O
				}
				return mysqlLineageSource{
					Alias:   alias,
					Table:   typed.Name.O,
					Columns: applyColumnAliases(cteColumns, source.ColumnNames),
				}, nil
			}
		}

		databaseName := typed.Schema.O
		if databaseName == "" {
			databaseName = r.defaultDatabase
		}
		columnNames, err := r.provider.LoadColumns(ctx, databaseName, typed.Name.O)
		if err != nil {
			return mysqlLineageSource{}, fmt.Errorf("load mysql columns for %s.%s: %w", databaseName, typed.Name.O, err)
		}
		columns := make([]mysqlLineageColumn, 0, len(columnNames))
		for _, columnName := range columnNames {
			columns = append(columns, mysqlLineageColumn{
				Name: columnName,
				Origin: masking.ColumnOrigin{
					Database: databaseName,
					Table:    typed.Name.O,
					Column:   columnName,
				},
				Dependencies: []masking.ColumnOrigin{{
					Database: databaseName,
					Table:    typed.Name.O,
					Column:   columnName,
				}},
			})
		}
		alias := source.AsName.O
		if alias == "" {
			alias = typed.Name.O
		}
		return mysqlLineageSource{
			Alias:    alias,
			Table:    typed.Name.O,
			Database: databaseName,
			Columns:  columns,
		}, nil
	case *tidbast.SelectStmt:
		columns, err := r.resolveSelectStmt(ctx, typed, ctes)
		if err != nil {
			return mysqlLineageSource{}, err
		}
		alias := source.AsName.O
		if alias == "" {
			alias = "derived"
		}
		return mysqlLineageSource{
			Alias:   alias,
			Table:   alias,
			Columns: applyColumnAliases(columns, source.ColumnNames),
		}, nil
	case *tidbast.SetOprStmt:
		columns, err := r.resolveSetOprStmt(ctx, typed, ctes)
		if err != nil {
			return mysqlLineageSource{}, err
		}
		alias := source.AsName.O
		if alias == "" {
			alias = "derived"
		}
		return mysqlLineageSource{
			Alias:   alias,
			Table:   alias,
			Columns: applyColumnAliases(columns, source.ColumnNames),
		}, nil
	case *tidbast.Join:
		nestedSources, err := r.resolveJoinSources(ctx, typed, ctes)
		if err != nil {
			return mysqlLineageSource{}, err
		}
		columns := flattenSourceColumns(nestedSources)
		alias := source.AsName.O
		if alias == "" {
			alias = "join"
		}
		return mysqlLineageSource{
			Alias:   alias,
			Table:   alias,
			Columns: applyColumnAliases(columns, source.ColumnNames),
		}, nil
	default:
		return mysqlLineageSource{}, fmt.Errorf("unsupported mysql table source %T", source.Source)
	}
}

func expandWildcardField(field *tidbast.WildCardField, sources []mysqlLineageSource) ([]mysqlLineageColumn, error) {
	if field == nil {
		return nil, nil
	}
	if field.Table.O == "" {
		return flattenSourceColumns(sources), nil
	}

	var matched []mysqlLineageColumn
	for _, source := range sources {
		if strings.EqualFold(strings.TrimSpace(field.Table.O), strings.TrimSpace(source.Alias)) ||
			strings.EqualFold(strings.TrimSpace(field.Table.O), strings.TrimSpace(source.Table)) {
			if field.Schema.O != "" && !strings.EqualFold(strings.TrimSpace(field.Schema.O), strings.TrimSpace(source.Database)) {
				continue
			}
			matched = append(matched, source.Columns...)
		}
	}
	if len(matched) == 0 {
		return nil, fmt.Errorf("mysql wildcard %s.* did not match any source", field.Table.O)
	}
	return matched, nil
}

func resolveExprLineage(expr tidbast.ExprNode, sources []mysqlLineageSource) ([]masking.ColumnOrigin, masking.ColumnOrigin, error) {
	if expr == nil {
		return nil, masking.ColumnOrigin{}, nil
	}
	columnOrigins := collectColumnExprOrigins(expr, sources)
	switch len(columnOrigins) {
	case 0:
		return nil, masking.ColumnOrigin{}, nil
	case 1:
		return columnOrigins, columnOrigins[0], nil
	default:
		return columnOrigins, masking.ColumnOrigin{}, nil
	}
}

func collectColumnExprOrigins(expr tidbast.ExprNode, sources []mysqlLineageSource) []masking.ColumnOrigin {
	collector := &mysqlColumnRefCollector{}
	_, _ = expr.Accept(collector)

	origins := make([]masking.ColumnOrigin, 0, len(collector.columns))
	seen := make(map[string]struct{}, len(collector.columns))
	for _, col := range collector.columns {
		origin, ok := resolveColumnReference(col, sources)
		if !ok {
			continue
		}
		key := strings.ToLower(origin.Database) + "|" + strings.ToLower(origin.Table) + "|" + strings.ToLower(origin.Column)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		origins = append(origins, origin)
	}
	return origins
}

type mysqlColumnRefCollector struct {
	columns []*tidbast.ColumnName
}

func (c *mysqlColumnRefCollector) Enter(node tidbast.Node) (tidbast.Node, bool) {
	if colExpr, ok := node.(*tidbast.ColumnNameExpr); ok && colExpr.Name != nil {
		c.columns = append(c.columns, colExpr.Name)
	}
	return node, false
}

func (c *mysqlColumnRefCollector) Leave(node tidbast.Node) (tidbast.Node, bool) {
	return node, true
}

func resolveColumnReference(col *tidbast.ColumnName, sources []mysqlLineageSource) (masking.ColumnOrigin, bool) {
	if col == nil {
		return masking.ColumnOrigin{}, false
	}

	var matches []masking.ColumnOrigin
	for _, source := range sources {
		if col.Table.O != "" &&
			!strings.EqualFold(col.Table.O, source.Alias) &&
			!strings.EqualFold(col.Table.O, source.Table) {
			continue
		}
		if col.Schema.O != "" && source.Database != "" && !strings.EqualFold(col.Schema.O, source.Database) {
			continue
		}
		for _, sourceColumn := range source.Columns {
			if strings.EqualFold(col.Name.O, sourceColumn.Name) || strings.EqualFold(col.Name.O, sourceColumn.Origin.Column) {
				matches = append(matches, sourceColumn.Origin)
			}
		}
	}

	if len(matches) != 1 {
		return masking.ColumnOrigin{}, false
	}
	return matches[0], true
}

func flattenSourceColumns(sources []mysqlLineageSource) []mysqlLineageColumn {
	total := 0
	for _, source := range sources {
		total += len(source.Columns)
	}
	columns := make([]mysqlLineageColumn, 0, total)
	for _, source := range sources {
		columns = append(columns, source.Columns...)
	}
	return columns
}

func applyColumnAliases(columns []mysqlLineageColumn, aliases []tidbast.CIStr) []mysqlLineageColumn {
	if len(aliases) == 0 {
		return columns
	}
	aliased := make([]mysqlLineageColumn, len(columns))
	copy(aliased, columns)
	for idx := range aliased {
		if idx < len(aliases) && strings.TrimSpace(aliases[idx].O) != "" {
			aliased[idx].Name = aliases[idx].O
		}
	}
	return aliased
}

func mergeSetOperationColumns(left, right []mysqlLineageColumn) []mysqlLineageColumn {
	size := len(left)
	if len(right) < size {
		size = len(right)
	}
	merged := make([]mysqlLineageColumn, 0, size)
	for i := 0; i < size; i++ {
		column := left[i]
		if !sameOrigin(column.Origin, right[i].Origin) {
			column.Origin = masking.ColumnOrigin{}
		}
		merged = append(merged, column)
	}
	return merged
}

func sameOrigin(left, right masking.ColumnOrigin) bool {
	return strings.EqualFold(strings.TrimSpace(left.Database), strings.TrimSpace(right.Database)) &&
		strings.EqualFold(strings.TrimSpace(left.Schema), strings.TrimSpace(right.Schema)) &&
		strings.EqualFold(strings.TrimSpace(left.Table), strings.TrimSpace(right.Table)) &&
		strings.EqualFold(strings.TrimSpace(left.Column), strings.TrimSpace(right.Column))
}

func cloneLineageMap(src map[string][]mysqlLineageColumn) map[string][]mysqlLineageColumn {
	if len(src) == 0 {
		return make(map[string][]mysqlLineageColumn)
	}
	dst := make(map[string][]mysqlLineageColumn, len(src))
	for key, columns := range src {
		copied := make([]mysqlLineageColumn, len(columns))
		copy(copied, columns)
		dst[key] = copied
	}
	return dst
}

func restoreMySQLExpr(expr tidbast.ExprNode) string {
	if expr == nil {
		return ""
	}
	var sb strings.Builder
	ctx := tidbformat.NewRestoreCtx(tidbformat.DefaultRestoreFlags, &sb)
	if err := expr.Restore(ctx); err != nil {
		return ""
	}
	return strings.TrimSpace(sb.String())
}

func (p *mysqlInfoSchemaProvider) LoadColumns(ctx context.Context, databaseName, tableName string) ([]string, error) {
	key := strings.ToLower(strings.TrimSpace(databaseName)) + "." + strings.ToLower(strings.TrimSpace(tableName))
	if columns, ok := p.cache[key]; ok {
		return columns, nil
	}

	rows, err := p.conn.QueryContext(ctx,
		`SELECT COLUMN_NAME
		 FROM information_schema.COLUMNS
		 WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		 ORDER BY ORDINAL_POSITION`,
		databaseName, tableName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := make([]string, 0)
	for rows.Next() {
		var columnName string
		if err := rows.Scan(&columnName); err != nil {
			return nil, err
		}
		columns = append(columns, columnName)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	p.cache[key] = columns
	return columns, nil
}
