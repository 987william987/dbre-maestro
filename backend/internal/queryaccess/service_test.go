package queryaccess

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/jmoiron/sqlx"
)

func TestExtractObjectRefsMySQLJoinAndCTE(t *testing.T) {
	conn := &model.DBConnection{
		ID:     7,
		DBType: "mysql",
		DatabaseName: func() *string {
			v := "analytics"
			return &v
		}(),
	}

	refs, err := ExtractObjectRefs(conn, `
WITH active_users AS (
  SELECT id FROM users WHERE deleted_at IS NULL
)
SELECT u.id, o.id
FROM active_users u
JOIN orders o ON o.user_id = u.id;
`, CheckContext{})
	if err != nil {
		t.Fatalf("ExtractObjectRefs() error = %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("len(refs) = %d, want 2, refs=%#v", len(refs), refs)
	}
	assertHasRef(t, refs, ObjectRef{ConnectionID: 7, DatabaseName: "analytics", TableName: "users"})
	assertHasRef(t, refs, ObjectRef{ConnectionID: 7, DatabaseName: "analytics", TableName: "orders"})
}

func TestExtractObjectRefsPostgresCTEAndSubquery(t *testing.T) {
	conn := &model.DBConnection{
		ID:     9,
		DBType: "postgres",
		DatabaseName: func() *string {
			v := "appdb"
			return &v
		}(),
	}

	refs, err := ExtractObjectRefs(conn, `
WITH t AS (
  SELECT id FROM public.users
)
SELECT *
FROM (SELECT * FROM public.orders) o
JOIN t ON t.id = o.user_id;
`, CheckContext{SchemaName: "public"})
	if err != nil {
		t.Fatalf("ExtractObjectRefs() error = %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("len(refs) = %d, want 2, refs=%#v", len(refs), refs)
	}
	assertHasRef(t, refs, ObjectRef{ConnectionID: 9, DatabaseName: "appdb", SchemaName: "public", TableName: "users"})
	assertHasRef(t, refs, ObjectRef{ConnectionID: 9, DatabaseName: "appdb", SchemaName: "public", TableName: "orders"})
}

func TestMatchesAnyGrantSupportsDatabaseAndTableScopes(t *testing.T) {
	ref := ObjectRef{ConnectionID: 1, DatabaseName: "analytics", TableName: "users"}
	grants := []model.QueryAccessGrant{
		{
			ConnectionID: 1,
			DatabaseName: func() *string {
				v := "analytics"
				return &v
			}(),
			TableName: nil,
		},
	}
	if !matchesAnyGrant(ref, grants) {
		t.Fatalf("expected database-level grant to match")
	}

	table := "users"
	grants = []model.QueryAccessGrant{
		{
			ConnectionID: 1,
			DatabaseName: func() *string {
				v := "analytics"
				return &v
			}(),
			TableName: &table,
		},
	}
	if !matchesAnyGrant(ref, grants) {
		t.Fatalf("expected table-level grant to match")
	}
}

func TestCheckSQLAllowsProtectedAdminWithoutQueryAccessGrant(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	now := time.Now().UTC()
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(uint64(1)).
		WillReturnRows(userRows().AddRow(uint64(1), "admin", "admin@example.com", "", "hash", true, true, true, now, now))

	sqlxDB := sqlx.NewDb(db, "mysql")
	service := NewService(repository.NewQueryAccessRepo(sqlxDB), repository.NewUserRepo(sqlxDB))
	conn := mysqlConnection(7, "analytics")

	err = service.CheckSQL(context.Background(), 1, conn, "SELECT * FROM users", CheckContext{})
	if err != nil {
		t.Fatalf("CheckSQL() error = %v, want nil for protected admin", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestCheckSQLAllowsAllPermissionsAuthGroupWithoutQueryAccessGrant(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	now := time.Now().UTC()
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(uint64(2)).
		WillReturnRows(userRows().AddRow(uint64(2), "dba", "dba@example.com", "", "hash", true, false, true, now, now))
	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs(uint64(2), sqlmock.AnyArg(), uint64(2), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	sqlxDB := sqlx.NewDb(db, "mysql")
	service := NewService(repository.NewQueryAccessRepo(sqlxDB), repository.NewUserRepo(sqlxDB))
	conn := mysqlConnection(7, "analytics")

	err = service.CheckSQL(context.Background(), 2, conn, "SELECT * FROM users", CheckContext{})
	if err != nil {
		t.Fatalf("CheckSQL() error = %v, want nil for all-permissions auth group", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func assertHasRef(t *testing.T, refs []ObjectRef, expected ObjectRef) {
	t.Helper()
	for _, ref := range refs {
		if ref.ConnectionID == expected.ConnectionID &&
			ref.DatabaseName == expected.DatabaseName &&
			ref.SchemaName == expected.SchemaName &&
			ref.TableName == expected.TableName {
			return
		}
	}
	t.Fatalf("expected ref %#v not found in %#v", expected, refs)
}

func userRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"username",
		"email",
		"lark_recipient",
		"password",
		"is_setup",
		"is_protected",
		"is_active",
		"created_at",
		"updated_at",
	})
}

func mysqlConnection(id uint64, databaseName string) *model.DBConnection {
	return &model.DBConnection{
		ID:     id,
		DBType: "mysql",
		DatabaseName: func() *string {
			v := databaseName
			return &v
		}(),
	}
}
