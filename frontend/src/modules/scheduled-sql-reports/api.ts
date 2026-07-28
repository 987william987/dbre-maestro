import { apiClient } from '@/shared/api/client'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { MetadataResponse } from '@/shared/types/sqlEditor'

export type ScheduledSQLReport = {
  id: number
  name: string
  description?: string | null
  db_connection_id: number
  database_name?: string | null
  schema_name?: string | null
  sql_content: string
  cron_expression: string
  timezone: string
  recipient_user_ids: number[]
  is_active: boolean
  next_run_at?: string | null
  last_run_at?: string | null
  last_status?: string | null
  last_error?: string | null
  created_by: number
  updated_by: number
  created_at: string
  updated_at: string
}

export type ScheduledSQLReportRun = {
  id: number
  report_id: number
  status: string
  row_count: number
  file_name?: string | null
  error_message?: string | null
  started_at: string
  finished_at?: string | null
  created_at: string
}

export type ScheduledReportRecipient = {
  id: number
  username: string
  email: string
  lark_recipient: string
  lark_recipient_type: 'open_id' | 'union_id'
  lark_union_id: string
}

export type ScheduledSQLReportPayload = {
  name: string
  description: string
  db_connection_id: number
  database_name: string
  schema_name: string
  sql_content: string
  cron_expression: string
  timezone: string
  recipient_user_ids: number[]
  is_active: boolean
}

export async function listScheduledSQLReports() {
  const response = await apiClient.get<{ reports: ScheduledSQLReport[] }>('/scheduled-sql-reports')
  return Array.isArray(response.reports) ? response.reports : []
}

export async function getScheduledSQLReport(id: number) {
  return apiClient.get<{ report: ScheduledSQLReport; runs: ScheduledSQLReportRun[] }>(`/scheduled-sql-reports/${id}`).then((response) => ({
    report: response.report,
    runs: Array.isArray(response.runs) ? response.runs : [],
  }))
}

export async function listScheduledReportConnections() {
  const response = await apiClient.get<{ connections: DBConnection[] }>('/scheduled-sql-reports/connections')
  return Array.isArray(response.connections) ? response.connections : []
}

export async function listScheduledReportRecipients() {
  const response = await apiClient.get<{ users: ScheduledReportRecipient[] }>('/scheduled-sql-reports/recipients')
  return Array.isArray(response.users) ? response.users : []
}

export async function listScheduledReportMetadata(connectionId: number, params?: { database?: string; schema?: string }) {
  const searchParams = new URLSearchParams()
  if (params?.database) {
    searchParams.set('database', params.database)
  }
  if (params?.schema) {
    searchParams.set('schema', params.schema)
  }
  const query = searchParams.toString()
  const response = await apiClient.get<MetadataResponse>(`/db-connections/${connectionId}/metadata${query ? `?${query}` : ''}`)
  return {
    ...response,
    items: Array.isArray(response.items) ? response.items : [],
  }
}

export async function createScheduledSQLReport(payload: ScheduledSQLReportPayload) {
  return apiClient.post<ScheduledSQLReport>('/scheduled-sql-reports', payload)
}

export async function updateScheduledSQLReport(id: number, payload: ScheduledSQLReportPayload) {
  return apiClient.patch<ScheduledSQLReport>(`/scheduled-sql-reports/${id}`, payload)
}

export async function deleteScheduledSQLReport(id: number) {
  return apiClient.delete<void>(`/scheduled-sql-reports/${id}`)
}
