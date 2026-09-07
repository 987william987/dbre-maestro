package handler

import (
	"testing"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
)

func TestBuildDashboardOperationTrendFillsMissingUTCDays(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 3)

	trend := buildDashboardOperationTrend(start, end, []repository.PlatformOperationDailyCount{
		{Date: "2026-09-01", Type: string(model.TicketTypeDDL), Count: 2},
		{Date: "2026-09-01", Type: "query", Count: 9},
		{Date: "2026-09-03", Type: string(model.TicketTypeSensitiveQueryAccess), Count: 1},
	})

	if trend.Timezone != "UTC" || trend.StartDate != "2026-09-01" || trend.EndDate != "2026-09-03" {
		t.Fatalf("unexpected trend metadata: %#v", trend)
	}
	if len(trend.Points) != 3 {
		t.Fatalf("len(points) = %d, want 3", len(trend.Points))
	}
	if trend.Points[0].DDL != 2 || trend.Points[0].Query != 9 {
		t.Fatalf("first point = %#v", trend.Points[0])
	}
	if trend.Points[1] != (dashboardOperationTrendPoint{Date: "2026-09-02"}) {
		t.Fatalf("missing day was not zero-filled: %#v", trend.Points[1])
	}
	if trend.Points[2].SensitiveQueryAccess != 1 {
		t.Fatalf("last point = %#v", trend.Points[2])
	}
}

func TestBuildDashboardTicketSummaryDoesNotCountApprovedAsActive(t *testing.T) {
	summary := buildDashboardTicketSummary(&repository.TicketDashboardSummary{
		Total: 6,
		ByStatus: []repository.WorkflowDashboardCount{
			{Key: "approved", Count: 4},
			{Key: "failed", Count: 2},
		},
	})

	if summary.Active != 0 {
		t.Fatalf("Active = %d, want 0 because approved tickets can be terminal for access/export flows", summary.Active)
	}
	if summary.Failed != 2 {
		t.Fatalf("Failed = %d, want 2", summary.Failed)
	}
}

func TestBuildDashboardTicketSummaryCountsOnlyOpenWorkflowStatusesAsActive(t *testing.T) {
	summary := buildDashboardTicketSummary(&repository.TicketDashboardSummary{
		Total: 5,
		ByStatus: []repository.WorkflowDashboardCount{
			{Key: "pending_review", Count: 1},
			{Key: "pending_execution", Count: 1},
			{Key: "executing", Count: 1},
			{Key: "needs_admin_attention", Count: 1},
			{Key: "completed", Count: 1},
		},
	})

	if summary.Active != 4 {
		t.Fatalf("Active = %d, want 4", summary.Active)
	}
	if summary.Completed != 1 {
		t.Fatalf("Completed = %d, want 1", summary.Completed)
	}
}
