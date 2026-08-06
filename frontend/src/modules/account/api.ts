import { apiClient } from '@/shared/api/client'
import type { DashboardDBScope, DashboardQueryAccessScope } from '@/modules/dashboard/api'

export type AccountSession = {
  id: number
  user_id: number
  user_agent?: string | null
  ip_address?: string | null
  expires_at: string
  revoked_at?: string | null
  created_at: string
  is_current?: boolean
}

export async function listAccountSessions() {
  return apiClient.get<{ sessions: AccountSession[]; current_session_id?: number }>('/auth/sessions').then((response) => {
    const currentSessionID = typeof response.current_session_id === 'number' ? response.current_session_id : 0
    return {
      sessions: Array.isArray(response.sessions)
        ? response.sessions.map((session) => ({
            ...session,
            is_current: currentSessionID > 0 && session.id === currentSessionID,
          }))
        : [],
      current_session_id: currentSessionID,
    }
  })
}

export async function revokeAccountSession(id: number) {
  return apiClient.delete<void>(`/auth/sessions/${id}`)
}

export async function revokeAllAccountSessions() {
  return apiClient.delete<void>('/auth/sessions')
}

export type AccountAccessScopesResponse = {
  db_scopes: DashboardDBScope[]
  query_access_scopes: DashboardQueryAccessScope[]
  db_scope_count: number
  query_scope_count: number
}

export async function getAccountAccessScopes() {
  return apiClient.get<AccountAccessScopesResponse>('/account/access-scopes').then((response) => ({
    db_scopes: Array.isArray(response.db_scopes) ? response.db_scopes : [],
    query_access_scopes: Array.isArray(response.query_access_scopes) ? response.query_access_scopes : [],
    db_scope_count: response.db_scope_count ?? 0,
    query_scope_count: response.query_scope_count ?? 0,
  }))
}
