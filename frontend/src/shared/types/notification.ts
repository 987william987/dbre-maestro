export type NotificationItem = {
  id: number
  user_id: number
  type: string
  title: string
  body: string
  resource_type?: string | null
  resource_id?: number | null
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
