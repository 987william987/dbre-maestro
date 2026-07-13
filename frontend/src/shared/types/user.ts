import type { AuthGroup } from '@/shared/types/auth'

export type UserSummary = {
  id: number
  username: string
  email: string
  lark_recipient: string
  auth_groups: AuthGroup[]
  permissions?: string[]
  direct_permissions?: string[]
  db_connection_ids?: number[]
  protected: boolean
  is_active: boolean
  created_at: string
  updated_at: string
}

export type UserMembership = {
  id: number
  user_id: number
  auth_group: AuthGroup
  granted_by: number | null
  expires_at: string | null
  created_at: string
}

export type UserDetail = {
  id: number
  username: string
  email: string
  lark_recipient: string
  protected: boolean
  is_active: boolean
  created_at: string
  updated_at: string
  memberships: UserMembership[]
  permissions?: string[]
  db_connection_ids?: number[]
  direct_permissions?: string[]
  direct_db_connection_ids?: number[]
}
