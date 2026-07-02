export type MaskingRule = {
  id: number
  column_name: string
  match_type: 'exact' | 'regex'
  mask_mode: 'full' | 'partial' | 'hash' | 'email' | 'fixed' | 'numeric' | 'datetime' | 'ip'
  mask_config: Record<string, unknown> | null
  created_by: number
  created_at: string
}

export type MaskingWhitelist = {
  id: number
  db_connection_id: number
  database_name: string
  schema_name?: string
  table_name: string
  column_name: string
  created_by: number
  created_at: string
}

export type RedisSensitiveKeyPrefix = {
  id: number
  db_connection_id: number
  redis_db_index?: number | null
  key_prefix: string
  reason?: string | null
  is_active: boolean
  created_by?: number | null
  created_at: string
  updated_at: string
}
