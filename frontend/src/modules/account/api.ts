import { apiClient } from '@/shared/api/client'

export type AccountSession = {
  id: number
  user_id: number
  user_agent?: string | null
  ip_address?: string | null
  expires_at: string
  revoked_at?: string | null
  created_at: string
}

export async function listAccountSessions() {
  return apiClient.get<{ sessions: AccountSession[] }>('/auth/sessions').then((response) => ({
    sessions: Array.isArray(response.sessions) ? response.sessions : [],
  }))
}

export async function revokeAccountSession(id: number) {
  return apiClient.delete<void>(`/auth/sessions/${id}`)
}

export async function revokeAllAccountSessions() {
  return apiClient.delete<void>('/auth/sessions')
}
