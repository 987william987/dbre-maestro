import { apiClient } from '@/shared/api/client'
import type { NotificationListResponse, NotificationSummary } from '@/shared/types/notification'

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

export async function listNotificationSummary() {
  return apiClient.get<NotificationSummary>('/notifications/summary').then((response) => ({
    pending: typeof response.pending === 'number' ? response.pending : 0,
    review_required: typeof response.review_required === 'number' ? response.review_required : 0,
    execution_required: typeof response.execution_required === 'number' ? response.execution_required : 0,
  }))
}
