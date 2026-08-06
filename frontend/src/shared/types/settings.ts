export type PlatformSettings = {
  app_env?: string
  sensitive_export_reviewer_user_ids: number[]
  sensitive_query_access_reviewer_user_ids: number[]
  require_non_sensitive_export_review: boolean
  approval_policies: ApprovalPolicy[]
  workflow_rules: WorkflowRule[]
  lark_app_id: string
  lark_app_secret?: string
  lark_app_secret_configured: boolean
  lark_interactive_cards_enabled: boolean
  lark_card_callback_mode: 'http' | 'long_connection'
  lark_card_verification_token?: string
  lark_card_verification_token_configured: boolean
  lark_oauth_enabled: boolean
  lark_oauth_site: 'lark' | 'feishu'
  lark_oauth_redirect_url: string
  sso_oidc_enabled: boolean
  sso_oidc_display_name: string
  sso_oidc_issuer_url: string
  sso_oidc_client_id: string
  sso_oidc_client_secret?: string
  sso_oidc_client_secret_configured: boolean
  sso_oidc_redirect_url: string
  sso_oidc_scopes: string[]
  sso_oidc_trust_mfa: boolean
  sql_editor_app_timeout_seconds: number
  sql_editor_mysql_max_execution_time_ms: number
  sql_editor_postgres_statement_timeout_ms: number
  sql_export_app_timeout_seconds: number
  sql_export_mysql_max_execution_time_ms: number
  sql_export_postgres_statement_timeout_ms: number
  mysql_rollback_enabled: boolean
  mysql_rollback_my2sql_path: string
  mysql_rollback_generation_timeout_seconds: number
  mysql_rollback_max_sql_bytes: number
  db_metadata_inventory_enabled: boolean
  db_metadata_inventory_regions: string[]
  db_metadata_inventory_engines: string[]
  db_metadata_inventory_cron: string
  db_metadata_inventory_sync_interval_minutes: number
  db_metadata_object_enabled: boolean
  db_metadata_object_enabled_connection_ids: number[]
  db_metadata_object_cron: string
  db_metadata_object_sync_interval_minutes: number
  db_metadata_cron_timezone: string
}

export type ApprovalWorkflowType =
  | 'ddl'
  | 'dml'
  | 'redis_command'
  | 'query_access'
  | 'sql_export_normal'
  | 'sql_export_sensitive'
  | 'sensitive_query_access'

export type ApprovalPolicy = {
  workflow_type: ApprovalWorkflowType
  reviewer_user_ids: number[]
  reviewer_auth_groups: string[]
  enabled: boolean
}

export type WorkflowRule = {
  id?: number
  rule_name: string
  ticket_type: 'ddl' | 'dml' | 'redis_command' | 'query_access' | 'sql_export' | 'sensitive_query_access'
  db_connection_id?: number | null
  export_sensitivity?: 'normal' | 'sensitive' | null
  approval_enabled: boolean
  execution_mode: 'manual' | 'auto_after_approval'
  approval_auth_groups: string[]
  executor_auth_groups: string[]
  priority: number
  enabled: boolean
}

export type WorkflowResolution = {
  rule_id?: number
  rule_name: string
  ticket_type: WorkflowRule['ticket_type']
  db_connection_id?: number | null
  export_sensitivity?: 'normal' | 'sensitive' | null
  approval_enabled: boolean
  approval_user_ids: number[]
  executor_user_ids: number[]
  error_code?: string
  error_message?: string
}

export type WorkflowRuleUser = {
  id: number
  username: string
}

export type WorkflowRulePreview = {
  rule: WorkflowRule
  resolution: WorkflowResolution
  approval_users: WorkflowRuleUser[]
  executor_users: WorkflowRuleUser[]
  admin_users: WorkflowRuleUser[]
  effective: boolean
  shadowed_by_rule_id?: number
  shadowed_by_rule_name?: string
  conflict_rule_ids: number[]
  conflict_rule_names: string[]
}

export type WorkflowSimulationRequest = {
  ticket_type: WorkflowRule['ticket_type']
  db_connection_id?: number | null
  export_sensitivity?: 'normal' | 'sensitive' | null
}

export type ApprovalResolutionUser = {
  id: number
  username: string
  sources?: string[]
  reason?: string
}

export type ApprovalResolutionWorkflow = {
  workflow_type: ApprovalWorkflowType
  enabled: boolean
  required_permissions: string[]
  reviewer_user_ids: number[]
  reviewer_auth_groups: string[]
  candidate_reviewers: ApprovalResolutionUser[]
  effective_reviewers: ApprovalResolutionUser[]
  excluded_reviewers: ApprovalResolutionUser[]
  missing_reviewer_user_ids: number[]
}
