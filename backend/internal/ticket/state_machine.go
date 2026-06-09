package ticket

import (
	"fmt"

	"github.com/dbre-maestro/maestro/internal/model"
)

// TE3: centralized Transition Table — all state changes go through here.
// Adding/removing a transition requires a single edit in one place.
var allowedTransitions = map[model.TicketStatus][]model.TicketStatus{
	model.TicketStatusPendingReview: {
		model.TicketStatusApproved,
		model.TicketStatusRejected,
	},
	model.TicketStatusApproved: {
		model.TicketStatusPendingExecution,
		model.TicketStatusRejected,
	},
	model.TicketStatusPendingExecution: {
		model.TicketStatusExecuting,
		model.TicketStatusStopped,
	},
	model.TicketStatusExecuting: {
		model.TicketStatusCompleted,
		model.TicketStatusFailed,
		model.TicketStatusStopped,
		model.TicketStatusInterrupted,
	},
	// Terminal states — no outgoing transitions
	model.TicketStatusRejected:    {},
	model.TicketStatusCompleted:   {},
	model.TicketStatusFailed:      {},
	model.TicketStatusStopped:     {},
	model.TicketStatusInterrupted: {},
}

type TransitionError struct {
	From model.TicketStatus
	To   model.TicketStatus
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("invalid ticket transition: %s → %s", e.From, e.To)
}

func CanTransition(from, to model.TicketStatus) bool {
	allowed, ok := allowedTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

func ValidateTransition(from, to model.TicketStatus) error {
	if !CanTransition(from, to) {
		return &TransitionError{From: from, To: to}
	}
	return nil
}
