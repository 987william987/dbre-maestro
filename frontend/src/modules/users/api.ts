import { apiClient } from '@/shared/api/client'
import type { AuthGroup } from '@/shared/types/auth'
import type { DBConnection } from '@/shared/types/dbConnection'
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

export function listUsers() {
  return apiClient.get<ListUsersResponse>('/users').then((response): ListUsersResponse => ({
    ...response,
    users: Array.isArray(response.users)
      ? response.users.map((user) => ({
          ...user,
          lark_recipient: typeof user.lark_recipient === 'string' ? user.lark_recipient : '',
          auth_groups: Array.isArray(user.auth_groups) ? user.auth_groups : [],
          permissions: Array.isArray(user.permissions) ? user.permissions : [],
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
