import { apiClient } from '@/shared/api/client'
import type { NotificationListResponse } from '@/shared/types/notification'

export async function listNotifications(limit = 20, offset = 0) {
  return apiClient
    .get<NotificationListResponse>(`/notifications?limit=${limit}&offset=${offset}`)
    .then((response) => ({
      ...response,
      notifications: Array.isArray(response.notifications) ? response.notifications : [],
    }))
}

export async function markNotificationRead(id: number) {
  return apiClient.post<void>(`/notifications/${id}/read`)
}

export async function markAllNotificationsRead() {
  return apiClient.post<void>('/notifications/read-all')
}
