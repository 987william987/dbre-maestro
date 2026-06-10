import { apiClient } from '@/shared/api/client'
import type { AuthGroup } from '@/shared/types/auth'
import type { AuthGroupDetail, AuthGroupSummary } from '@/shared/types/authGroup'

type AuthGroupsResponse = {
  auth_groups: AuthGroupSummary[]
}

type CreateAuthGroupPayload = {
  name: string
  description: string
  user_ids?: number[]
  permissions?: string[]
  db_connection_ids?: number[]
}

type PatchAuthGroupPayload = {
  name?: string
  description?: string
  user_ids?: number[]
  permissions?: string[]
  db_connection_ids?: number[]
}

export function listAuthGroups() {
  return apiClient.get<AuthGroupsResponse>('/auth-groups').then((response): AuthGroupsResponse => ({
    ...response,
    auth_groups: Array.isArray(response.auth_groups)
      ? response.auth_groups.map((group) => ({
          ...group,
          permissions: Array.isArray(group.permissions) ? group.permissions : [],
          db_connection_ids: Array.isArray(group.db_connection_ids) ? group.db_connection_ids : [],
        }))
      : [],
  }))
}

export function getAuthGroup(group: AuthGroup) {
  return apiClient.get<AuthGroupDetail>(`/auth-groups/${group}`).then((detail): AuthGroupDetail => ({
    ...detail,
    users: Array.isArray(detail.users) ? detail.users : [],
    permissions: Array.isArray(detail.permissions) ? detail.permissions : [],
    db_connection_ids: Array.isArray(detail.db_connection_ids) ? detail.db_connection_ids : [],
  }))
}

export function createAuthGroup(payload: CreateAuthGroupPayload) {
  return apiClient.post<AuthGroupDetail>('/auth-groups', payload)
}

export function patchAuthGroup(group: AuthGroup, payload: PatchAuthGroupPayload) {
  return apiClient.patch<AuthGroupDetail>(`/auth-groups/${group}`, payload)
}

export function deleteAuthGroup(group: AuthGroup) {
  return apiClient.delete<void>(`/auth-groups/${group}`)
}

export function addAuthGroupPermission(group: AuthGroup, permissionKey: string) {
  return apiClient.post<void>(`/auth-groups/${group}/permissions`, { permission_key: permissionKey })
}

export function removeAuthGroupPermission(group: AuthGroup, permissionKey: string) {
  return apiClient.delete<void>(`/auth-groups/${group}/permissions/${encodeURIComponent(permissionKey)}`)
}

export function addAuthGroupDBConnection(group: AuthGroup, dbConnectionId: number) {
  return apiClient.post<void>(`/auth-groups/${group}/db-connections`, { db_connection_id: dbConnectionId })
}

export function removeAuthGroupDBConnection(group: AuthGroup, dbConnectionId: number) {
  return apiClient.delete<void>(`/auth-groups/${group}/db-connections/${dbConnectionId}`)
}
