export type MaskingRule = {
  id: number
  column_name: string
  mask_mode: 'full' | 'partial' | 'hash'
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
