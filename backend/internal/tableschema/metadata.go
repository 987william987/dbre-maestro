package tableschema

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

var systemDatabases = map[string]struct{}{
	"information_schema": {}, "mysql": {}, "performance_schema": {}, "sys": {},
}

type TableMetadata struct {
	Name    string       `json:"name"`
	Options TableOptions `json:"options"`
}

type Dependency struct {
	Database           string `json:"database"`
	Table              string `json:"table"`
	ReferencedDatabase string `json:"referenced_database"`
	ReferencedTable    string `json:"referenced_table"`
}

type DependencyAnalysis struct {
	Order    []string     `json:"order"`
	External []Dependency `json:"external"`
	Cycles   [][]string   `json:"cycles"`
}

func ListDatabases(ctx context.Context, db Queryer) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT SCHEMA_NAME FROM information_schema.SCHEMATA ORDER BY SCHEMA_NAME`)
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	defer rows.Close()
	items := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan database: %w", err)
		}
		if _, excluded := systemDatabases[strings.ToLower(name)]; !excluded {
			items = append(items, name)
		}
	}
	return items, rows.Err()
}

func ListTables(ctx context.Context, db Queryer, database string) ([]TableMetadata, error) {
	if err := validateIdentifier(database); err != nil {
		return nil, &StableError{Code: ErrorInvalidTableSelection, Message: "invalid source database"}
	}
	rows, err := db.QueryContext(ctx, `SELECT t.TABLE_NAME, COALESCE(t.ENGINE, ''),
COALESCE(c.CHARACTER_SET_NAME, ''), COALESCE(t.TABLE_COLLATION, ''),
COALESCE(t.ROW_FORMAT, ''), t.AUTO_INCREMENT
FROM information_schema.TABLES t
LEFT JOIN information_schema.COLLATIONS c ON c.COLLATION_NAME = t.TABLE_COLLATION
WHERE t.TABLE_SCHEMA = ? AND t.TABLE_TYPE = 'BASE TABLE'
ORDER BY t.TABLE_NAME`, database)
	if err != nil {
		return nil, fmt.Errorf("list base tables: %w", err)
	}
	defer rows.Close()
	items := []TableMetadata{}
	for rows.Next() {
		var item TableMetadata
		var autoIncrement sql.NullString
		if err := rows.Scan(&item.Name, &item.Options.Engine, &item.Options.Charset, &item.Options.Collation, &item.Options.RowFormat, &autoIncrement); err != nil {
			return nil, fmt.Errorf("scan base table: %w", err)
		}
		if autoIncrement.Valid {
			item.Options.AutoIncrement = autoIncrement.String
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func LoadDependencies(ctx context.Context, db Queryer, database string, tables []string) ([]Dependency, error) {
	if err := validateIdentifier(database); err != nil {
		return nil, &StableError{Code: ErrorInvalidTableSelection, Message: "invalid source database"}
	}
	names, err := normalizeTableNames(tables, true)
	if err != nil || len(names) == 0 {
		return []Dependency{}, err
	}
	args := make([]any, 0, len(names)+1)
	args = append(args, database)
	placeholders := make([]string, len(names))
	for i, name := range names {
		placeholders[i] = "?"
		args = append(args, name)
	}
	query := dependencyQuery(` AND TABLE_NAME IN (` + strings.Join(placeholders, ",") + `)`)
	return queryDependencies(ctx, db, query, args...)
}

func ListDependencies(ctx context.Context, db Queryer, database string) ([]Dependency, error) {
	if err := validateIdentifier(database); err != nil {
		return nil, &StableError{Code: ErrorInvalidTableSelection, Message: "invalid source database"}
	}
	return queryDependencies(ctx, db, dependencyQuery(""), database)
}

func dependencyQuery(tableFilter string) string {
	return `SELECT DISTINCT TABLE_SCHEMA, TABLE_NAME, REFERENCED_TABLE_SCHEMA, REFERENCED_TABLE_NAME
