package tableschema

import (
	"context"
	"database/sql"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/pool"
)

type recordingResolver struct {
	role string
	conn *model.DBConnection
}

func (r *recordingResolver) ResolveCredential(conn *model.DBConnection, role string) (*model.DBConnection, string, error) {
	r.role = role
	r.conn = conn
	resolved := *conn
	resolved.Host, resolved.Port, resolved.Username = "readonly.internal", 3306, "schema_reader"
	return &resolved, "secret", nil
}

func TestOpenReadonlyAlwaysResolvesReadonlyCredentialAndMetadataProfile(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectPing()
	resolver := &recordingResolver{}
	connection := &model.DBConnection{ID: 7, DBType: "mysql", Host: "writer.internal", Port: 3306, Username: "writer"}
	var gotProfile pool.Profile
	opened, err := openReadonly(context.Background(), resolver, connection, func(driver, dsn string, profile pool.Profile) (*sql.DB, error) {
		if driver != "mysql" {
			t.Fatalf("driver = %q", driver)
		}
		if !regexp.MustCompile(`schema_reader:secret@tcp\(readonly\.internal:3306\)`).MatchString(dsn) {
			t.Fatalf("dsn does not use resolved readonly identity: %q", dsn)
		}
		gotProfile = profile
		return db, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	if resolver.role != model.DBCredentialRoleReadonly {
		t.Fatalf("credential role = %q", resolver.role)
	}
	if gotProfile != pool.ProfileMetadata {
		t.Fatalf("profile = %q", gotProfile)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenRoleUsesReadwriteCredentialForSyncTarget(t *testing.T) {
	db, mock, _ := sqlmock.New(sqlmock.MonitorPingsOption(true))
	defer db.Close()
	mock.ExpectPing()
	resolver := &recordingResolver{}
	opened, err := openRole(context.Background(), resolver, &model.DBConnection{DBType: "mysql", Host: "writer", Port: 3306}, model.DBCredentialRoleReadwrite, func(_, _ string, _ pool.Profile) (*sql.DB, error) { return db, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	if resolver.role != model.DBCredentialRoleReadwrite {
		t.Fatalf("role = %q", resolver.role)
	}
}

func TestLoadSnapshotSortsTablesAndParsesShowCreateOptions(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("SHOW CREATE TABLE `app`.`orders`")).
		WithArgs().WillReturnRows(sqlmock.NewRows([]string{"Table", "Create Table"}).AddRow("orders", "CREATE TABLE `orders` (`id` bigint) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci"))
	mock.ExpectQuery(regexp.QuoteMeta("SHOW CREATE TABLE `app`.`users`")).
		WithArgs().WillReturnRows(sqlmock.NewRows([]string{"Table", "Create Table"}).AddRow("users", "CREATE TABLE `users` (`id` bigint) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci"))

	snapshot, err := LoadSnapshot(context.Background(), db, "app", []string{"users", "orders"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tables) != 2 || snapshot.Tables[0].Name != "orders" || snapshot.Tables[1].Name != "users" {
		t.Fatalf("snapshot order = %#v", snapshot.Tables)
	}
	if snapshot.Tables[0].Options.Engine != "InnoDB" || snapshot.Tables[0].Options.Charset != "utf8mb4" {
		t.Fatalf("options = %#v", snapshot.Tables[0].Options)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSnapshotRejectsDuplicateOrOversizedSelectionBeforeQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, tables := range [][]string{{"users", "users"}, make([]string, MaxTables+1)} {
		_, err := LoadSnapshot(context.Background(), db, "app", tables)
		stable, ok := err.(*StableError)
		if !ok || stable.Code != ErrorInvalidTableSelection {
			t.Fatalf("tables=%d error=%#v", len(tables), err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadCapabilitiesUsesServerReportedEngineAndCollationSupport(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SHOW ENGINES").WillReturnRows(sqlmock.NewRows([]string{"Engine", "Support", "Comment", "Transactions", "XA", "Savepoints"}).
		AddRow("InnoDB", "DEFAULT", "Supports transactions", "YES", "YES", "YES").
		AddRow("ARCHIVE", "NO", "Archive storage engine", "NO", "NO", "NO"))
	mock.ExpectQuery("SELECT CHARACTER_SET_NAME, COLLATION_NAME").WillReturnRows(sqlmock.NewRows([]string{"CHARACTER_SET_NAME", "COLLATION_NAME"}).
		AddRow("utf8mb4", "utf8mb4_unicode_ci"))

	capabilities, err := LoadCapabilities(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if !capabilities.Engines["innodb"] || capabilities.Engines["archive"] {
		t.Fatalf("engines = %#v", capabilities.Engines)
	}
	if !capabilities.Charsets["utf8mb4"]["utf8mb4_unicode_ci"] {
		t.Fatalf("charsets = %#v", capabilities.Charsets)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
