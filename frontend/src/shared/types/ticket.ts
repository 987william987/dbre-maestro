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

export type TicketType = 'ddl' | 'dml'
  | 'redis_command'
  | 'sql_export'
  | 'sensitive_query_access'
  | 'query_access'

export type QueryAccessScopeMode = 'database' | 'table'

export type QueryAccessTicketItem = {
  id: number
  ticket_id: number
  connection_id: number
  scope_mode: QueryAccessScopeMode
  database_name: string
  table_name?: string | null
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
  can_download_export: boolean
}

export type TicketWorkflowParticipants = {
  reviewers: string[]
  executors: string[]
}

export type TicketDetail = {
  ticket: Ticket
  executions: TicketExecution[]
  review_results: TicketReviewResult[]
  activity_logs: AuditLog[]
  scopes: TicketScope[]
  query_access_items: QueryAccessTicketItem[]
  export_request: {
    status: string
    expires_at: string
    download_url?: string | null
  } | null
  workflow_participants: TicketWorkflowParticipants
  capabilities: TicketCapabilities
}
