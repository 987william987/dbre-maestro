package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

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

const (
	TicketExecutionRunModeBatch           = "batch"
	TicketExecutionRunModeWorkflowAuto    = "workflow_auto"
	TicketExecutionRunModeManualStatement = "manual_statement"
	TicketExecutionRunModeWholeTicket     = "whole_ticket"
	TicketDMLExecutionModePerStatement    = "per_statement"
	TicketDMLExecutionModeWholeTicket     = "whole_ticket"
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
	ApprovedAt              *time.Time   `db:"approved_at"      json:"approved_at,omitempty"`
	ReviewRejectedAt        *time.Time   `db:"review_rejected_at" json:"review_rejected_at,omitempty"`
	PendingExecutionAt      *time.Time   `db:"pending_execution_at" json:"pending_execution_at,omitempty"`
	ExecutionRequestedAt    *time.Time   `db:"execution_requested_at" json:"execution_requested_at,omitempty"`
	ExecutionRunMode        *string      `db:"execution_run_mode" json:"execution_run_mode,omitempty"`
	DMLExecutionMode        *string      `db:"dml_execution_mode" json:"dml_execution_mode,omitempty"`
	ExecutionRejectedAt     *time.Time   `db:"execution_rejected_at" json:"execution_rejected_at,omitempty"`
	WithdrawnAt             *time.Time   `db:"withdrawn_at"     json:"withdrawn_at,omitempty"`
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
	ID                 uint64     `db:"id"                  json:"id"`
	TicketID           uint64     `db:"ticket_id"           json:"ticket_id"`
	Seq                int        `db:"seq"                 json:"seq"`
	SQLStmt            string     `db:"sql_stmt"            json:"sql_stmt"`
	Status             string     `db:"status"              json:"status"`
	RowsAffected       *int64     `db:"rows_affected"       json:"rows_affected,omitempty"`
	ErrorMsg           *string    `db:"error_msg"           json:"error_msg,omitempty"`
	StartedAt          *time.Time `db:"started_at"          json:"started_at,omitempty"`
	CompletedAt        *time.Time `db:"completed_at"        json:"completed_at,omitempty"`
	DurationMs         *int64     `db:"duration_ms"         json:"duration_ms,omitempty"`
	SentToDBAt         *time.Time `db:"sent_to_db_at"       json:"sent_to_db_at,omitempty"`
	DBProcessType      *string    `db:"db_process_type"     json:"db_process_type,omitempty"`
	DBProcessID        *uint64    `db:"db_process_id"       json:"db_process_id,omitempty"`
	InterruptionReason *string    `db:"interruption_reason" json:"interruption_reason,omitempty"`
	OutcomeConfidence  *string    `db:"outcome_confidence"  json:"outcome_confidence,omitempty"`
}

type TicketReviewResult struct {
	ID               uint64             `db:"id"                json:"id"`
	TicketID         uint64             `db:"ticket_id"         json:"ticket_id"`
	Seq              int                `db:"seq"               json:"seq"`
	SQLStmt          string             `db:"sql_stmt"          json:"sql_stmt"`
	Phase            string             `db:"phase"             json:"phase"`
	ValidationStage  *string            `db:"validation_stage"  json:"validation_stage,omitempty"`
	StatementKind    *string            `db:"statement_kind"    json:"statement_kind,omitempty"`
	ObjectType       *string            `db:"object_type"       json:"object_type,omitempty"`
	Tables           TicketReviewTables `db:"tables_json"      json:"tables,omitempty"`
	ValidationMethod *string            `db:"validation_method" json:"validation_method,omitempty"`
	ScanRows         int64              `db:"scan_rows"         json:"scan_rows"`
	Status           string             `db:"status"            json:"status"`
	Message          *string            `db:"message"           json:"message,omitempty"`
	CreatedAt        time.Time          `db:"created_at"        json:"created_at"`
}

type TicketReviewTable struct {
	DatabaseName  string `json:"database_name,omitempty"`
	SchemaName    string `json:"schema_name,omitempty"`
	TableName     string `json:"table_name"`
	RowCount      *int64 `json:"row_count,omitempty"`
	DataSizeBytes *int64 `json:"data_size_bytes,omitempty"`
}

type TicketReviewTables []TicketReviewTable

type TicketExecutionRollback struct {
	ID                   uint64     `db:"id"                     json:"id"`
	TicketID             uint64     `db:"ticket_id"              json:"ticket_id"`
	ExecutionID          uint64     `db:"execution_id"           json:"execution_id"`
	Seq                  int        `db:"seq"                    json:"seq"`
	Status               string     `db:"status"                 json:"status"`
	UnsupportedReason    *string    `db:"unsupported_reason"     json:"unsupported_reason,omitempty"`
	FailureMessage       *string    `db:"failure_message"        json:"failure_message,omitempty"`
	Generator            string     `db:"generator"              json:"generator,omitempty"`
	GeneratorVersion     string     `db:"generator_version"      json:"generator_version,omitempty"`
	SourceConnectionID   uint64     `db:"source_connection_id"   json:"source_connection_id"`
	SourceDatabaseName   *string    `db:"source_database_name"   json:"source_database_name,omitempty"`
	SourceSchemaName     *string    `db:"source_schema_name"     json:"source_schema_name,omitempty"`
	BinlogStartFile      *string    `db:"binlog_start_file"      json:"binlog_start_file,omitempty"`
	BinlogStartPos       *uint64    `db:"binlog_start_pos"       json:"binlog_start_pos,omitempty"`
	BinlogEndFile        *string    `db:"binlog_end_file"        json:"binlog_end_file,omitempty"`
	BinlogEndPos         *uint64    `db:"binlog_end_pos"         json:"binlog_end_pos,omitempty"`
	RollbackSQLEncrypted []byte     `db:"rollback_sql_encrypted" json:"-"`
	RollbackSQLSHA256    *string    `db:"rollback_sql_sha256"    json:"rollback_sql_sha256,omitempty"`
	RollbackSQLBytes     *int64     `db:"rollback_sql_bytes"     json:"rollback_sql_bytes,omitempty"`
	StatementCount       *int       `db:"statement_count"        json:"statement_count,omitempty"`
	Confidence           *string    `db:"confidence"             json:"confidence,omitempty"`
	WarningMessage       *string    `db:"warning_message"        json:"warning_message,omitempty"`
	RollbackTicketID     *uint64    `db:"rollback_ticket_id"     json:"rollback_ticket_id,omitempty"`
	GeneratedAt          *time.Time `db:"generated_at"           json:"generated_at,omitempty"`
	CreatedAt            time.Time  `db:"created_at"             json:"created_at"`
	UpdatedAt            time.Time  `db:"updated_at"             json:"updated_at"`
}

func (tables TicketReviewTables) Value() (driver.Value, error) {
	if len(tables) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(tables)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func (tables *TicketReviewTables) Scan(value any) error {
	if tables == nil {
		return nil
	}
	if value == nil {
		*tables = nil
		return nil
	}
	var data []byte
	switch typed := value.(type) {
	case []byte:
		data = typed
	case string:
		data = []byte(typed)
	default:
		return fmt.Errorf("scan ticket review tables: unsupported type %T", value)
	}
	if len(data) == 0 {
		*tables = nil
		return nil
	}
	return json.Unmarshal(data, tables)
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
