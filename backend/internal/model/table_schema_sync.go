package model

import "time"

const (
	TableSchemaSyncQueued          = "queued"
	TableSchemaSyncRunning         = "running"
	TableSchemaSyncCancelRequested = "cancel_requested"
	TableSchemaSyncCompleted       = "completed"
	TableSchemaSyncFailed          = "failed"
	TableSchemaSyncCancelled       = "cancelled"
	TableSchemaSyncInterrupted     = "interrupted"
	TableSchemaSyncItemPending     = "pending"
	TableSchemaSyncItemCreated     = "created"
	TableSchemaSyncItemFailed      = "failed"
	TableSchemaSyncItemNotStarted  = "not_started"
)

type TableSchemaSyncJob struct {
	ID                   uint64     `db:"id" json:"id"`
	RequestedBy          uint64     `db:"requested_by" json:"requested_by"`
	SourceConnectionID   uint64     `db:"source_connection_id" json:"source_connection_id"`
	SourceDatabase       string     `db:"source_database" json:"source_database"`
	TargetConnectionID   uint64     `db:"target_connection_id" json:"target_connection_id"`
	TargetDatabase       string     `db:"target_database" json:"target_database"`
	TransformationConfig []byte     `db:"transformation_config" json:"transformation_config"`
	Status               string     `db:"status" json:"status"`
	ActiveTargetKey      *string    `db:"active_target_key" json:"-"`
	CancelRequestedBy    *uint64    `db:"cancel_requested_by" json:"cancel_requested_by,omitempty"`
	RetryOfJobID         *uint64    `db:"retry_of_job_id" json:"retry_of_job_id,omitempty"`
	TableCount           uint       `db:"table_count" json:"table_count"`
	CreatedCount         uint       `db:"created_count" json:"created_count"`
	FailedCount          uint       `db:"failed_count" json:"failed_count"`
	NotStartedCount      uint       `db:"not_started_count" json:"not_started_count"`
	ErrorCode            *string    `db:"error_code" json:"error_code,omitempty"`
	ErrorMessage         *string    `db:"error_message" json:"error_message,omitempty"`
	StartedAt            *time.Time `db:"started_at" json:"started_at,omitempty"`
	FinishedAt           *time.Time `db:"finished_at" json:"finished_at,omitempty"`
	CreatedAt            time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt            time.Time  `db:"updated_at" json:"updated_at"`
}

type TableSchemaSyncJobItem struct {
	ID              uint64     `db:"id" json:"id"`
	JobID           uint64     `db:"job_id" json:"job_id"`
	TableName       string     `db:"table_name" json:"table_name"`
	DependencyOrder uint       `db:"dependency_order" json:"dependency_order"`
	SourceDDLSHA256 string     `db:"source_ddl_sha256" json:"source_ddl_sha256"`
	TargetDDLSHA256 *string    `db:"target_ddl_sha256" json:"target_ddl_sha256,omitempty"`
	Status          string     `db:"status" json:"status"`
	ErrorCode       *string    `db:"error_code" json:"error_code,omitempty"`
	DurationMs      *uint64    `db:"duration_ms" json:"duration_ms,omitempty"`
	StartedAt       *time.Time `db:"started_at" json:"started_at,omitempty"`
	FinishedAt      *time.Time `db:"finished_at" json:"finished_at,omitempty"`
	CreatedAt       time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at" json:"updated_at"`
}
