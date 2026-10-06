import { apiClient } from '@/shared/api/client'

export type BinlogExportStatus = 'queued' | 'running' | 'cancel_requested' | 'succeeded' | 'failed' | 'cancelled' | 'interrupted'
export type BinlogArtifactKind = 'forward_sql' | 'rollback_sql'

export type BinlogExportConnection = { id: number; name: string }
export type BinlogFile = { name: string; size_bytes: number; active: boolean; current_position?: number; start_time?: string; end_time?: string }
export type BinlogTimestamp = { file: string; start_time: string }

export type BinlogExportJob = {
  id: number
  requested_by: number
  source_connection_id: number
  range_mode: 'time' | 'position'
  timezone: string
  requested_start_time?: string | null
  requested_end_time?: string | null
  requested_start_file?: string | null
  requested_start_pos?: number | null
  requested_end_file?: string | null
  requested_end_pos?: number | null
  source_database_name?: string | null
  source_tables: string[]
  dml_types: string[]
  acknowledged_unfiltered: boolean
  status: BinlogExportStatus
  phase: string
  progress_message?: string | null
  error_message?: string | null
  retry_of_job_id?: number | null
	artifact_expires_at?: string | null
  started_at?: string | null
  completed_at?: string | null
  created_at: string
  updated_at: string
}

export type CreateBinlogExportPayload = {
  source_connection_id: number
  range_mode: 'time' | 'position'
  timezone: string
  start_time?: string
  end_time?: string
  start_file?: string
  start_pos?: number
  end_file?: string
  end_pos?: number
  database?: string
  tables: string[]
  dml_types: string[]
  acknowledge_unfiltered: boolean
}

export async function listBinlogExportConnections() {
  const response = await apiClient.get<{ items: BinlogExportConnection[] }>('/binlog-exports/connections')
  return Array.isArray(response.items) ? response.items : []
}

export async function listBinlogExportJobs(limit = 20, offset = 0) {
  const response = await apiClient.get<{ items: BinlogExportJob[]; total: number }>(`/binlog-exports?limit=${limit}&offset=${offset}`)
  return { items: Array.isArray(response.items) ? response.items : [], total: response.total ?? 0 }
}

export async function listBinlogs(connectionID: number) {
  const response = await apiClient.get<{ items: BinlogFile[]; time_metadata_available: boolean }>(`/binlog-exports/connections/${connectionID}/binlogs`)
  return { ...response, items: Array.isArray(response.items) ? response.items : [] }
}

export async function probeBinlogTimestamps(connectionID: number, file: string) {
  const response = await apiClient.post<{ items: BinlogTimestamp[] }>(`/binlog-exports/connections/${connectionID}/binlogs/timestamps`, { file })
  return Array.isArray(response.items) ? response.items : []
}

export async function listBinlogDatabases(connectionID: number) {
  const response = await apiClient.get<{ items: string[] }>(`/binlog-exports/connections/${connectionID}/databases`)
  return Array.isArray(response.items) ? response.items : []
}

export async function listBinlogTables(connectionID: number, database: string) {
  const response = await apiClient.get<{ items: string[] }>(`/binlog-exports/connections/${connectionID}/tables?database=${encodeURIComponent(database)}`)
  return Array.isArray(response.items) ? response.items : []
}

export function createBinlogExport(payload: CreateBinlogExportPayload) {
  return apiClient.post<{ id: number; status: BinlogExportStatus }>('/binlog-exports', payload)
}

export function cancelBinlogExport(id: number) {
  return apiClient.post<{ ok: boolean }>(`/binlog-exports/${id}/cancel`)
}

export function retryBinlogExport(id: number) {
  return apiClient.post<{ id: number; status: BinlogExportStatus }>(`/binlog-exports/${id}/retry`)
}

export async function downloadBinlogArtifact(id: number, kind: BinlogArtifactKind) {
  const response = await apiClient.download(`/binlog-exports/${id}/artifacts/${kind}`)
  const blob = await response.blob()
  const objectURL = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  const filename = response.headers.get('content-disposition')?.match(/filename="?([^";]+)"?/i)?.[1] ?? `binlog-export-${id}-${kind}.sql`
  anchor.href = objectURL
  anchor.download = filename
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(objectURL)
}

export function previewBinlogArtifact(id: number, kind: BinlogArtifactKind) {
	return apiClient.get<{ sql: string; truncated: boolean; expires_at: string }>(`/binlog-exports/${id}/artifacts/${kind}/preview`)
}
