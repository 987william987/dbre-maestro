export type DBConnection = {
  id: number
  name: string
  db_type: string
  host: string
  port: number
  database_name?: string | null
  username: string
  encryption_key_version: number
  ssl_mode: string
  extra_params?: string | null
  created_by: number
  created_at: string
  updated_at: string
}
