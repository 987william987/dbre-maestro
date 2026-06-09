package ticket_test

import (
	"testing"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/ticket"
)

func TestAllowedTransitions(t *testing.T) {
	cases := []struct {
		from    model.TicketStatus
		to      model.TicketStatus
		allowed bool
	}{
		{model.TicketStatusPendingReview, model.TicketStatusApproved, true},
		{model.TicketStatusPendingReview, model.TicketStatusRejected, true},
		{model.TicketStatusApproved, model.TicketStatusPendingExecution, true},
		{model.TicketStatusPendingExecution, model.TicketStatusExecuting, true},
		{model.TicketStatusExecuting, model.TicketStatusCompleted, true},
		{model.TicketStatusExecuting, model.TicketStatusFailed, true},
		{model.TicketStatusExecuting, model.TicketStatusStopped, true},
		{model.TicketStatusExecuting, model.TicketStatusInterrupted, true},
	}
	for _, c := range cases {
		got := ticket.CanTransition(c.from, c.to)
		if got != c.allowed {
			t.Errorf("CanTransition(%s→%s) = %v, want %v", c.from, c.to, got, c.allowed)
		}
	}
}

func TestBlockedTransitions(t *testing.T) {
	blocked := []struct {
		from model.TicketStatus
		to   model.TicketStatus
	}{
		{model.TicketStatusRejected, model.TicketStatusExecuting},
		{model.TicketStatusCompleted, model.TicketStatusPendingReview},
		{model.TicketStatusInterrupted, model.TicketStatusExecuting},
		{model.TicketStatusPendingReview, model.TicketStatusExecuting},
		{model.TicketStatusExecuting, model.TicketStatusApproved},
	}
	for _, c := range blocked {
		if ticket.CanTransition(c.from, c.to) {
			t.Errorf("CanTransition(%s→%s) should be blocked", c.from, c.to)
		}
		err := ticket.ValidateTransition(c.from, c.to)
		if err == nil {
			t.Errorf("ValidateTransition(%s→%s) should return error", c.from, c.to)
		}
	}
}

func TestValidateTransitionError(t *testing.T) {
	err := ticket.ValidateTransition(model.TicketStatusRejected, model.TicketStatusExecuting)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	te, ok := err.(*ticket.TransitionError)
	if !ok {
		t.Fatalf("expected *TransitionError, got %T", err)
	}
	if te.From != model.TicketStatusRejected || te.To != model.TicketStatusExecuting {
		t.Errorf("wrong error fields: %+v", te)
	}
}
