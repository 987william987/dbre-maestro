package onlineddl

import "testing"

func TestCapabilityMatrixKeepsNativeAndExternalModesDistinct(t *testing.T) {
	matrix := CapabilityMatrix()
	if len(matrix) != 3 || matrix[0].Mode != ModeNative || matrix[0].RequiresExternalTool || matrix[1].Mode != ModeGhost || !matrix[1].RequiresExternalTool || matrix[2].Mode != ModePTOSC || matrix[2].RuntimeTuning != "immutable" {
		t.Fatalf("matrix = %+v", matrix)
	}
}

func TestStateMachineRejectsUnsafeTerminalAndRestartTransitions(t *testing.T) {
	allowed := [][2]string{{StatusPlanned, StatusQueued}, {StatusQueued, StatusRunning}, {StatusRunning, StatusPaused}, {StatusPaused, StatusRunning}, {StatusRunning, StatusCancelRequested}, {StatusCancelRequested, StatusOutcomeUnknown}}
	for _, transition := range allowed {
		if !CanTransition(transition[0], transition[1]) {
			t.Fatalf("expected %s -> %s to be allowed", transition[0], transition[1])
		}
	}
	for _, transition := range [][2]string{{StatusPlanned, StatusRunning}, {StatusCompleted, StatusRunning}, {StatusFailed, StatusQueued}, {StatusPaused, StatusCompleted}} {
		if CanTransition(transition[0], transition[1]) {
			t.Fatalf("unsafe transition %s -> %s must be rejected", transition[0], transition[1])
		}
	}
}

func TestOnlyLiveToolStatusesHoldConnectionLock(t *testing.T) {
	for _, status := range []string{StatusQueued, StatusRunning, StatusPaused, StatusCancelRequested} {
		if !IsActive(status) {
			t.Fatalf("%s must hold the connection lock", status)
		}
	}
	for _, status := range []string{StatusPlanned, StatusCompleted, StatusFailed, StatusInterrupted, StatusCancelled, StatusOutcomeUnknown} {
		if IsActive(status) {
			t.Fatalf("%s must release the connection lock", status)
		}
	}
}
