package tableschema

import (
	"fmt"
	"time"
)

const (
	MaxTables        = 50
	MaxTableDDLBytes = 2 << 20
	MaxExportBytes   = 16 << 20
	PreviewTTL       = 60 * time.Second
)

type ErrorCode string

const (
	ErrorFeatureNotReady         ErrorCode = "feature_not_ready"
	ErrorUnsupportedEngine       ErrorCode = "unsupported_engine"
	ErrorInvalidTableSelection   ErrorCode = "invalid_table_selection"
	ErrorSourceScopeDenied       ErrorCode = "source_scope_denied"
	ErrorTargetScopeDenied       ErrorCode = "target_scope_denied"
	ErrorInvalidTransformation   ErrorCode = "invalid_transformation"
	ErrorConnectionUnavailable   ErrorCode = "connection_unavailable"
	ErrorMetadataQueryFailed     ErrorCode = "metadata_query_failed"
	ErrorForeignKeyCycle         ErrorCode = "foreign_key_cycle"
	ErrorExportTooLarge          ErrorCode = "export_too_large"
	ErrorTargetTableExists       ErrorCode = "target_table_exists"
	ErrorExternalMissing         ErrorCode = "external_dependency_missing"
	ErrorPreviewExpired          ErrorCode = "preview_expired"
	ErrorPreviewReplayed         ErrorCode = "preview_replayed"
	ErrorSourceDDLChanged        ErrorCode = "source_ddl_changed"
	ErrorTargetCapabilityChanged ErrorCode = "target_capability_changed"
	ErrorInvalidRetryState       ErrorCode = "invalid_retry_state"
	ErrorRetryTargetDrifted      ErrorCode = "retry_target_drifted"
)

type StableError struct {
	Code    ErrorCode
	Message string
}

func (e *StableError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

type TransformationConfig struct {
	ResetAutoIncrement bool   `json:"reset_auto_increment"`
	Engine             string `json:"engine"`
	Charset            string `json:"charset"`
	Collation          string `json:"collation"`
	RowFormat          string `json:"row_format"`
}

type SourceSelection struct {
	ConnectionID uint64   `json:"connection_id"`
	Database     string   `json:"database"`
	Tables       []string `json:"tables"`
}

type TargetSelection struct {
	ConnectionID uint64 `json:"connection_id"`
	Database     string `json:"database"`
}

type ExportRequest struct {
	Source         SourceSelection      `json:"source"`
	Transformation TransformationConfig `json:"transformation"`
}

type ExportTable struct {
	Name            string       `json:"name"`
	Source          TableOptions `json:"source"`
	Output          TableOptions `json:"output"`
	SourceDDLSHA256 string       `json:"-"`
}

type ExportResult struct {
	Database             string        `json:"database"`
	Tables               []ExportTable `json:"tables"`
	Order                []string      `json:"order"`
	ExternalDependencies []Dependency  `json:"external_dependencies"`
	Warnings             []string      `json:"warnings"`
	Script               string        `json:"script"`
}

type SyncPreviewRequest struct {
	Source         SourceSelection      `json:"source"`
	Target         TargetSelection      `json:"target"`
	Transformation TransformationConfig `json:"transformation"`
}

type SyncPreviewResult struct {
	ExportResult
	PreviewToken string    `json:"preview_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type RetryItem struct {
	TableName       string
	DependencyOrder uint
	SourceDDLSHA256 string
}
