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
  created_at: string
  updated_at: string
}
