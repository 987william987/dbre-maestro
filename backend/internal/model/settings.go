package model

type PlatformSettings struct {
	SensitiveExportReviewerUserIDs       []uint64 `json:"sensitive_export_reviewer_user_ids"`
	SensitiveQueryAccessReviewerUserIDs  []uint64 `json:"sensitive_query_access_reviewer_user_ids"`
	RequireNonSensitiveExportReview      bool     `json:"require_non_sensitive_export_review"`
	LarkAppID                            string   `json:"lark_app_id"`
	LarkAppSecret                        string   `json:"lark_app_secret,omitempty"`
	LarkAppSecretConfigured              bool     `json:"lark_app_secret_configured"`
	SQLEditorAppTimeoutSeconds           int      `json:"sql_editor_app_timeout_seconds"`
	SQLEditorMySQLMaxExecutionTimeMs     int      `json:"sql_editor_mysql_max_execution_time_ms"`
	SQLEditorPostgresStatementTimeoutMs  int      `json:"sql_editor_postgres_statement_timeout_ms"`
	DBMetadataInventoryEnabled           bool     `json:"db_metadata_inventory_enabled"`
	DBMetadataInventoryRegions           []string `json:"db_metadata_inventory_regions"`
	DBMetadataInventoryEngines           []string `json:"db_metadata_inventory_engines"`
	DBMetadataInventorySyncIntervalMins  int      `json:"db_metadata_inventory_sync_interval_minutes"`
	DBMetadataObjectEnabled              bool     `json:"db_metadata_object_enabled"`
	DBMetadataObjectEnabledConnectionIDs []uint64 `json:"db_metadata_object_enabled_connection_ids"`
	DBMetadataObjectSyncIntervalMins     int      `json:"db_metadata_object_sync_interval_minutes"`
}
