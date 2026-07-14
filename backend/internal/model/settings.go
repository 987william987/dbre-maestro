package model

type PlatformSettings struct {
	AppEnv string `json:"app_env,omitempty"`
	// Deprecated: reviewer routing is now controlled by ApprovalPolicies.
	SensitiveExportReviewerUserIDs []uint64 `json:"sensitive_export_reviewer_user_ids"`
	// Deprecated: reviewer routing is now controlled by ApprovalPolicies.
	SensitiveQueryAccessReviewerUserIDs  []uint64         `json:"sensitive_query_access_reviewer_user_ids"`
	RequireNonSensitiveExportReview      bool             `json:"require_non_sensitive_export_review"`
	ApprovalPolicies                     []ApprovalPolicy `json:"approval_policies"`
	WorkflowRules                        []WorkflowRule   `json:"workflow_rules"`
	LarkAppID                            string           `json:"lark_app_id"`
	LarkAppSecret                        string           `json:"lark_app_secret,omitempty"`
	LarkAppSecretConfigured              bool             `json:"lark_app_secret_configured"`
	LarkOAuthEnabled                     bool             `json:"lark_oauth_enabled"`
	LarkOAuthSite                        string           `json:"lark_oauth_site"`
	LarkOAuthRedirectURL                 string           `json:"lark_oauth_redirect_url"`
	SQLEditorAppTimeoutSeconds           int              `json:"sql_editor_app_timeout_seconds"`
	SQLEditorMySQLMaxExecutionTimeMs     int              `json:"sql_editor_mysql_max_execution_time_ms"`
	SQLEditorPostgresStatementTimeoutMs  int              `json:"sql_editor_postgres_statement_timeout_ms"`
	SQLExportAppTimeoutSeconds           int              `json:"sql_export_app_timeout_seconds"`
	SQLExportMySQLMaxExecutionTimeMs     int              `json:"sql_export_mysql_max_execution_time_ms"`
	SQLExportPostgresStatementTimeoutMs  int              `json:"sql_export_postgres_statement_timeout_ms"`
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

type WorkflowRule struct {
	ID                 uint64      `db:"id" json:"id"`
	RuleName           string      `db:"rule_name" json:"rule_name"`
	TicketType         TicketType  `db:"ticket_type" json:"ticket_type"`
	DBConnectionID     *uint64     `db:"db_connection_id" json:"db_connection_id,omitempty"`
	ExportSensitivity  *string     `db:"export_sensitivity" json:"export_sensitivity,omitempty"`
	ApprovalEnabled    bool        `db:"approval_enabled" json:"approval_enabled"`
	ExecutionMode      string      `db:"execution_mode" json:"execution_mode"`
	ApprovalAuthGroups []AuthGroup `json:"approval_auth_groups"`
	ExecutorAuthGroups []AuthGroup `json:"executor_auth_groups"`
	Priority           int         `db:"priority" json:"priority"`
	Enabled            bool        `db:"enabled" json:"enabled"`
}

type WorkflowExcludedUser struct {
	UserID   uint64 `json:"user_id"`
	Reason   string `json:"reason"`
	Username string `json:"username,omitempty"`
}

type WorkflowResolution struct {
	RuleID                *uint64                `json:"rule_id,omitempty"`
	RuleName              string                 `json:"rule_name"`
	TicketType            TicketType             `json:"ticket_type"`
	DBConnectionID        *uint64                `json:"db_connection_id,omitempty"`
	ExportSensitivity     *string                `json:"export_sensitivity,omitempty"`
	ApprovalEnabled       bool                   `json:"approval_enabled"`
	ExecutionMode         string                 `json:"execution_mode"`
	ApprovalUserIDs       []uint64               `json:"approval_user_ids"`
	ExecutorUserIDs       []uint64               `json:"executor_user_ids"`
	AdminUserIDs          []uint64               `json:"admin_user_ids,omitempty"`
	MissingApprovalGroups []AuthGroup            `json:"missing_approval_groups"`
	MissingExecutorGroups []AuthGroup            `json:"missing_executor_groups"`
	ExcludedApprovalUsers []WorkflowExcludedUser `json:"excluded_approval_users"`
	ExcludedExecutorUsers []WorkflowExcludedUser `json:"excluded_executor_users"`
	ErrorCode             string                 `json:"error_code"`
	ErrorMessage          string                 `json:"error_message"`
}

type WorkflowRulePreview struct {
	Rule               WorkflowRule       `json:"rule"`
	Resolution         WorkflowResolution `json:"resolution"`
	ApprovalUsers      []WorkflowRuleUser `json:"approval_users"`
	ExecutorUsers      []WorkflowRuleUser `json:"executor_users"`
	AdminUsers         []WorkflowRuleUser `json:"admin_users"`
	Effective          bool               `json:"effective"`
	ShadowedByRuleID   *uint64            `json:"shadowed_by_rule_id,omitempty"`
	ShadowedByRuleName string             `json:"shadowed_by_rule_name,omitempty"`
	ConflictRuleIDs    []uint64           `json:"conflict_rule_ids"`
	ConflictRuleNames  []string           `json:"conflict_rule_names"`
}

type WorkflowRuleUser struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
}
