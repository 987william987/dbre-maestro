package model

type PlatformSettings struct {
	// Deprecated: reviewer routing is now controlled by ApprovalPolicies.
	SensitiveExportReviewerUserIDs []uint64 `json:"sensitive_export_reviewer_user_ids"`
	// Deprecated: reviewer routing is now controlled by ApprovalPolicies.
	SensitiveQueryAccessReviewerUserIDs  []uint64         `json:"sensitive_query_access_reviewer_user_ids"`
	RequireNonSensitiveExportReview      bool             `json:"require_non_sensitive_export_review"`
	ApprovalPolicies                     []ApprovalPolicy `json:"approval_policies"`
	LarkAppID                            string           `json:"lark_app_id"`
	LarkAppSecret                        string           `json:"lark_app_secret,omitempty"`
	LarkAppSecretConfigured              bool             `json:"lark_app_secret_configured"`
	SQLEditorAppTimeoutSeconds           int              `json:"sql_editor_app_timeout_seconds"`
	SQLEditorMySQLMaxExecutionTimeMs     int              `json:"sql_editor_mysql_max_execution_time_ms"`
	SQLEditorPostgresStatementTimeoutMs  int              `json:"sql_editor_postgres_statement_timeout_ms"`
	DBMetadataInventoryEnabled           bool             `json:"db_metadata_inventory_enabled"`
	DBMetadataInventoryRegions           []string         `json:"db_metadata_inventory_regions"`
	DBMetadataInventoryEngines           []string         `json:"db_metadata_inventory_engines"`
	DBMetadataInventoryCron              string           `json:"db_metadata_inventory_cron"`
	DBMetadataInventorySyncIntervalMins  int              `json:"db_metadata_inventory_sync_interval_minutes"`
	DBMetadataObjectEnabled              bool             `json:"db_metadata_object_enabled"`
	DBMetadataObjectEnabledConnectionIDs []uint64         `json:"db_metadata_object_enabled_connection_ids"`
	DBMetadataObjectCron                 string           `json:"db_metadata_object_cron"`
	DBMetadataObjectSyncIntervalMins     int              `json:"db_metadata_object_sync_interval_minutes"`
	DBMetadataCronTimezone               string           `json:"db_metadata_cron_timezone"`
}

type ApprovalWorkflowType string

const (
	ApprovalWorkflowDDL                  ApprovalWorkflowType = "ddl"
	ApprovalWorkflowDML                  ApprovalWorkflowType = "dml"
	ApprovalWorkflowRedisCommand         ApprovalWorkflowType = "redis_command"
	ApprovalWorkflowQueryAccess          ApprovalWorkflowType = "query_access"
	ApprovalWorkflowSQLExportNormal      ApprovalWorkflowType = "sql_export_normal"
	ApprovalWorkflowSQLExportSensitive   ApprovalWorkflowType = "sql_export_sensitive"
	ApprovalWorkflowSensitiveQueryAccess ApprovalWorkflowType = "sensitive_query_access"
)

type ApprovalPolicy struct {
	WorkflowType       ApprovalWorkflowType `db:"workflow_type" json:"workflow_type"`
	ReviewerUserIDs    []uint64             `json:"reviewer_user_ids"`
	ReviewerAuthGroups []AuthGroup          `json:"reviewer_auth_groups"`
	Enabled            bool                 `db:"enabled" json:"enabled"`
}
