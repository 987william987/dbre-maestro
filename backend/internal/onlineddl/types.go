package onlineddl

import (
	"context"
	"errors"
)

const (
	ModeNative = "native"
	ModeGhost  = "gh-ost"
	ModePTOSC  = "pt-osc"

	StatusPlanned              = "planned"
	StatusQueued               = "queued"
	StatusRunning              = "running"
	StatusPaused               = "paused"
	StatusCancelRequested      = "cancel_requested"
	StatusCompleted            = "completed"
	StatusFailed               = "failed"
	StatusInterrupted          = "interrupted"
	StatusCompletedAfterCancel = "completed_after_cancel"
	StatusCancelled            = "cancelled"
	StatusCancelledArtifacts   = "cancelled_with_artifacts"
	StatusOutcomeUnknown       = "outcome_unknown"
)

type ModeCapability struct {
	Mode                 string
	PauseResume          string
	RuntimeTuning        string
	RequiresExternalTool bool
}

func CapabilityMatrix() []ModeCapability {
	return []ModeCapability{
		{Mode: ModeNative, PauseResume: "none", RuntimeTuning: "none"},
		{Mode: ModeGhost, PauseResume: "control_socket", RuntimeTuning: "allowlist", RequiresExternalTool: true},
		{Mode: ModePTOSC, PauseResume: "pause_file", RuntimeTuning: "immutable", RequiresExternalTool: true},
	}
}

var (
	ErrConflict             = errors.New("online_ddl_conflict")
	ErrStaleVersion         = errors.New("stale_control_version")
	ErrInvalidTransition    = errors.New("invalid_online_ddl_transition")
	ErrModeDisabled         = errors.New("mode_disabled")
	ErrUnsupportedStatement = errors.New("unsupported_statement")
	ErrPreflightChanged     = errors.New("preflight_changed")
	ErrToolUnavailable      = errors.New("tool_unavailable")
	ErrInvalidParameters    = errors.New("invalid_tool_parameters")
	ErrControlNotSupported  = errors.New("control_not_supported")
	ErrToolProcessFailed    = errors.New("tool_process_failed")
	ErrCancelledArtifacts   = errors.New("cancelled_with_artifacts")
	ErrOutcomeUnknown       = errors.New("outcome_unknown")
)

type Adapter interface {
	Mode() string
	Version(context.Context) (string, error)
	Start(context.Context, ToolRequest) (ToolProcess, error)
}

func IsActive(status string) bool {
	return status == StatusQueued || status == StatusRunning || status == StatusPaused || status == StatusCancelRequested
}

func CanTransition(from, to string) bool {
	switch from {
	case StatusPlanned:
		return to == StatusQueued
	case StatusQueued:
		return to == StatusRunning || to == StatusCancelled || to == StatusFailed || to == StatusInterrupted
	case StatusRunning:
		return to == StatusPaused || to == StatusCancelRequested || to == StatusCompleted || to == StatusFailed || to == StatusInterrupted
	case StatusPaused:
		return to == StatusRunning || to == StatusCancelRequested || to == StatusFailed || to == StatusInterrupted
	case StatusCancelRequested:
		return to == StatusCompletedAfterCancel || to == StatusCancelled || to == StatusCancelledArtifacts || to == StatusOutcomeUnknown || to == StatusFailed || to == StatusInterrupted
	default:
		return false
	}
}
