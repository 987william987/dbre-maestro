export type NotificationItem = {
  id: number
  user_id: number
  type: string
  title: string
  body: string
  resource_type?: string | null
  resource_id?: number | null
  resource_ref?: string | null
  is_read: boolean
  created_at: string
}

export type NotificationListResponse = {
  notifications: NotificationItem[]
  total: number
  unread: number
  limit: number
  offset: number
}

export type NotificationSummary = {
  pending: number
  review_required: number
  execution_required: number
}
