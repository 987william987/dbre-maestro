import { apiClient } from '@/shared/api/client'

export type TableSchemaConnection = { id: number; name: string }
export type TableSchemaOptions = { engine?: string; charset?: string; collation?: string; row_format?: string; auto_increment?: string }
export type TableSchemaTable = { name: string; options: TableSchemaOptions }
export type TableSchemaDependency = { database: string; table: string; referenced_database: string; referenced_table: string }
export type TableSchemaTransformation = { reset_auto_increment: boolean; engine: string; charset: string; collation: string; row_format: string }
export type TableSchemaSelection = { connection_id: number; database: string; tables: string[] }
export type TableSchemaTarget = { connection_id: number; database: string }
export type TableSchemaExportRequest = { source: TableSchemaSelection; transformation: TableSchemaTransformation }
export type TableSchemaSyncRequest = TableSchemaExportRequest & { target: TableSchemaTarget }
export type TableSchemaPreviewTable = { name: string; source: TableSchemaOptions; output: TableSchemaOptions }
export type TableSchemaPreview = { database: string; tables: TableSchemaPreviewTable[]; order: string[]; external_dependencies: TableSchemaDependency[]; warnings: string[]; script: string }
export type TableSchemaSyncPreview = TableSchemaPreview & { preview_token: string; expires_at: string }
export type TableSchemaJobStatus = 'queued' | 'running' | 'cancel_requested' | 'completed' | 'failed' | 'cancelled' | 'interrupted'
export type TableSchemaJob = { id: number; requested_by: number; source_connection_id: number; source_database: string; target_connection_id: number; target_database: string; transformation_config: TableSchemaTransformation; status: TableSchemaJobStatus; retry_of_job_id?: number; table_count: number; created_count: number; failed_count: number; not_started_count: number; error_code?: string; started_at?: string; finished_at?: string; created_at: string; updated_at: string }
export type TableSchemaJobItem = { id: number; job_id: number; table_name: string; dependency_order: number; status: 'pending' | 'created' | 'failed' | 'not_started'; error_code?: string; duration_ms?: number; started_at?: string; finished_at?: string }

const base = '/dba-tools/table-schemas'

export async function listTableSchemaConnections(signal?: AbortSignal) {
	const response = await apiClient.get<{ items: TableSchemaConnection[] }>(`${base}/connections`, { signal })
	return response.items ?? []
}
export async function listTableSchemaDatabases(connectionID: number, signal?: AbortSignal) {
	const response = await apiClient.get<{ items: string[] }>(`${base}/connections/${connectionID}/databases`, { signal })
	return response.items ?? []
}
export async function listTableSchemaTables(connectionID: number, database: string, signal?: AbortSignal) {
	return apiClient.get<{ items: TableSchemaTable[]; dependencies: TableSchemaDependency[] }>(`${base}/connections/${connectionID}/tables?database=${encodeURIComponent(database)}`, { signal })
}
export function previewTableSchemaExport(payload: TableSchemaExportRequest, signal?: AbortSignal) { return apiClient.post<TableSchemaPreview>(`${base}/export/preview`, payload, { signal }) }
export async function downloadTableSchemaExport(payload: TableSchemaExportRequest, signal?: AbortSignal) {
	const response = await apiClient.download(`${base}/export`, { method: 'POST', body: payload, signal })
	const blob = await response.blob(); const url = URL.createObjectURL(blob); const anchor = document.createElement('a')
	anchor.href = url; anchor.download = response.headers.get('content-disposition')?.match(/filename="?([^";]+)"?/i)?.[1] ?? 'table-schema.sql'
	document.body.appendChild(anchor); anchor.click(); anchor.remove(); URL.revokeObjectURL(url)
}
export function previewTableSchemaSync(payload: TableSchemaSyncRequest, signal?: AbortSignal) { return apiClient.post<TableSchemaSyncPreview>(`${base}/sync/preview`, payload, { signal }) }
export function createTableSchemaSyncJob(payload: TableSchemaSyncRequest & { preview_token: string }, signal?: AbortSignal) { return apiClient.post<TableSchemaJob>(`${base}/sync/jobs`, payload, { signal }) }
export async function listTableSchemaJobs(limit: number, offset: number, signal?: AbortSignal) {
	const response = await apiClient.get<{ items: TableSchemaJob[]; total: number }>(`${base}/sync/jobs?limit=${limit}&offset=${offset}`, { signal })
	return { items: response.items ?? [], total: response.total ?? 0 }
}
export function getTableSchemaJob(id: number, signal?: AbortSignal) { return apiClient.get<{ job: TableSchemaJob; items: TableSchemaJobItem[] }>(`${base}/sync/jobs/${id}`, { signal }) }
export function cancelTableSchemaJob(id: number) { return apiClient.post<{ cancel_requested: boolean }>(`${base}/sync/jobs/${id}/cancel`) }
export function retryTableSchemaJob(id: number) { return apiClient.post<TableSchemaJob>(`${base}/sync/jobs/${id}/retry`) }
