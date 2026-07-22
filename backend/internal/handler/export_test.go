package handler

import (
	"strings"
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

	queryCtx := exportQueryExecutionContextFromContext(conn, "", "", nil)
	if queryCtx.DatabaseName != "default_db" {
		t.Fatalf("queryCtx.DatabaseName = %q, want %q", queryCtx.DatabaseName, "default_db")
	}
	queryCtx = exportQueryExecutionContextFromContext(conn, "ticket_db", "ticket_schema", []model.TicketScope{
		{
			DatabaseName: &scopeDatabaseName,
			SchemaName:   &scopeSchemaName,
		},
	})
	if queryCtx.DatabaseName != "ticket_db" {
		t.Fatalf("queryCtx.DatabaseName = %q, want %q", queryCtx.DatabaseName, "ticket_db")
	}
	if queryCtx.SchemaName != "ticket_schema" {
		t.Fatalf("queryCtx.SchemaName = %q, want %q", queryCtx.SchemaName, "ticket_schema")
	}

	queryCtx = exportQueryExecutionContextFromContext(conn, "ticket_db", "", nil)
	if queryCtx.DatabaseName != "ticket_db" {
		t.Fatalf("queryCtx.DatabaseName = %q, want %q", queryCtx.DatabaseName, "ticket_db")
	}

	queryCtx = exportQueryExecutionContextFromContext(conn, "", "", []model.TicketScope{
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

func TestRequestRateLimiterAllowsOnlyFiveHitsPerMinutePerKey(t *testing.T) {
	limiter := newRequestRateLimiter(5, time.Minute)
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		if !limiter.Allow("export-download:1:user:10", now.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("Allow() denied hit %d, want allowed", i+1)
		}
	}
	if limiter.Allow("export-download:1:user:10", now.Add(30*time.Second)) {
		t.Fatal("Allow() = true on 6th hit within one minute, want false")
	}
	if !limiter.Allow("export-download:1:user:11", now.Add(30*time.Second)) {
		t.Fatal("Allow() = false for a different user key, want true")
	}
	if !limiter.Allow("export-download:1:user:10", now.Add(61*time.Second)) {
		t.Fatal("Allow() = false after window elapsed, want true")
	}
}

func TestBuildTicketNotificationBodyIncludesExportContext(t *testing.T) {
	databaseName := "analytics"
	connName := "warehouse-prod"
	containsSensitive := false
	ticket := &model.Ticket{
		ID:                42,
		TicketNo:          "TK-20260622-080000000-ABCDEF",
		TicketType:        model.TicketTypeSQLExport,
		ContainsSensitive: &containsSensitive,
		DatabaseName:      &databaseName,
	}

	body := buildTicketNotificationBody(
		ticket,
		&connName,
		"william",
		exportTicketStateLabel(model.TicketStatusPendingReview),
		"/tickets/TK-20260622-080000000-ABCDEF",
	)

	for _, part := range []string{
		"工單類型：SQL_EXPORT",
		"目前狀態：待審核",
		"提交者：william",
		"導出類型：普通數據導出",
		"數據庫實例：warehouse-prod",
		"數據庫：analytics",
		"工單連結：/tickets/TK-20260622-080000000-ABCDEF",
	} {
		if !strings.Contains(body, part) {
			t.Fatalf("body missing %q: %s", part, body)
		}
	}
	for _, removed := range []string{"待執行操作：", "說明：", "資料來源：", "資料庫："} {
		if strings.Contains(body, removed) {
			t.Fatalf("body should not include %q: %s", removed, body)
		}
	}
}

func TestBuildTicketNotificationBodyIncludesSensitiveExportType(t *testing.T) {
	containsSensitive := true
	ticket := &model.Ticket{
		ID:                42,
		TicketNo:          "TK-20260622-080000000-ABCDEF",
		TicketType:        model.TicketTypeSQLExport,
		ContainsSensitive: &containsSensitive,
	}

	body := buildTicketNotificationBody(
		ticket,
		nil,
		"william",
		exportTicketStateLabel(model.TicketStatusPendingReview),
		"",
	)

	if !strings.Contains(body, "導出類型：敏感數據導出") {
		t.Fatalf("body missing sensitive export type: %s", body)
	}
}

func TestExportPendingReviewTitle(t *testing.T) {
	got := exportPendingReviewTitle()
	want := "工單待審核"
	if got != want {
		t.Fatalf("exportPendingReviewTitle() = %q, want %q", got, want)
	}
}