FROM information_schema.KEY_COLUMN_USAGE
WHERE TABLE_SCHEMA = ?` + tableFilter + `
AND REFERENCED_TABLE_NAME IS NOT NULL
ORDER BY TABLE_SCHEMA, TABLE_NAME, REFERENCED_TABLE_SCHEMA, REFERENCED_TABLE_NAME`
}

func queryDependencies(ctx context.Context, db Queryer, query string, args ...any) ([]Dependency, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load foreign key dependencies: %w", err)
	}
	defer rows.Close()
	items := []Dependency{}
	for rows.Next() {
		var item Dependency
		if err := rows.Scan(&item.Database, &item.Table, &item.ReferencedDatabase, &item.ReferencedTable); err != nil {
			return nil, fmt.Errorf("scan foreign key dependency: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func AnalyzeDependencies(database string, tables []string, dependencies []Dependency) (DependencyAnalysis, error) {
	names, err := normalizeTableNames(tables, false)
	if err != nil {
		return DependencyAnalysis{}, err
	}
	result := DependencyAnalysis{Order: []string{}, External: []Dependency{}, Cycles: [][]string{}}
	selected := make(map[string]struct{}, len(names))
	edges := make(map[string]map[string]struct{}, len(names))
	indegree := make(map[string]int, len(names))
	for _, name := range names {
		selected[name] = struct{}{}
		edges[name] = map[string]struct{}{}
	}
	for _, dependency := range dependencies {
		if _, childSelected := selected[dependency.Table]; !childSelected {
			continue
		}
		_, parentSelected := selected[dependency.ReferencedTable]
		if dependency.ReferencedDatabase != database || !parentSelected {
			result.External = append(result.External, dependency)
			continue
		}
		if _, exists := edges[dependency.ReferencedTable][dependency.Table]; !exists {
			edges[dependency.ReferencedTable][dependency.Table] = struct{}{}
			indegree[dependency.Table]++
		}
	}
	sort.Slice(result.External, func(i, j int) bool { return dependencyKey(result.External[i]) < dependencyKey(result.External[j]) })
	remaining := append([]string(nil), names...)
	for len(remaining) > 0 {
		pick := -1
		for i, name := range remaining {
			if indegree[name] == 0 {
				pick = i
				break
			}
		}
		if pick < 0 {
			break
		}
		name := remaining[pick]
		remaining = append(remaining[:pick], remaining[pick+1:]...)
		result.Order = append(result.Order, name)
		for child := range edges[name] {
			indegree[child]--
		}
	}
	result.Cycles = stronglyConnectedCycles(names, edges)
	return result, nil
}

func normalizeTableNames(tables []string, allowEmpty bool) ([]string, error) {
	if (!allowEmpty && len(tables) == 0) || len(tables) > MaxTables {
		return nil, &StableError{Code: ErrorInvalidTableSelection, Message: fmt.Sprintf("select between 1 and %d tables", MaxTables)}
	}
	names := append([]string(nil), tables...)
	sort.Strings(names)
	for i, name := range names {
		if validateIdentifier(name) != nil || (i > 0 && name == names[i-1]) {
			return nil, &StableError{Code: ErrorInvalidTableSelection, Message: "table names must be unique valid MySQL identifiers"}
		}
	}
	return names, nil
}

func dependencyKey(item Dependency) string {
	return item.Database + "\x00" + item.Table + "\x00" + item.ReferencedDatabase + "\x00" + item.ReferencedTable
}

func stronglyConnectedCycles(nodes []string, edges map[string]map[string]struct{}) [][]string {
	index := 0
	indices, low := map[string]int{}, map[string]int{}
	onStack := map[string]bool{}
	stack := []string{}
	cycles := [][]string{}
	var visit func(string)
	visit = func(node string) {
		indices[node], low[node] = index, index
		index++
		stack = append(stack, node)
		onStack[node] = true
		children := make([]string, 0, len(edges[node]))
		for child := range edges[node] {
			children = append(children, child)
		}
		sort.Strings(children)
		for _, child := range children {
			if _, seen := indices[child]; !seen {
				visit(child)
				if low[child] < low[node] {
					low[node] = low[child]
				}
			} else if onStack[child] && indices[child] < low[node] {
				low[node] = indices[child]
			}
		}
		if low[node] != indices[node] {
			return
		}
		component := []string{}
		for {
			last := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[last] = false
			component = append(component, last)
			if last == node {
				break
			}
		}
		sort.Strings(component)
		if len(component) > 1 {
			cycles = append(cycles, component)
		} else if _, self := edges[node][node]; self {
			cycles = append(cycles, component)
		}
	}
	for _, node := range nodes {
		if _, seen := indices[node]; !seen {
			visit(node)
		}
	}
	sort.Slice(cycles, func(i, j int) bool { return strings.Join(cycles[i], "\x00") < strings.Join(cycles[j], "\x00") })
	return cycles
}
