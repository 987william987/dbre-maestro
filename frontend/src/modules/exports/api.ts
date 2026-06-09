import { apiClient } from '@/shared/api/client'

type CreateExportResponse = {
  download_url: string
  expires_at: string
}

export function createExportRequest(payload: { sql_content: string; db_connection_id: number }) {
  return apiClient.post<CreateExportResponse>('/exports', payload)
}
