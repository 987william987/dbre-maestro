export type DBConnectionCredential = {
  id: number
  db_connection_id: number
  credential_role: 'readonly' | 'readwrite' | string
  username: string
  encryption_key_version: number
  created_at: string
  updated_at: string
  has_password: boolean
}

export type DBConnection = {
  id: number
  name: string
  db_type: string
  host: string
  port: number
  readonly_host?: string
  readonly_port?: number
  readwrite_host?: string
  readwrite_port?: number
  database_name?: string | null
  username: string
  encryption_key_version: number
  ssl_mode: string
  extra_params?: string | null
  last_test_status?: 'passed' | 'failed' | string | null
  last_test_error?: string | null
  last_tested_at?: string | null
  created_by: number
  created_at: string
  updated_at: string
  credentials?: DBConnectionCredential[]
}

export type ResourceBoundUser = {
  id: number
  username: string
}

export type ResourceBoundAuthGroup = {
  id: number
  group_key: string
  name: string
}

export type DBConnectionBindings = {
  db_connection_id: number
  direct_users: ResourceBoundUser[]
  effective_users: ResourceBoundUser[]
  auth_groups: ResourceBoundAuthGroup[]
}
