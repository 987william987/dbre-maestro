import { apiClient } from '@/shared/api/client'
import type { DBConnection, DBConnectionBindings } from '@/shared/types/dbConnection'

type ConnectionsResponse = {
  connections: DBConnection[]
}

type ConnectionTestResponse = {
  ok: boolean
  error?: string
  last_test_status: 'passed' | 'failed' | string
  last_test_error?: string
  last_tested_at?: string
  results?: Array<{
    credential_role: 'readonly' | 'readwrite' | string
    ok: boolean
    error?: string
  }>
}

type CreateConnectionPayload = {
  name: string
  db_type: string
  host: string
  port: number
  readonly_host: string
  readonly_port: number
  readwrite_host: string
  readwrite_port: number
  database_name?: string | null
  username: string
  password: string
  ssl_mode?: string
  credentials?: Array<{
    credential_role: 'readonly' | 'readwrite' | string
    username: string
    password: string
  }>
}

type PatchConnectionPayload = Partial<CreateConnectionPayload>
type ConnectionBindingsResponse = DBConnectionBindings

export function listDBConnections() {
  return apiClient.get<ConnectionsResponse>('/db-connections').then((response) => ({
    ...response,
    connections: Array.isArray(response.connections) ? response.connections : [],
  }))
}

export function createDBConnection(payload: CreateConnectionPayload) {
  return apiClient.post<DBConnection>('/db-connections', payload)
}

export function testDBConnection(id: number, credentialRole?: 'readonly' | 'readwrite' | string) {
  const suffix = credentialRole ? `?credential_role=${encodeURIComponent(credentialRole)}` : ''
  return apiClient.post<ConnectionTestResponse>(`/db-connections/${id}/test${suffix}`)
}

export function deleteDBConnection(id: number) {
  return apiClient.delete<void>(`/db-connections/${id}`)
}

export function patchDBConnection(id: number, payload: PatchConnectionPayload) {
  return apiClient.patch<DBConnection>(`/db-connections/${id}`, payload)
}

export function getDBConnectionBindings(id: number) {
  return apiClient.get<ConnectionBindingsResponse>(`/db-connections/${id}/bindings`)
}
