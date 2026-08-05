package handler

import "testing"

func TestPendingExecutionNotificationIncludesActor(t *testing.T) {
	policy := ticketNotificationPolicies[ticketEventPendingExecution]
	if !policy.NotifyActor {
		t.Fatal("pending execution notifications must include the actor when the reviewer is also an executor")
	}
}
