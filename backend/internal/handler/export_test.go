package handler

import (
	"testing"
	"time"

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

func TestExportDownloadRateLimiterAllowsOnlyThreeHitsPerMinute(t *testing.T) {
	limiter := newExportDownloadRateLimiter(3, time.Minute)
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 3; i++ {
		if !limiter.Allow("token-1", now.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("Allow() denied hit %d, want allowed", i+1)
		}
	}
	if limiter.Allow("token-1", now.Add(30*time.Second)) {
		t.Fatal("Allow() = true on 4th hit within one minute, want false")
	}
	if !limiter.Allow("token-1", now.Add(61*time.Second)) {
		t.Fatal("Allow() = false after window elapsed, want true")
	}
}
