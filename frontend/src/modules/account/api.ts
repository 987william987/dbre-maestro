import { apiClient } from '@/shared/api/client'

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
