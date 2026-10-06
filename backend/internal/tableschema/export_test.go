package tableschema

import (
	"context"
	"errors"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func exportCapabilities() Capabilities {
	return Capabilities{Engines: map[string]bool{"innodb": true}, Charsets: map[string]map[string]bool{"utf8mb4": {"utf8mb4_unicode_ci": true}}}
}

func TestAssembleExportUsesDependencyOrderAndWarnsAboutExternalReferences(t *testing.T) {
	snapshot := &Snapshot{Database: "app", Tables: []TableSnapshot{
		{Name: "child", CreateSQL: "CREATE TABLE `child` (`id` bigint) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci", Options: TableOptions{Engine: "InnoDB", Charset: "utf8mb4", Collation: "utf8mb4_unicode_ci"}},
		{Name: "parent", CreateSQL: "CREATE TABLE `parent` (`id` bigint) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci", Options: TableOptions{Engine: "InnoDB", Charset: "utf8mb4", Collation: "utf8mb4_unicode_ci"}},
	}}
	analysis := DependencyAnalysis{Order: []string{"parent", "child"}, External: []Dependency{{Database: "app", Table: "child", ReferencedDatabase: "audit", ReferencedTable: "actor"}}, Cycles: [][]string{}}
	result, err := assembleExport(context.Background(), snapshot, "app", analysis, TransformationConfig{}, exportCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	parentAt, childAt := strings.Index(result.Script, "`app`.`parent`"), strings.Index(result.Script, "`app`.`child`")
	if parentAt < 0 || childAt <= parentAt || len(result.Warnings) != 1 || len(result.ExternalDependencies) != 1 {
		t.Fatalf("unexpected export: %#v, script=%q", result, result.Script)
	}
}

func TestBuildExportRejectsForeignKeyCycleBeforeCapabilityQueries(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ddl := func(name string) string {
		return "CREATE TABLE `" + name + "` (`id` bigint) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci"
	}
	mock.ExpectQuery("SHOW CREATE TABLE `app`.`a`").WillReturnRows(sqlmock.NewRows([]string{"Table", "Create Table"}).AddRow("a", ddl("a")))
	mock.ExpectQuery("SHOW CREATE TABLE `app`.`b`").WillReturnRows(sqlmock.NewRows([]string{"Table", "Create Table"}).AddRow("b", ddl("b")))
	mock.ExpectQuery("SELECT DISTINCT TABLE_SCHEMA").WithArgs("app", "a", "b").WillReturnRows(sqlmock.NewRows([]string{"TABLE_SCHEMA", "TABLE_NAME", "REFERENCED_TABLE_SCHEMA", "REFERENCED_TABLE_NAME"}).AddRow("app", "a", "app", "b").AddRow("app", "b", "app", "a"))
	_, err = BuildExport(context.Background(), db, ExportRequest{Source: SourceSelection{Database: "app", Tables: []string{"b", "a"}}})
	var stable *StableError
	if !errors.As(err, &stable) || stable.Code != ErrorForeignKeyCycle {
		t.Fatalf("error = %v, want foreign_key_cycle", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildExportHonorsCancelledRequestBeforeSourceRead(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = BuildExport(ctx, db, ExportRequest{Source: SourceSelection{Database: "app", Tables: []string{"orders"}}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSnapshotRejectsOversizedDDL(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	oversized := "CREATE TABLE `orders` (`value` text COMMENT '" + strings.Repeat("x", MaxTableDDLBytes) + "')"
	mock.ExpectQuery("SHOW CREATE TABLE `app`.`orders`").WillReturnRows(sqlmock.NewRows([]string{"Table", "Create Table"}).AddRow("orders", oversized))
	_, err = LoadSnapshot(context.Background(), db, "app", []string{"orders"})
	var stable *StableError
	if !errors.As(err, &stable) || stable.Code != ErrorExportTooLarge {
		t.Fatalf("error = %v, want export_too_large", err)
	}
}
