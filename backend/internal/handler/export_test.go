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

func TestRequestRateLimiterAllowsOnlyThreeHitsPerMinute(t *testing.T) {
	limiter := newRequestRateLimiter(3, time.Minute)
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

func TestBuildTicketNotificationBodyIncludesExportContext(t *testing.T) {
	databaseName := "analytics"
	description := "submitter already sent the ticket, waiting for reviewer action."
	connName := "warehouse-prod"
	ticket := &model.Ticket{
		ID:           42,
		DatabaseName: &databaseName,
	}

	body := buildTicketNotificationBody(
		ticket,
		&connName,
		exportTicketStateLabel(model.TicketStatusPendingReview),
		"請審核是否通過此工單",
		description,
		"/tickets/42",
	)

	for _, part := range []string{
		"目前狀態：待審核",
		"待執行操作：請審核是否通過此工單",
		"資料來源：warehouse-prod",
		"資料庫：analytics",
		"說明：submitter already sent the ticket, waiting for reviewer action.",
		"工單連結：/tickets/42",
	} {
		if !strings.Contains(body, part) {
			t.Fatalf("body missing %q: %s", part, body)
		}
	}
}

func TestExportPendingReviewTitleIncludesTicketNumber(t *testing.T) {
	got := exportPendingReviewTitle("TK-20260616-151459000-038A8E")
	want := "[工單待審核] 工單 TK-20260616-151459000-038A8E"
	if got != want {
		t.Fatalf("exportPendingReviewTitle() = %q, want %q", got, want)
	}
}
