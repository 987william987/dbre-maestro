export type MaskingRule = {
  id: number
  db_connection_id?: number | null
  table_name: string
  column_name: string
  mask_mode: 'full' | 'partial' | 'hash'
  created_by: number
  created_at: string
}
