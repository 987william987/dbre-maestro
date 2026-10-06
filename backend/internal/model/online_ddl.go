package model

import (
	"encoding/json"
	"time"
)

type OnlineDDLRun struct {
	ID                  uint64           `db:"id" json:"id"`
	TicketID            uint64           `db:"ticket_id" json:"ticket_id"`
	ExecutionID         uint64           `db:"execution_id" json:"execution_id"`
	ConnectionID        uint64           `db:"connection_id" json:"connection_id"`
	ExecutorID          uint64           `db:"executor_id" json:"executor_id"`
	Mode                string           `db:"mode" json:"mode"`
	Status              string           `db:"status" json:"status"`
	Phase               *string          `db:"phase" json:"phase,omitempty"`
	InitialParameters   json.RawMessage  `db:"initial_parameters" json:"initial_parameters"`
	EffectiveParameters json.RawMessage  `db:"effective_parameters" json:"effective_parameters"`
	SQLSHA256           string           `db:"sql_sha256" json:"sql_sha256"`
	PreflightSHA256     string           `db:"preflight_sha256" json:"preflight_sha256"`
	ToolVersion         *string          `db:"tool_version" json:"tool_version,omitempty"`
	ProgressPercent     *float64         `db:"progress_percent" json:"progress_percent,omitempty"`
	CopiedRows          *uint64          `db:"copied_rows" json:"copied_rows,omitempty"`
	ETASeconds          *uint64          `db:"eta_seconds" json:"eta_seconds,omitempty"`
	ReplicationLagMs    *uint64          `db:"replication_lag_ms" json:"replication_lag_ms,omitempty"`
	ThreadsRunning      *uint            `db:"threads_running" json:"threads_running,omitempty"`
	ThrottleReason      *string          `db:"throttle_reason" json:"throttle_reason,omitempty"`
	PauseRequested      bool             `db:"pause_requested" json:"pause_requested"`
	CancelRequested     bool             `db:"cancel_requested" json:"cancel_requested"`
	OutcomeConfidence   *string          `db:"outcome_confidence" json:"outcome_confidence,omitempty"`
	ArtifactSummary     *json.RawMessage `db:"artifact_summary" json:"artifact_summary,omitempty"`
	ErrorCode           *string          `db:"error_code" json:"error_code,omitempty"`
	ActiveConnectionID  *uint64          `db:"active_connection_id" json:"-"`
	Version             uint64           `db:"version" json:"version"`
	HeartbeatAt         *time.Time       `db:"heartbeat_at" json:"heartbeat_at,omitempty"`
	StartedAt           *time.Time       `db:"started_at" json:"started_at,omitempty"`
	FinishedAt          *time.Time       `db:"finished_at" json:"finished_at,omitempty"`
	CreatedAt           time.Time        `db:"created_at" json:"created_at"`
	UpdatedAt           time.Time        `db:"updated_at" json:"updated_at"`
}

type OnlineDDLEvent struct {
	ID               uint64          `db:"id" json:"id"`
	RunID            uint64          `db:"run_id" json:"run_id"`
	ActorID          *uint64         `db:"actor_id" json:"actor_id,omitempty"`
	EventType        string          `db:"event_type" json:"event_type"`
	Phase            *string         `db:"phase" json:"phase,omitempty"`
	ProgressPercent  *float64        `db:"progress_percent" json:"progress_percent,omitempty"`
	BeforeParameters json.RawMessage `db:"before_parameters" json:"before_parameters,omitempty"`
	AfterParameters  json.RawMessage `db:"after_parameters" json:"after_parameters,omitempty"`
	Result           *string         `db:"result" json:"result,omitempty"`
	CreatedAt        time.Time       `db:"created_at" json:"created_at"`
}
