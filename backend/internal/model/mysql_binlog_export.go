package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

type MySQLBinlogExportStatus string

const (
	MySQLBinlogExportStatusQueued          MySQLBinlogExportStatus = "queued"
	MySQLBinlogExportStatusRunning         MySQLBinlogExportStatus = "running"
	MySQLBinlogExportStatusCancelRequested MySQLBinlogExportStatus = "cancel_requested"
	MySQLBinlogExportStatusSucceeded       MySQLBinlogExportStatus = "succeeded"
	MySQLBinlogExportStatusFailed          MySQLBinlogExportStatus = "failed"
	MySQLBinlogExportStatusCancelled       MySQLBinlogExportStatus = "cancelled"
	MySQLBinlogExportStatusInterrupted     MySQLBinlogExportStatus = "interrupted"
)

type MySQLBinlogExportArtifactKind string

const (
	MySQLBinlogExportArtifactForwardSQL  MySQLBinlogExportArtifactKind = "forward_sql"
	MySQLBinlogExportArtifactRollbackSQL MySQLBinlogExportArtifactKind = "rollback_sql"
)

type StringList []string

func (items StringList) Value() (driver.Value, error) {
	if items == nil {
		items = StringList{}
	}
	data, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func (items *StringList) Scan(value any) error {
	if items == nil {
		return nil
	}
	if value == nil {
		*items = StringList{}
		return nil
	}
	var data []byte
	switch typed := value.(type) {
	case []byte:
		data = typed
	case string:
		data = []byte(typed)
	default:
		return fmt.Errorf("scan string list: unsupported type %T", value)
	}
	if len(data) == 0 {
		*items = StringList{}
		return nil
	}
	return json.Unmarshal(data, items)
}

type MySQLBinlogExportJob struct {
	ID                     uint64                  `db:"id" json:"id"`
	RequestedBy            uint64                  `db:"requested_by" json:"requested_by"`
	SourceConnectionID     uint64                  `db:"source_connection_id" json:"source_connection_id"`
	RangeMode              string                  `db:"range_mode" json:"range_mode"`
	Timezone               string                  `db:"timezone" json:"timezone"`
	RequestedStartTime     *time.Time              `db:"requested_start_time" json:"requested_start_time,omitempty"`
	RequestedEndTime       *time.Time              `db:"requested_end_time" json:"requested_end_time,omitempty"`
	RequestedStartFile     *string                 `db:"requested_start_file" json:"requested_start_file,omitempty"`
	RequestedStartPos      *uint64                 `db:"requested_start_pos" json:"requested_start_pos,omitempty"`
	RequestedEndFile       *string                 `db:"requested_end_file" json:"requested_end_file,omitempty"`
	RequestedEndPos        *uint64                 `db:"requested_end_pos" json:"requested_end_pos,omitempty"`
	ActualStartFile        *string                 `db:"actual_start_file" json:"actual_start_file,omitempty"`
	ActualStartPos         *uint64                 `db:"actual_start_pos" json:"actual_start_pos,omitempty"`
	ActualEndFile          *string                 `db:"actual_end_file" json:"actual_end_file,omitempty"`
	ActualEndPos           *uint64                 `db:"actual_end_pos" json:"actual_end_pos,omitempty"`
	SourceDatabaseName     *string                 `db:"source_database_name" json:"source_database_name,omitempty"`
	SourceTables           StringList              `db:"source_tables" json:"source_tables"`
	DMLTypes               StringList              `db:"dml_types" json:"dml_types"`
	AcknowledgedUnfiltered bool                    `db:"acknowledged_unfiltered" json:"acknowledged_unfiltered"`
	Status                 MySQLBinlogExportStatus `db:"status" json:"status"`
	Phase                  string                  `db:"phase" json:"phase"`
	ProgressMessage        *string                 `db:"progress_message" json:"progress_message,omitempty"`
	Generator              string                  `db:"generator" json:"generator"`
	GeneratorVersion       string                  `db:"generator_version" json:"generator_version,omitempty"`
	ErrorCode              *string                 `db:"error_code" json:"error_code,omitempty"`
	ErrorMessage           *string                 `db:"error_message" json:"error_message,omitempty"`
	CancelRequestedBy      *uint64                 `db:"cancel_requested_by" json:"cancel_requested_by,omitempty"`
	RetryOfJobID           *uint64                 `db:"retry_of_job_id" json:"retry_of_job_id,omitempty"`
	BinlogFileCount        *uint                   `db:"binlog_file_count" json:"binlog_file_count,omitempty"`
	QueueWaitMs            *uint64                 `db:"queue_wait_ms" json:"queue_wait_ms,omitempty"`
	RangeResolveMs         *uint64                 `db:"range_resolve_ms" json:"range_resolve_ms,omitempty"`
	ForwardGenerationMs    *uint64                 `db:"forward_generation_ms" json:"forward_generation_ms,omitempty"`
	RollbackGenerationMs   *uint64                 `db:"rollback_generation_ms" json:"rollback_generation_ms,omitempty"`
	ArtifactPersistMs      *uint64                 `db:"artifact_persist_ms" json:"artifact_persist_ms,omitempty"`
	TotalDurationMs        *uint64                 `db:"total_duration_ms" json:"total_duration_ms,omitempty"`
	ArtifactExpiresAt      *time.Time              `db:"artifact_expires_at" json:"artifact_expires_at,omitempty"`
	StartedAt              *time.Time              `db:"started_at" json:"started_at,omitempty"`
	CompletedAt            *time.Time              `db:"completed_at" json:"completed_at,omitempty"`
	InterruptedAt          *time.Time              `db:"interrupted_at" json:"interrupted_at,omitempty"`
	CreatedAt              time.Time               `db:"created_at" json:"created_at"`
	UpdatedAt              time.Time               `db:"updated_at" json:"updated_at"`
}

type MySQLBinlogExportArtifact struct {
	ID              uint64                        `db:"id" json:"id"`
	JobID           uint64                        `db:"job_id" json:"job_id"`
	Kind            MySQLBinlogExportArtifactKind `db:"artifact_kind" json:"artifact_kind"`
	Compression     string                        `db:"compression" json:"compression"`
	SQLEncrypted    []byte                        `db:"sql_encrypted" json:"-"`
	PlaintextSHA256 string                        `db:"plaintext_sha256" json:"plaintext_sha256"`
	PlaintextBytes  uint64                        `db:"plaintext_bytes" json:"plaintext_bytes"`
	CompressedBytes uint64                        `db:"compressed_bytes" json:"compressed_bytes"`
	StatementCount  uint                          `db:"statement_count" json:"statement_count"`
	ExpiresAt       time.Time                     `db:"expires_at" json:"expires_at"`
	PurgedAt        *time.Time                    `db:"purged_at" json:"purged_at,omitempty"`
	CreatedAt       time.Time                     `db:"created_at" json:"created_at"`
}
