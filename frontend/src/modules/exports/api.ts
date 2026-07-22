import { apiClient } from '@/shared/api/client'

export type CreateExportResponse = {
  ticket_id: number
  ticket_no: string
  status: 'pending_review' | 'approved' | 'rejected' | 'ready' | 'pending' | 'expired'
  contains_sensitive: boolean
  scope_count: number
}

export function createExportRequest(payload: {
  sql_content: string
  db_connection_id: number
  database_name?: string
  schema_name?: string
  query_context_token?: string
  reason: string
}) {
  return apiClient.post<CreateExportResponse>('/exports', payload)
}
