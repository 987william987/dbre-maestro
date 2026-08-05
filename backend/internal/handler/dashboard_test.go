package handler

import (
	"testing"

	"github.com/dbre-maestro/maestro/internal/repository"
)

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
