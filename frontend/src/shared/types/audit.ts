export type AuditLog = {
  id: number
  actor_id?: number | null
  actor_name: string
  action_type: string
  resource_type?: string | null
  resource_id?: number | null
  details?: unknown
  ip_address?: string | null
  created_at: string
}
