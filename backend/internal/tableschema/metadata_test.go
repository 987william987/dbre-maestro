package tableschema

import (
	"context"
	"reflect"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestListDatabasesExcludesSystemSchemas(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("SELECT SCHEMA_NAME").WillReturnRows(sqlmock.NewRows([]string{"SCHEMA_NAME"}).AddRow("app").AddRow("information_schema").AddRow("mysql").AddRow("performance_schema").AddRow("sys"))
	got, err := ListDatabases(context.Background(), db)
	if err != nil || !reflect.DeepEqual(got, []string{"app"}) {
		t.Fatalf("got %#v, err %v", got, err)
	}
}

func TestListTablesReturnsOnlyQueryResultsAndOptions(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("FROM information_schema.TABLES").WithArgs("app").WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME", "ENGINE", "CHARACTER_SET_NAME", "TABLE_COLLATION", "ROW_FORMAT", "AUTO_INCREMENT"}).AddRow("orders", "InnoDB", "utf8mb4", "utf8mb4_0900_ai_ci", "Dynamic", int64(42)))
	got, err := ListTables(context.Background(), db, "app")
	if err != nil || len(got) != 1 || got[0].Name != "orders" || got[0].Options.AutoIncrement != "42" {
		t.Fatalf("got %#v, err %v", got, err)
	}
}

func TestLoadDependenciesEmptySelectionDoesNotQuery(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	got, err := LoadDependencies(context.Background(), db, "app", nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %#v, err %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAnalyzeDependenciesDeterministicDiamondAndExternal(t *testing.T) {
	deps := []Dependency{
		{Database: "app", Table: "left", ReferencedDatabase: "app", ReferencedTable: "root"},
		{Database: "app", Table: "right", ReferencedDatabase: "app", ReferencedTable: "root"},
		{Database: "app", Table: "leaf", ReferencedDatabase: "app", ReferencedTable: "left"},
		{Database: "app", Table: "leaf", ReferencedDatabase: "app", ReferencedTable: "right"},
		{Database: "app", Table: "leaf", ReferencedDatabase: "audit", ReferencedTable: "actors"},
	}
	got, err := AnalyzeDependencies("app", []string{"right", "leaf", "root", "left"}, deps)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"root", "left", "right", "leaf"}
	if !reflect.DeepEqual(got.Order, want) || len(got.External) != 1 || len(got.Cycles) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestAnalyzeDependenciesSelectionBounds(t *testing.T) {
	if _, err := AnalyzeDependencies("app", nil, nil); err == nil {
		t.Fatal("empty table selection must be rejected before preview work")
	}
	got, err := AnalyzeDependencies("app", []string{"orders"}, nil)
	if err != nil || !reflect.DeepEqual(got.Order, []string{"orders"}) {
		t.Fatalf("single table analysis = %#v, err %v", got, err)
	}
}

func TestAnalyzeDependenciesReportsOnlyActualCycleMembers(t *testing.T) {
	deps := []Dependency{
		{Database: "app", Table: "a", ReferencedDatabase: "app", ReferencedTable: "b"},
		{Database: "app", Table: "b", ReferencedDatabase: "app", ReferencedTable: "a"},
		{Database: "app", Table: "downstream", ReferencedDatabase: "app", ReferencedTable: "a"},
	}
	got, err := AnalyzeDependencies("app", []string{"downstream", "b", "a"}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Cycles, [][]string{{"a", "b"}}) {
		t.Fatalf("cycles = %#v", got.Cycles)
	}
}

func TestAnalyzeDependenciesSelfCycleAndUnselectedParent(t *testing.T) {
	deps := []Dependency{
		{Database: "app", Table: "a", ReferencedDatabase: "app", ReferencedTable: "a"},
		{Database: "app", Table: "a", ReferencedDatabase: "app", ReferencedTable: "outside"},
	}
	got, err := AnalyzeDependencies("app", []string{"a"}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Cycles, [][]string{{"a"}}) || len(got.External) != 1 {
		t.Fatalf("got %#v", got)
	}
}
