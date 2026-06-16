package handler

import (
	"context"
	"testing"

	"github.com/dbre-maestro/maestro/internal/masking"
	"github.com/dbre-maestro/maestro/internal/sqlparse"
)

type fakeMySQLSchemaProvider map[string][]string

func (f fakeMySQLSchemaProvider) LoadColumns(_ context.Context, databaseName, tableName string) ([]string, error) {
	return append([]string(nil), f[databaseName+"."+tableName]...), nil
}

func TestMySQLLineageResolverResolvesAliasedColumn(t *testing.T) {
	parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, "SELECT u.email AS customer_email FROM users u")
	if err != nil {
		t.Fatalf("ParseSQL() error = %v", err)
	}

	resolver := &mysqlLineageResolver{
		defaultDatabase: "analytics",
		provider: fakeMySQLSchemaProvider{
			"analytics.users": {"id", "email"},
		},
	}

	columns, err := resolver.resolveStatementColumns(context.Background(), parsed.Statements[0].AST)
	if err != nil {
		t.Fatalf("resolveStatementColumns() error = %v", err)
	}
	if len(columns) != 1 {
		t.Fatalf("len(columns) = %d, want 1", len(columns))
	}
	if columns[0].Origin != (masking.ColumnOrigin{Database: "analytics", Table: "users", Column: "email"}) {
		t.Fatalf("origin = %#v", columns[0].Origin)
	}
}

func TestMySQLLineageResolverResolvesCTEColumn(t *testing.T) {
	parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, "WITH cte AS (SELECT email FROM users) SELECT email FROM cte")
	if err != nil {
		t.Fatalf("ParseSQL() error = %v", err)
	}

	resolver := &mysqlLineageResolver{
		defaultDatabase: "analytics",
		provider: fakeMySQLSchemaProvider{
			"analytics.users": {"id", "email"},
		},
	}

	columns, err := resolver.resolveStatementColumns(context.Background(), parsed.Statements[0].AST)
	if err != nil {
		t.Fatalf("resolveStatementColumns() error = %v", err)
	}
	if len(columns) != 1 {
		t.Fatalf("len(columns) = %d, want 1", len(columns))
	}
	if columns[0].Origin != (masking.ColumnOrigin{Database: "analytics", Table: "users", Column: "email"}) {
		t.Fatalf("origin = %#v", columns[0].Origin)
	}
}

func TestMySQLLineageResolverExpandsWildcard(t *testing.T) {
	parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, "SELECT u.* FROM users u")
	if err != nil {
		t.Fatalf("ParseSQL() error = %v", err)
	}

	resolver := &mysqlLineageResolver{
		defaultDatabase: "analytics",
		provider: fakeMySQLSchemaProvider{
			"analytics.users": {"id", "email"},
		},
	}

	columns, err := resolver.resolveStatementColumns(context.Background(), parsed.Statements[0].AST)
	if err != nil {
		t.Fatalf("resolveStatementColumns() error = %v", err)
	}
	if len(columns) != 2 {
		t.Fatalf("len(columns) = %d, want 2", len(columns))
	}
	if columns[0].Origin.Column != "id" || columns[1].Origin.Column != "email" {
		t.Fatalf("origins = %#v", columns)
	}
}

func TestMySQLLineageResolverResolvesSingleSourceExpression(t *testing.T) {
	parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, "SELECT LOWER(email) AS email_lower FROM users")
	if err != nil {
		t.Fatalf("ParseSQL() error = %v", err)
	}

	resolver := &mysqlLineageResolver{
		defaultDatabase: "analytics",
		provider: fakeMySQLSchemaProvider{
			"analytics.users": {"id", "email"},
		},
	}

	columns, err := resolver.resolveStatementColumns(context.Background(), parsed.Statements[0].AST)
	if err != nil {
		t.Fatalf("resolveStatementColumns() error = %v", err)
	}
	if len(columns) != 1 {
		t.Fatalf("len(columns) = %d, want 1", len(columns))
	}
	if columns[0].Origin != (masking.ColumnOrigin{Database: "analytics", Table: "users", Column: "email"}) {
		t.Fatalf("origin = %#v", columns[0].Origin)
	}
	if len(columns[0].Dependencies) != 1 {
		t.Fatalf("dependencies = %#v", columns[0].Dependencies)
	}
}

func TestMySQLLineageResolverResolvesNestedDerivedTable(t *testing.T) {
	parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, "SELECT outer_q.email FROM (SELECT inner_q.email FROM (SELECT u.email FROM users u) inner_q) outer_q")
	if err != nil {
		t.Fatalf("ParseSQL() error = %v", err)
	}

	resolver := &mysqlLineageResolver{
		defaultDatabase: "analytics",
		provider: fakeMySQLSchemaProvider{
			"analytics.users": {"id", "email"},
		},
	}

	columns, err := resolver.resolveStatementColumns(context.Background(), parsed.Statements[0].AST)
	if err != nil {
		t.Fatalf("resolveStatementColumns() error = %v", err)
	}
	if len(columns) != 1 {
		t.Fatalf("len(columns) = %d, want 1", len(columns))
	}
	if columns[0].Origin != (masking.ColumnOrigin{Database: "analytics", Table: "users", Column: "email"}) {
		t.Fatalf("origin = %#v", columns[0].Origin)
	}
}

func TestMySQLLineageResolverKeepsMultiSourceDependencies(t *testing.T) {
	parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, "SELECT CONCAT(u.first_name, u.last_name) AS full_name FROM users u")
	if err != nil {
		t.Fatalf("ParseSQL() error = %v", err)
	}

	resolver := &mysqlLineageResolver{
		defaultDatabase: "analytics",
		provider: fakeMySQLSchemaProvider{
			"analytics.users": {"id", "first_name", "last_name"},
		},
	}

	columns, err := resolver.resolveStatementColumns(context.Background(), parsed.Statements[0].AST)
	if err != nil {
		t.Fatalf("resolveStatementColumns() error = %v", err)
	}
	if len(columns) != 1 {
		t.Fatalf("len(columns) = %d, want 1", len(columns))
	}
	if columns[0].Origin != (masking.ColumnOrigin{}) {
		t.Fatalf("origin = %#v, want empty for multi-source expression", columns[0].Origin)
	}
	if len(columns[0].Dependencies) != 2 {
		t.Fatalf("dependencies = %#v", columns[0].Dependencies)
	}
}
