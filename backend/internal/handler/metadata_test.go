package handler

import (
	"bytes"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/model"
	"log/slog"
	"regexp"
	"strings"
	"testing"
)

func TestConnectionDatabaseNameTrimsWhitespace(t *testing.T) {
	name := "  maestro  "
	conn := &model.DBConnection{DatabaseName: &name}

	if got := connectionDatabaseName(conn); got != "maestro" {
		t.Fatalf("connectionDatabaseName() = %q, want %q", got, "maestro")
	}
}

func TestBuildRedisMetadataItems(t *testing.T) {
	items := buildRedisMetadataItems()

	if len(items) != 16 {
		t.Fatalf("len(items) = %d, want 16", len(items))
	}
	if items[0].Kind != "redis_db" || items[0].Name != "0" {
		t.Fatalf("items[0] = %#v", items[0])
	}
	if items[15].Name != "15" {
		t.Fatalf("items[15].Name = %q, want %q", items[15].Name, "15")
	}
}

func TestBuildPostgresTableDefinition(t *testing.T) {
	definition := buildPostgresTableDefinition(
		"public",
		"tickets",
		[]postgresDefinitionColumn{
			{
				Name:       "id",
				DataType:   "bigint",
				IsNullable: false,
			},
			{
				Name:        "title",
				DataType:    "text",
				DefaultExpr: "'draft'::text",
				IsNullable:  true,
			},
		},
		[]postgresDefinitionConstraint{
			{
				Name:       "tickets_pkey",
				Definition: "PRIMARY KEY (id)",
			},
		},
		[]postgresDefinitionIndex{
			{
				Name:       "tickets_title_idx",
				Definition: `CREATE INDEX tickets_title_idx ON public.tickets USING btree (title)`,
			},
		},
	)

	want := "CREATE TABLE \"public\".\"tickets\" (\n" +
		"    \"id\" bigint NOT NULL,\n" +
		"    \"title\" text DEFAULT 'draft'::text,\n" +
		"    CONSTRAINT \"tickets_pkey\" PRIMARY KEY (id)\n" +
		");\n\n" +
		"CREATE INDEX tickets_title_idx ON public.tickets USING btree (title);"

	if definition != want {
		t.Fatalf("definition = %q, want %q", definition, want)
	}
}

func TestLogMetadataQueryError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	previous := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})

	logMetadataQueryError("tables", &model.DBConnection{
		ID:     42,
		Name:   "aws-sg-bot-pg-nonprod",
		DBType: "postgres",
		Host:   "db.example.internal",
	}, "rdsadmin", "public", "tickets", errors.New(`pg_hba.conf rejects connection for host "10.183.27.22"`))

	logged := buf.String()
	for _, expected := range []string{
		`"msg":"metadata query failed"`,
		`"operation":"tables"`,
		`"connection_id":42`,
		`"connection_name":"aws-sg-bot-pg-nonprod"`,
		`"database":"rdsadmin"`,
		`"schema":"public"`,
		`"table":"tickets"`,
		`pg_hba.conf rejects connection`,
	} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("log output %q does not contain %q", logged, expected)
		}
	}
}

func TestShouldSkipPostgresMetadataDatabase(t *testing.T) {
	for _, name := range []string{"rdsadmin", "RDSADMIN", " rdsadmin "} {
		if !shouldSkipPostgresMetadataDatabase(name) {
			t.Fatalf("shouldSkipPostgresMetadataDatabase(%q) = false, want true", name)
		}
	}
	if shouldSkipPostgresMetadataDatabase("postgres") {
		t.Fatal(`shouldSkipPostgresMetadataDatabase("postgres") = true, want false`)
	}
}

func TestLoadPostgresSchemaTablesIncludesPartitionedTables(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mockRows := sqlmock.NewRows([]string{"table_schema", "table_name"}).
		AddRow("public", "activities").
		AddRow("public", "activities_000")

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
			n.nspname AS table_schema,
			c.relname AS table_name
		 FROM pg_class c
		 JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = $1
		   AND c.relkind IN ('r', 'p')
		 ORDER BY c.relname`)).
		WithArgs("public").
		WillReturnRows(mockRows)

	rows, err := db.Query(`SELECT
			n.nspname AS table_schema,
			c.relname AS table_name
		 FROM pg_class c
		 JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = $1
		   AND c.relkind IN ('r', 'p')
		 ORDER BY c.relname`, "public")
	if err != nil {
		t.Fatalf("db.Query() error = %v", err)
	}
	defer rows.Close()

	items := make([]metadataItem, 0)
	for rows.Next() {
		var schemaName string
		var tableName string
		if err := rows.Scan(&schemaName, &tableName); err != nil {
			t.Fatalf("rows.Scan() error = %v", err)
		}
		items = append(items, metadataItem{
			Kind:     "table",
			Name:     tableName,
			Database: "capy_indexer",
			Schema:   schemaName,
		})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() error = %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].Name != "activities" || items[1].Name != "activities_000" {
		t.Fatalf("items = %#v", items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations not met: %v", err)
	}
}
