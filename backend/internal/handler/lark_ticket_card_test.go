package handler

import (
	"strings"
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

func TestBuildLarkTicketDetailCardKeepsCurrentStatusActions(t *testing.T) {
	ticket := &model.Ticket{ID: 10, TicketNo: "TK-10", TicketType: model.TicketTypeDDL, Status: model.TicketStatusPendingReview}
	card := buildLarkTicketDetailCard(t.Context(), nil, nil, "https://dbre.example.test", ticket, "待審核", larkTicketCardStageReview, false)
	if card == nil {
		t.Fatal("detail card = nil")
	}
	if len(card.Actions) != 3 || card.Actions[0].Action != larkTicketActionApprove || card.Actions[1].Action != larkTicketActionReject || card.Actions[2].Action != larkTicketActionHideDetails || card.Actions[2].Text != "收起詳情" {
		t.Fatalf("detail card actions = %#v, want approve/reject/hide", card.Actions)
	}
}

func TestBuildLarkTicketDetailCardForTerminalStatusOnlyShowsView(t *testing.T) {
	ticket := &model.Ticket{ID: 11, TicketNo: "TK-11", TicketType: model.TicketTypeDDL, Status: model.TicketStatusCompleted}
	card := buildLarkTicketDetailCard(t.Context(), nil, nil, "https://dbre.example.test", ticket, "已完成", larkTicketCardStageResult, true)
	if card == nil {
		t.Fatal("detail card = nil")
	}
	if len(card.Actions) != 1 || card.Actions[0].Action != larkTicketActionHideDetails {
		t.Fatalf("detail card actions = %#v, want hide only", card.Actions)
	}
}

func TestBuildLarkTicketDetailCardOmitsStatementStatusBlock(t *testing.T) {
	ticket := &model.Ticket{
		ID:         16,
		TicketNo:   "TK-16",
		TicketType: model.TicketTypeDDL,
		Status:     model.TicketStatusCompleted,
		SQLContent: "ALTER TABLE test_a ADD COLUMN note VARCHAR(255);",
	}
	card := buildLarkTicketDetailCard(t.Context(), nil, nil, "https://dbre.example.test", ticket, "已完成", larkTicketCardStageResult, true)
	if card == nil {
		t.Fatal("detail card = nil")
	}
	content := strings.Join(card.MarkdownBlocks, "\n")
	if strings.Contains(content, "語句狀態") {
		t.Fatalf("detail card markdown = %q, want no statement status block", content)
	}
	if !strings.Contains(content, "**SQL**") {
		t.Fatalf("detail card markdown = %q, want SQL block", content)
	}
}

func TestBuildLarkTicketSummaryCardRestoresViewDetailsAction(t *testing.T) {
	ticket := &model.Ticket{ID: 12, TicketNo: "TK-12", TicketType: model.TicketTypeDDL, Status: model.TicketStatusPendingReview}
	card := buildLarkTicketSummaryCard(t.Context(), nil, nil, "https://dbre.example.test", ticket, "待審核", larkTicketCardStageReview, false)
	if card == nil {
		t.Fatal("summary card = nil")
	}
	if card.Title != "工單待審核" {
		t.Fatalf("summary card title = %q, want 工單待審核", card.Title)
	}
	if len(card.Actions) != 3 || card.Actions[0].Action != larkTicketActionApprove || card.Actions[1].Action != larkTicketActionReject || card.Actions[2].Action != larkTicketActionViewDetails || card.Actions[2].Text != "查看詳情" {
		t.Fatalf("summary card actions = %#v, want approve/reject/view", card.Actions)
	}
}

func TestBuildLarkTicketSummaryCardKeepsHandledReviewContext(t *testing.T) {
	ticket := &model.Ticket{ID: 15, TicketNo: "TK-15", TicketType: model.TicketTypeDDL, Status: model.TicketStatusPendingExecution}
	card := buildLarkTicketSummaryCard(t.Context(), nil, nil, "https://dbre.example.test", ticket, "待執行", larkTicketCardStageReview, true)
	if card == nil {
		t.Fatal("summary card = nil")
	}
	if card.Title != "審批已完成" {
		t.Fatalf("summary card title = %q, want 審批已完成", card.Title)
	}
	if len(card.Actions) != 1 || card.Actions[0].Action != larkTicketActionViewDetails {
		t.Fatalf("summary card actions = %#v, want view only", card.Actions)
	}
}

func TestLarkTicketCardContextMarksStaleReviewCardHandled(t *testing.T) {
	ticket := &model.Ticket{Status: model.TicketStatusPendingExecution}
	if !larkTicketCardContextHandled(ticket, larkTicketCardStageReview, false) {
		t.Fatal("stale review card should be handled after ticket leaves pending review")
	}
}

func TestBuildLarkTicketDetailCardKeepsOriginalReviewStageAfterTicketMovesForward(t *testing.T) {
	ticket := &model.Ticket{ID: 13, TicketNo: "TK-13", TicketType: model.TicketTypeDDL, Status: model.TicketStatusPendingExecution}
	card := buildLarkTicketDetailCard(t.Context(), nil, nil, "https://dbre.example.test", ticket, "待執行", larkTicketCardStageReview, false)
	if card == nil {
		t.Fatal("detail card = nil")
	}
	if len(card.Actions) != 3 || card.Actions[0].Action != larkTicketActionApprove || card.Actions[1].Action != larkTicketActionReject || card.Actions[2].Action != larkTicketActionHideDetails {
		t.Fatalf("detail card actions = %#v, want original review-stage actions", card.Actions)
	}
}

func TestBuildLarkTicketDetailCardHandledOnlyShowsHide(t *testing.T) {
	ticket := &model.Ticket{ID: 14, TicketNo: "TK-14", TicketType: model.TicketTypeDDL, Status: model.TicketStatusPendingExecution}
	card := buildLarkTicketDetailCard(t.Context(), nil, nil, "https://dbre.example.test", ticket, "待執行", larkTicketCardStageReview, true)
	if card == nil {
		t.Fatal("detail card = nil")
	}
	if len(card.Actions) != 1 || card.Actions[0].Action != larkTicketActionHideDetails {
		t.Fatalf("detail card actions = %#v, want hide only for handled card", card.Actions)
	}
}

func TestAppendLarkTicketLinkFieldRendersFullURL(t *testing.T) {
	fields := appendLarkTicketLinkField(nil, "https://dbre.example.test/tickets/TK-9")
	if len(fields) != 1 || fields[0].Label != "工單連結" || fields[0].Value != "[https://dbre.example.test/tickets/TK-9](https://dbre.example.test/tickets/TK-9)" {
		t.Fatalf("appendLarkTicketLinkField() = %#v", fields)
	}
}
