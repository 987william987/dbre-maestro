import type { AuditLog } from '@/shared/types/audit'

export type TicketStatus =
  | 'pending_review'
  | 'approved'
  | 'rejected'
  | 'withdrawn'
  | 'pending_execution'
  | 'executing'
  | 'completed'
  | 'failed'
  | 'stopped'
  | 'interrupted'
  | 'needs_admin_attention'

export type TicketType = 'ddl' | 'dml'
  | 'redis_command'
  | 'sql_export'
  | 'sensitive_query_access'
  | 'query_access'

export type TicketExecutionRunMode = 'batch' | 'workflow_auto' | 'manual_statement' | 'whole_ticket' | string
export type DMLExecutionMode = 'per_statement' | 'whole_ticket'

export type QueryAccessScopeMode = 'database' | 'table'
export type QueryAccessEffect = 'allow' | 'deny'

export type QueryAccessTicketItem = {
  id: number
  ticket_id: number
  connection_id: number
  db_connection_name?: string | null
  scope_mode: QueryAccessScopeMode
  database_name: string
  table_name?: string | null
  effect?: QueryAccessEffect
  database_pattern?: string
  table_pattern?: string
  created_at: string
}

export type Ticket = {
  id: number
  ticket_no: string
  title: string
  description?: string | null
  sql_content: string
  ticket_type: TicketType
  db_connection_id?: number | null
  db_connection_name?: string | null
  database_name?: string | null
  schema_name?: string | null
  status: TicketStatus
  submitter_id: number
  submitter_name?: string | null
  reviewer_id?: number | null
  reviewer_name?: string | null
  executor_id?: number | null
  executor_name?: string | null
  review_comment?: string | null
  rejection_reason?: string | null
  scheduled_at?: string | null
  started_at?: string | null
  completed_at?: string | null
  execution_run_mode?: TicketExecutionRunMode | null
  dml_execution_mode?: DMLExecutionMode | null
  approved_duration_minutes?: number | null
  approved_until?: string | null
  revoked_at?: string | null
  revoked_by?: number | null
  revoked_by_name?: string | null
  created_at: string
  updated_at: string
}

export type TicketScope = {
  id: number
  ticket_id: number
  connection_id: number
  database_name?: string | null
  schema_name?: string | null
  table_name?: string | null
  column_name: string
  is_sensitive: boolean
  source_kind: string
  created_at: string
}

export type TicketExecution = {
  id: number
  ticket_id: number
  seq: number
  sql_stmt: string
  status: string
  rows_affected?: number | null
  error_msg?: string | null
  started_at?: string | null
  completed_at?: string | null
  duration_ms?: number | null
  sent_to_db_at?: string | null
  db_process_type?: string | null
  db_process_id?: number | null
  interruption_reason?: string | null
  outcome_confidence?: string | null
}

export type TicketExecutionRollback = {
  id: number
  ticket_id: number
  execution_id: number
  seq: number
  status: 'unsupported' | 'generating' | 'generated' | 'failed' | 'submitted' | string
  unsupported_reason?: string | null
  failure_message?: string | null
  generator?: string | null
  generator_version?: string | null
  source_connection_id: number
  source_database_name?: string | null
  source_schema_name?: string | null
  binlog_start_file?: string | null
  binlog_start_pos?: number | null
  binlog_end_file?: string | null
  binlog_end_pos?: number | null
  rollback_sql_sha256?: string | null
  rollback_sql_bytes?: number | null
  statement_count?: number | null
  confidence?: string | null
  warning_message?: string | null
  rollback_ticket_id?: number | null
  generated_at?: string | null
  created_at: string
  updated_at: string
}

export type TicketReviewResult = {
  id: number
  ticket_id: number
  seq: number
  sql_stmt: string
  phase: string
  validation_stage?: string | null
  statement_kind?: string | null
  object_type?: string | null
  tables?: Array<{
    database_name?: string | null
    schema_name?: string | null
    table_name: string
    row_count?: number | null
    data_size_bytes?: number | null
  }>
  validation_method?: string | null
  scan_rows: number
  status: 'pass' | 'error' | string
  message?: string | null
}

export type TicketCapabilities = {
  can_review: boolean
  can_reject: boolean
  can_withdraw: boolean
  can_revoke: boolean
  can_execute: boolean
  can_retry_workflow_resolution?: boolean
  can_download_export: boolean
}

export type TicketWorkflowParticipants = {
  reviewers: string[]
  executors: string[]
}

export type TicketWorkflowResolution = {
  approval_enabled: boolean
  execution_mode: 'manual' | 'auto_after_approval' | string
}

export type TicketWorkflowTrace = {
  workflow_rule_id?: number | null
  workflow_rule_name: string
  approval_enabled: boolean
  execution_mode?: 'manual' | 'auto_after_approval'
  approval_user_ids: number[]
  executor_user_ids: number[]
  admin_user_ids: number[]
  approval_users?: Array<{ id: number; username: string }>
  executor_users?: Array<{ id: number; username: string }>
  admin_users?: Array<{ id: number; username: string }>
  missing_approval_groups?: Array<{ group_key: string; name: string }>
  missing_executor_groups?: Array<{ group_key: string; name: string }>
  error_code?: string
  error_message?: string
  resolved_at: string
  resolution_trace?: unknown
}

export type TicketDetail = {
  ticket: Ticket
  executions: TicketExecution[]
  execution_rollbacks: TicketExecutionRollback[]
  review_results: TicketReviewResult[]
  activity_logs: AuditLog[]
  scopes: TicketScope[]
  query_access_items: QueryAccessTicketItem[]
  export_request: {
    id: number
    status: string
    expires_at: string
    downloaded_at?: string | null
    download_url?: string | null
  } | null
  workflow_participants: TicketWorkflowParticipants
  workflow_resolution?: TicketWorkflowResolution | null
  workflow_resolution_trace?: TicketWorkflowTrace | null
  capabilities: TicketCapabilities
}

export type WorkflowDashboardSummary = {
  normal_exports: number
  sensitive_exports: number
  auto_approved_exports: number
  needs_admin_attention: number
  by_type: Array<{ key: string; count: number }>
  by_submitter: Array<{ user_id?: number | null; username?: string | null; count: number }>
  by_reviewer: Array<{ user_id?: number | null; username?: string | null; count: number }>
  by_executor: Array<{ user_id?: number | null; username?: string | null; count: number }>
  by_workflow_error: Array<{ error_code: string; count: number }>
}
