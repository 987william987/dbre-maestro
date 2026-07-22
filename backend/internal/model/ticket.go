package model

import "time"

type TicketStatus string

const (
	TicketStatusPendingReview       TicketStatus = "pending_review"
	TicketStatusApproved            TicketStatus = "approved"
	TicketStatusRejected            TicketStatus = "rejected"
	TicketStatusWithdrawn           TicketStatus = "withdrawn"
	TicketStatusPendingExecution    TicketStatus = "pending_execution"
	TicketStatusExecuting           TicketStatus = "executing"
	TicketStatusCompleted           TicketStatus = "completed"
	TicketStatusFailed              TicketStatus = "failed"
	TicketStatusStopped             TicketStatus = "stopped"
	TicketStatusInterrupted         TicketStatus = "interrupted"
	TicketStatusNeedsAdminAttention TicketStatus = "needs_admin_attention"
)

type TicketType string

const (
	TicketTypeDDL                  TicketType = "ddl"
	TicketTypeDML                  TicketType = "dml"
	TicketTypeRedisCommand         TicketType = "redis_command"
	TicketTypeSQLExport            TicketType = "sql_export"
	TicketTypeSensitiveQueryAccess TicketType = "sensitive_query_access"
	TicketTypeQueryAccess          TicketType = "query_access"
)

type Ticket struct {
	ID                      uint64       `db:"id"               json:"id"`
	TicketNo                string       `db:"ticket_no"        json:"ticket_no"`
	Title                   string       `db:"title"            json:"title"`
	Description             *string      `db:"description"      json:"description,omitempty"`
	SQLContent              string       `db:"sql_content"      json:"sql_content"`
	TicketType              TicketType   `db:"ticket_type"      json:"ticket_type"`
	ContainsSensitive       *bool        `db:"contains_sensitive" json:"contains_sensitive,omitempty"`
	DBConnectionID          *uint64      `db:"db_connection_id" json:"db_connection_id,omitempty"`
	DatabaseName            *string      `db:"database_name"    json:"database_name,omitempty"`
	SchemaName              *string      `db:"schema_name"      json:"schema_name,omitempty"`
	Status                  TicketStatus `db:"status"           json:"status"`
	SubmitterID             uint64       `db:"submitter_id"     json:"submitter_id"`
	ReviewerID              *uint64      `db:"reviewer_id"      json:"reviewer_id,omitempty"`
	ExecutorID              *uint64      `db:"executor_id"      json:"executor_id,omitempty"`
	ReviewComment           *string      `db:"review_comment"   json:"review_comment,omitempty"`
	RejectionReason         *string      `db:"rejection_reason" json:"rejection_reason,omitempty"`
	ScheduledAt             *time.Time   `db:"scheduled_at"     json:"scheduled_at,omitempty"`
	StartedAt               *time.Time   `db:"started_at"       json:"started_at,omitempty"`
	CompletedAt             *time.Time   `db:"completed_at"     json:"completed_at,omitempty"`
	ApprovedDurationMinutes *int         `db:"approved_duration_minutes" json:"approved_duration_minutes,omitempty"`
	ApprovedUntil           *time.Time   `db:"approved_until"     json:"approved_until,omitempty"`
	RevokedAt               *time.Time   `db:"revoked_at"         json:"revoked_at,omitempty"`
	RevokedBy               *uint64      `db:"revoked_by"         json:"revoked_by,omitempty"`
	CreatedAt               time.Time    `db:"created_at"       json:"created_at"`
	UpdatedAt               time.Time    `db:"updated_at"       json:"updated_at"`
}

type TicketScope struct {
	ID           uint64    `db:"id"            json:"id"`
	TicketID     uint64    `db:"ticket_id"     json:"ticket_id"`
	ConnectionID uint64    `db:"connection_id" json:"connection_id"`
	DatabaseName *string   `db:"database_name" json:"database_name,omitempty"`
	SchemaName   *string   `db:"schema_name"   json:"schema_name,omitempty"`
	TableName    *string   `db:"table_name"    json:"table_name,omitempty"`
	ColumnName   string    `db:"column_name"   json:"column_name"`
	IsSensitive  bool      `db:"is_sensitive"  json:"is_sensitive"`
	SourceKind   string    `db:"source_kind"   json:"source_kind"`
	CreatedAt    time.Time `db:"created_at"    json:"created_at"`
}

type TicketExecution struct {
	ID           uint64     `db:"id"            json:"id"`
	TicketID     uint64     `db:"ticket_id"     json:"ticket_id"`
	Seq          int        `db:"seq"           json:"seq"`
	SQLStmt      string     `db:"sql_stmt"      json:"sql_stmt"`
	Status       string     `db:"status"        json:"status"`
	RowsAffected *int64     `db:"rows_affected" json:"rows_affected,omitempty"`
	ErrorMsg     *string    `db:"error_msg"     json:"error_msg,omitempty"`
	StartedAt    *time.Time `db:"started_at"    json:"started_at,omitempty"`
	CompletedAt  *time.Time `db:"completed_at"  json:"completed_at,omitempty"`
	DurationMs   *int64     `db:"duration_ms"   json:"duration_ms,omitempty"`
}

type TicketReviewResult struct {
	ID               uint64    `db:"id"                json:"id"`
	TicketID         uint64    `db:"ticket_id"         json:"ticket_id"`
	Seq              int       `db:"seq"               json:"seq"`
	SQLStmt          string    `db:"sql_stmt"          json:"sql_stmt"`
	Phase            string    `db:"phase"             json:"phase"`
	ValidationStage  *string   `db:"validation_stage"  json:"validation_stage,omitempty"`
	StatementKind    *string   `db:"statement_kind"    json:"statement_kind,omitempty"`
	ObjectType       *string   `db:"object_type"       json:"object_type,omitempty"`
	ValidationMethod *string   `db:"validation_method" json:"validation_method,omitempty"`
	ScanRows         int64     `db:"scan_rows"         json:"scan_rows"`
	Status           string    `db:"status"            json:"status"`
	Message          *string   `db:"message"           json:"message,omitempty"`
	CreatedAt        time.Time `db:"created_at"        json:"created_at"`
}

type TicketWorkflowSnapshot struct {
	TicketID        uint64    `db:"ticket_id" json:"ticket_id"`
	RuleID          *uint64   `db:"workflow_rule_id" json:"workflow_rule_id,omitempty"`
	RuleName        string    `db:"workflow_rule_name" json:"workflow_rule_name"`
	ApprovalEnabled bool      `db:"approval_enabled" json:"approval_enabled"`
	ExecutionMode   string    `db:"execution_mode" json:"execution_mode"`
	ApprovalUserIDs []uint64  `json:"approval_user_ids"`
	ExecutorUserIDs []uint64  `json:"executor_user_ids"`
	AdminUserIDs    []uint64  `json:"admin_user_ids"`
	ErrorCode       string    `db:"error_code" json:"error_code,omitempty"`
	ErrorMessage    string    `db:"error_message" json:"error_message,omitempty"`
	ResolutionTrace string    `db:"resolution_trace" json:"resolution_trace"`
	ResolvedAt      time.Time `db:"resolved_at" json:"resolved_at"`
	CreatedAt       time.Time `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time `db:"updated_at" json:"updated_at"`
}
