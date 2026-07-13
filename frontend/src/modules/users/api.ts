import { apiClient } from '@/shared/api/client'
import type { AccountSession } from '@/modules/account/api'
import type { AuthGroup } from '@/shared/types/auth'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { QueryAccessEffect } from '@/shared/types/ticket'
import type { UserDetail, UserSummary } from '@/shared/types/user'

type ListUsersResponse = {
  users: UserSummary[]
}

type CreateUserPayload = {
  username: string
  email: string
  lark_recipient?: string
  password: string
}

type CreateUserResponse = {
  id: number
  username: string
  email: string
  lark_recipient?: string
}

type PatchUserPayload = {
  username?: string
  email?: string
  lark_recipient?: string
  password?: string
  is_active?: boolean
  auth_groups?: string[]
  direct_permissions?: string[]
  direct_db_connection_ids?: number[]
}

type AddMembershipPayload = {
  auth_group: AuthGroup
}

type DirectPermissionPayload = {
  permission_key: string
}

type DirectDBConnectionPayload = {
  db_connection_id: number
}

type UserDBConnectionsResponse = {
  connections: DBConnection[]
}

export type QueryAccessRule = {
  id: number
  subject_type: 'user' | 'auth_group'
  subject_id: number
  effect: QueryAccessEffect
  connection_id: number
  database_pattern: string
  table_pattern: string
  granted_via: string
  source_ticket_id?: number | null
  expires_at?: string | null
  revoked_at?: string | null
  revoked_by?: number | null
  created_by?: number | null
  updated_by?: number | null
  created_at: string
  updated_at: string
}

type QueryAccessRulesResponse = {
  rules: QueryAccessRule[]
}

type CreateQueryAccessRulePayload = {
  subject_type: 'user' | 'auth_group'
  subject_id: number
  effect: QueryAccessEffect
  connection_id: number
  database_pattern: string
  table_pattern: string
  duration_minutes: number
}

export function listUsers() {
  return apiClient.get<ListUsersResponse>('/users').then((response): ListUsersResponse => ({
    ...response,
    users: Array.isArray(response.users)
      ? response.users.map((user) => ({
          ...user,
          lark_recipient: typeof user.lark_recipient === 'string' ? user.lark_recipient : '',
          auth_groups: Array.isArray(user.auth_groups) ? user.auth_groups : [],
          permissions: Array.isArray(user.permissions) ? user.permissions : [],
          direct_permissions: Array.isArray(user.direct_permissions) ? user.direct_permissions : [],
          db_connection_ids: Array.isArray(user.db_connection_ids) ? user.db_connection_ids : [],
        }))
      : [],
  }))
}

export function getUser(id: number) {
  return apiClient.get<UserDetail>(`/users/${id}`).then((user): UserDetail => ({
    ...user,
    lark_recipient: typeof user.lark_recipient === 'string' ? user.lark_recipient : '',
    memberships: Array.isArray(user.memberships) ? user.memberships : [],
    permissions: Array.isArray(user.permissions) ? user.permissions : [],
    db_connection_ids: Array.isArray(user.db_connection_ids) ? user.db_connection_ids : [],
    direct_permissions: Array.isArray(user.direct_permissions) ? user.direct_permissions : [],
    direct_db_connection_ids: Array.isArray(user.direct_db_connection_ids) ? user.direct_db_connection_ids : [],
  }))
}

export function createUser(payload: CreateUserPayload) {
  return apiClient.post<CreateUserResponse>('/users', payload)
}

export function patchUser(id: number, payload: PatchUserPayload) {
  return apiClient.patch<CreateUserResponse>(`/users/${id}`, payload)
}

export function addUserMembership(id: number, payload: AddMembershipPayload) {
  return apiClient.post<void>(`/users/${id}/memberships`, payload)
}

export function removeUserMembership(id: number, authGroup: AuthGroup) {
  return apiClient.delete<void>(`/users/${id}/memberships/${authGroup}`)
}

export function deleteUser(id: number) {
  return apiClient.delete<void>(`/users/${id}`)
}

export function addUserDirectPermission(id: number, payload: DirectPermissionPayload) {
  return apiClient.post<void>(`/users/${id}/permissions`, payload)
}

export function removeUserDirectPermission(id: number, permissionKey: string) {
  return apiClient.delete<void>(`/users/${id}/permissions/${encodeURIComponent(permissionKey)}`)
}

export function addUserDirectDBConnection(id: number, payload: DirectDBConnectionPayload) {
  return apiClient.post<void>(`/users/${id}/db-connections`, payload)
}

export function removeUserDirectDBConnection(id: number, dbConnectionId: number) {
  return apiClient.delete<void>(`/users/${id}/db-connections/${dbConnectionId}`)
}

export function listUserDBConnections() {
  return apiClient.get<UserDBConnectionsResponse>('/users/db-connections').then((response) => ({
    ...response,
    connections: Array.isArray(response.connections) ? response.connections : [],
  }))
}

export function listQueryAccessRules() {
  return apiClient.get<QueryAccessRulesResponse>('/users/query-access-rules').then((response) => ({
    ...response,
    rules: Array.isArray(response.rules) ? response.rules : [],
  }))
}

export function createQueryAccessRule(payload: CreateQueryAccessRulePayload) {
  return apiClient.post<QueryAccessRule>('/users/query-access-rules', payload)
}

export function updateQueryAccessRule(id: number, payload: CreateQueryAccessRulePayload) {
  return apiClient.put<QueryAccessRule>(`/users/query-access-rules/${id}`, payload)
}

export function revokeQueryAccessRule(id: number) {
  return apiClient.post<{ ok: boolean }>(`/users/query-access-rules/${id}/revoke`)
}

export function listUserSessions(id: number) {
  return apiClient.get<{ sessions: AccountSession[] }>(`/users/${id}/sessions`).then((response) => ({
    sessions: Array.isArray(response.sessions) ? response.sessions : [],
  }))
}

export function revokeUserSession(userId: number, sessionId: number) {
  return apiClient.delete<void>(`/users/${userId}/sessions/${sessionId}`)
}

export function revokeUserSessions(userId: number) {
  return apiClient.delete<void>(`/users/${userId}/sessions`)
}

export function resetUserMFA(userId: number) {
  return apiClient.post<void>(`/users/${userId}/mfa/reset`)
}
