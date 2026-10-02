import { apiClient } from '@/shared/api/client'

export type SessionConnection = { id: number; name: string; db_type: 'mysql' | 'postgres' | 'postgresql' | 'redis'; operations_configured: boolean }
export type SessionNode = { id: string; role: string; host: string; port: number; availability_zone?: string; status?: string }
export type SessionCluster = { id: string; engine: string; region: string; endpoint?: string; reader_endpoint?: string; nodes: SessionNode[] }
export type DBSession = { id: string; user?: string; database?: string; client?: string; state?: string; duration_seconds: number; query?: string; command?: string; protected: boolean; protected_reason?: string; query_hash: string; backend_start?: string }
export type SessionTarget = { connection_id: number; mode: 'aws'; region: string; cluster_id: string; node_id: string } | { connection_id: number; mode: 'manual'; host: string; port: number }
export type SessionIdentity = { user: string; database: string; client: string; query_hash: string; backend_start: string }
export type SessionActionPayload = SessionTarget & { expected: SessionIdentity }
export type PrefixPreviewItem = Pick<DBSession, 'id' | 'user' | 'database' | 'client' | 'state' | 'duration_seconds' | 'query_hash' | 'backend_start'> & { query_shape: string }
export type PrefixPreview = { preview_token: string; expires_at: string; prefix_shape: string; prefix_hash: string; items: PrefixPreviewItem[] }
export type PrefixCancelResult = { matched_count: number; cancelled_count: number; skipped_count: number; failed_count: number }
export type SessionLoopJob = {
  id: number; connection_id: number; engine: string; target_mode: string; region: string; cluster_id: string; node_id: string; target_host: string; target_port: number
  database_name: string; prefix_shape: string; prefix_hash: string; minimum_age_seconds: number; interval_seconds: number; duration_seconds: number; max_kills: number
  status: string; kill_count: number; consecutive_errors: number; last_error_code?: string; last_error_message?: string; started_at?: string; expires_at?: string; completed_at?: string; created_at: string; updated_at: string
}

export async function listSessionConnections() {
  const response = await apiClient.get<{ items: SessionConnection[] }>('/dba-tools/session-management/connections')
  return Array.isArray(response.items) ? response.items : []
}
export async function listAWSClusters(connectionID: number) {
  const response = await apiClient.get<{ items: SessionCluster[] }>(`/dba-tools/session-management/aws/clusters?connection_id=${connectionID}`)
  return Array.isArray(response.items) ? response.items : []
}
export function getAWSTopology(connectionID: number, region: string, clusterID: string) {
  return apiClient.get<SessionCluster>(`/dba-tools/session-management/aws/topology?connection_id=${connectionID}&region=${encodeURIComponent(region)}&cluster_id=${encodeURIComponent(clusterID)}`)
}
export function listSessions(target: SessionTarget, signal?: AbortSignal) {
  return apiClient.post<{ items: DBSession[]; truncated: boolean }>('/dba-tools/session-management/sessions', target, { signal })
}
export function cancelSession(sessionID: string, payload: SessionActionPayload) {
  return apiClient.post<{ ok: boolean }>(`/dba-tools/session-management/sessions/${encodeURIComponent(sessionID)}/cancel`, payload)
}
export function terminateSession(sessionID: string, payload: SessionActionPayload) {
  return apiClient.post<{ ok: boolean }>(`/dba-tools/session-management/sessions/${encodeURIComponent(sessionID)}/terminate`, payload)
}
export function previewPrefix(payload: SessionTarget & { database: string; prefix: string; minimum_age_seconds: number }) {
  return apiClient.post<PrefixPreview>('/dba-tools/session-management/prefix/preview', payload)
}
export function cancelPrefix(payload: SessionTarget & { preview_token: string }) {
  return apiClient.post<PrefixCancelResult>('/dba-tools/session-management/prefix/cancel', payload)
}
export async function listLoopJobs(limit = 20, offset = 0, signal?: AbortSignal) {
  const response = await apiClient.get<{ items: SessionLoopJob[] }>(`/dba-tools/session-management/loop-jobs?limit=${limit}&offset=${offset}`, { signal })
  return Array.isArray(response.items) ? response.items : []
}
export function createLoopJob(payload: SessionTarget & { preview_token: string; interval_seconds: number; duration_seconds: number; max_kills: number }) {
  return apiClient.post<SessionLoopJob>('/dba-tools/session-management/loop-jobs', payload)
}
export function stopLoopJob(id: number) {
  return apiClient.post<{ ok: boolean }>(`/dba-tools/session-management/loop-jobs/${id}/stop`)
}
