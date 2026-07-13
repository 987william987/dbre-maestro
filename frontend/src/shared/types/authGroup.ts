export type AuthGroupSummary = {
  id?: number
  name: string
  label: string
  description: string
  system_defined: boolean
  protected?: boolean
  all_permissions?: boolean
  user_count: number
  resource_group_count?: number
  permission_count?: number
  db_connection_count?: number
  permissions?: string[]
  db_connection_ids?: number[]
  created_at?: string
  updated_at?: string
}

export type AuthGroupUser = {
  id: number
  username: string
  email: string
  created_at: string
  updated_at: string
  protected: boolean
}

export type AuthGroupDetail = {
  id?: number
  name: string
  label: string
  description: string
  system_defined: boolean
  protected?: boolean
  all_permissions?: boolean
  users: AuthGroupUser[]
  permissions?: string[]
  db_connection_ids?: number[]
  created_at?: string
  updated_at?: string
}
