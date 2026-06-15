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
  table_name: string
  column_name: string
  created_by: number
  created_at: string
}
