package handler

import (
	"testing"

	"github.com/dbre-maestro/maestro/internal/model"
)

func TestExportQueryExecutionContextFromScopesUsesTicketScopes(t *testing.T) {
	databaseName := "default_db"
	scopeDatabaseName := "nacos"
	scopeSchemaName := "public"

	conn := &model.DBConnection{
		DatabaseName: &databaseName,
	}

	queryCtx := exportQueryExecutionContextFromScopes(conn, nil)
	if queryCtx.DatabaseName != "default_db" {
		t.Fatalf("queryCtx.DatabaseName = %q, want %q", queryCtx.DatabaseName, "default_db")
	}
	queryCtx = exportQueryExecutionContextFromScopes(conn, []model.TicketScope{
		{
			DatabaseName: &scopeDatabaseName,
			SchemaName:   &scopeSchemaName,
		},
	})
	if queryCtx.DatabaseName != "nacos" {
		t.Fatalf("queryCtx.DatabaseName = %q, want %q", queryCtx.DatabaseName, "nacos")
	}
	if queryCtx.SchemaName != "public" {
		t.Fatalf("queryCtx.SchemaName = %q, want %q", queryCtx.SchemaName, "public")
	}
}
