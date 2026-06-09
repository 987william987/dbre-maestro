import { apiClient } from '@/shared/api/client'
import type { DBConnection } from '@/shared/types/dbConnection'

type ConnectionsResponse = {
  connections: DBConnection[]
}

type ConnectionTestResponse = {
  ok: boolean
  error?: string
}

type CreateConnectionPayload = {
  name: string
  db_type: string
  host: string
  port: number
  database_name?: string | null
  username: string
  password: string
  ssl_mode?: string
}

export function listDBConnections() {
  return apiClient.get<ConnectionsResponse>('/db-connections')
}

export function createDBConnection(payload: CreateConnectionPayload) {
  return apiClient.post<DBConnection>('/db-connections', payload)
}

export function testDBConnection(id: number) {
  return apiClient.post<ConnectionTestResponse>(`/db-connections/${id}/test`)
}

export function deleteDBConnection(id: number) {
  return apiClient.delete<void>(`/db-connections/${id}`)
}
