import { apiClient } from '@/shared/api/client'
import type { AuditLog } from '@/shared/types/audit'

export type AuditListParams = {
  actionType?: string
  actorId?: string
  actorName?: string
  resourceType?: string
  resourceId?: string
  resourceName?: string
  from?: string
  to?: string
  limit?: number
  offset?: number
}

type AuditLogsResponse = {
  logs: AuditLog[]
  total: number
  limit: number
  offset: number
}

export function listAuditLogs(params: AuditListParams) {
  const query = new URLSearchParams()

  if (params.actionType) query.set('action_type', params.actionType)
  if (params.actorId) query.set('actor_id', params.actorId)
  if (params.actorName) query.set('actor_name', params.actorName)
  if (params.resourceType) query.set('resource_type', params.resourceType)
  if (params.resourceId) query.set('resource_id', params.resourceId)
  if (params.resourceName) query.set('resource_name', params.resourceName)
  if (params.from) query.set('from', params.from)
  if (params.to) query.set('to', params.to)
  query.set('limit', String(params.limit ?? 50))
  query.set('offset', String(params.offset ?? 0))

  return apiClient.get<AuditLogsResponse>(`/audit-logs?${query.toString()}`).then((response) => ({
    ...response,
    logs: Array.isArray(response.logs) ? response.logs : [],
  }))
}

export async function exportAuditLogs(params: AuditListParams) {
  const query = new URLSearchParams()

  if (params.actionType) query.set('action_type', params.actionType)
  if (params.actorId) query.set('actor_id', params.actorId)
  if (params.actorName) query.set('actor_name', params.actorName)
  if (params.resourceType) query.set('resource_type', params.resourceType)
  if (params.resourceId) query.set('resource_id', params.resourceId)
  if (params.resourceName) query.set('resource_name', params.resourceName)
  if (params.from) query.set('from', params.from)
  if (params.to) query.set('to', params.to)

  return apiClient.download(`/audit-logs/export?${query.toString()}`)
}
