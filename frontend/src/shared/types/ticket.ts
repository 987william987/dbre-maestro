export type TicketStatus =
  | 'pending_review'
  | 'approved'
  | 'rejected'
  | 'pending_execution'
  | 'executing'
  | 'completed'
  | 'failed'
  | 'stopped'
  | 'interrupted'

export type TicketType = 'ddl' | 'dml'
  | 'sql_export'
  | 'sensitive_query_access'

export type Ticket = {
  id: number
  ticket_no: string
  title: string
  description?: string | null
  sql_content: string
  ticket_type: TicketType
  db_connection_id?: number | null
  status: TicketStatus
  submitter_id: number
  reviewer_id?: number | null
  executor_id?: number | null
  review_comment?: string | null
  rejection_reason?: string | null
  scheduled_at?: string | null
  started_at?: string | null
  completed_at?: string | null
  approved_duration_minutes?: number | null
  approved_until?: string | null
  revoked_at?: string | null
  revoked_by?: number | null
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
}

export type TicketCapabilities = {
  can_review: boolean
  can_revoke: boolean
  can_request_execution: boolean
  can_execute: boolean
  can_download_export: boolean
}

export type TicketDetail = {
  ticket: Ticket
  executions: TicketExecution[]
  scopes: TicketScope[]
  export_request: {
    status: string
    expires_at: string
    download_url?: string | null
  } | null
  capabilities: TicketCapabilities
}
