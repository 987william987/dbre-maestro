package queryaccess

import (
	"testing"

	"github.com/dbre-maestro/maestro/internal/model"
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
