package handler

import (
	"testing"

	"github.com/dbre-maestro/maestro/internal/model"
)

func TestBuildLarkTicketCardActionsForPendingReview(t *testing.T) {
	ticket := &model.Ticket{ID: 7, TicketNo: "TK-7", TicketType: model.TicketTypeSQLExport}
	actions := buildLarkTicketCardActions(ticket, "ticket_pending_review")
	if len(actions) != 3 {
		t.Fatalf("actions len = %d, want 3", len(actions))
	}
	if actions[0].Action != larkTicketActionApprove || actions[1].Action != larkTicketActionReject || actions[2].Action != larkTicketActionViewDetails || actions[2].URL != "" {
		t.Fatalf("actions = %#v, want approve/reject/view", actions)
	}
}

func TestBuildLarkTicketCardActionsForPendingExecution(t *testing.T) {
	ticket := &model.Ticket{ID: 8, TicketNo: "TK-8", TicketType: model.TicketTypeDDL}
	actions := buildLarkTicketCardActions(ticket, "ticket_pending_execution")
	if len(actions) != 3 {
		t.Fatalf("actions len = %d, want 3", len(actions))
	}
	if actions[0].Action != larkTicketActionExecute || actions[1].Action != larkTicketActionReject || actions[2].Action != larkTicketActionViewDetails || actions[2].URL != "" {
		t.Fatalf("actions = %#v, want execute/reject/view", actions)
	}
}

func TestBuildLarkTicketCardActionsForTerminalNotificationOnlyView(t *testing.T) {
	ticket := &model.Ticket{ID: 9, TicketNo: "TK-9", TicketType: model.TicketTypeDML}
	actions := buildLarkTicketCardActions(ticket, "ticket_executed")
	if len(actions) != 1 || actions[0].URL != "" || actions[0].Action != larkTicketActionViewDetails {
		t.Fatalf("actions = %#v, want view only", actions)
	}
}

func TestAppendLarkTicketLinkFieldRendersFullURL(t *testing.T) {
	fields := appendLarkTicketLinkField(nil, "https://dbre.example.test/tickets/TK-9")
	if len(fields) != 1 || fields[0].Label != "工單連結" || fields[0].Value != "[https://dbre.example.test/tickets/TK-9](https://dbre.example.test/tickets/TK-9)" {
		t.Fatalf("appendLarkTicketLinkField() = %#v", fields)
	}
}
