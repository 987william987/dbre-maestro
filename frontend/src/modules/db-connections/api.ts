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

type RollbackCapabilityResponse = {
  ok: boolean
  message: string
  checks: Array<{
    name: string
    ok: boolean
    message?: string
  }>
  binlog?: {
    file: string
    pos: number
  }
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

export type DBDatabaseSummary = {
  snapshot_at: string
  database_name: string
  character_set_name?: string | null
  collation_name?: string | null
  table_count: number
  data_size_bytes: number
  index_size_bytes: number
}

export type DBAccountSnapshot = {
  principal_key: string
  principal_name: string
  principal_host?: string | null
  principal_type: 'user' | 'role' | string
  can_login: boolean
  is_superuser: boolean
  inherits_roles: boolean
  can_create_role: boolean
  can_create_database: boolean
  can_replicate: boolean
  can_bypass_rls: boolean
  is_locked: boolean
  valid_until?: string | null
}

export type DBAccountGrantSnapshot = {
  principal_key: string
  grant_kind: 'privilege' | 'role_membership' | string
  grant_statement?: string | null
  granted_role?: string | null
  scope_type?: string | null
  database_name?: string | null
  schema_name?: string | null
  object_name?: string | null
  privilege_type?: string | null
  is_grantable: boolean
}

export type DBAccountScanStatus = {
  last_attempt_at: string
  last_success_at?: string | null
  status: string
  error_message?: string | null
}

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

export function testRollbackCapability(id: number) {
  return apiClient.post<RollbackCapabilityResponse>(`/db-connections/${id}/test-rollback`)
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

export function getDBConnectionOverview(id: number) {
  return apiClient.get<DBConnection>(`/db-connections/${id}/overview`)
}

export function listDBConnectionDatabases(id: number) {
  return apiClient.get<{ connection: DBConnection; items: DBDatabaseSummary[]; total: number }>(`/db-connections/${id}/databases`).then((response) => ({ ...response, items: Array.isArray(response.items) ? response.items : [] }))
}

export function listDBConnectionAccounts(id: number) {
  return apiClient.get<{ connection: DBConnection; accounts: DBAccountSnapshot[]; grants: DBAccountGrantSnapshot[]; scan_status?: DBAccountScanStatus | null }>(`/db-connections/${id}/accounts`).then((response) => ({ ...response, accounts: Array.isArray(response.accounts) ? response.accounts : [], grants: Array.isArray(response.grants) ? response.grants : [] }))
}
