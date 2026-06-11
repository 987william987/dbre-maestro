import { apiClient } from '@/shared/api/client'

export type CreateExportResponse = {
  id: number
  status: 'ready' | 'pending' | 'rejected' | 'approved' | 'expired'
  sensitive: boolean
  download_url?: string
  expires_at?: string
}

export function createExportRequest(payload: { sql_content: string; db_connection_id: number }) {
  return apiClient.post<CreateExportResponse>('/exports', payload)
}
