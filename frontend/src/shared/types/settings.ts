export type PlatformSettings = {
  sensitive_export_reviewer_user_ids: number[]
  sensitive_query_access_reviewer_user_ids: number[]
  require_non_sensitive_export_review: boolean
  approval_policies: ApprovalPolicy[]
  lark_app_id: string
  lark_app_secret?: string
  lark_app_secret_configured: boolean
  sql_editor_app_timeout_seconds: number
  sql_editor_mysql_max_execution_time_ms: number
  sql_editor_postgres_statement_timeout_ms: number
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
