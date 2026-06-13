package handler

import (
	"bytes"
	"errors"
	"github.com/dbre-maestro/maestro/internal/model"
	"log/slog"
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
