export type QueryResult = {
  columns: string[]
  raw_columns?: string[]
  sensitive_column_indexes?: number[]
  rows: Array<Array<string | number | boolean | null>>
  row_count: number
  duration_ms: number
  sensitive_override_active?: boolean
  query_context_token?: string
}

export type QueryHistoryEntry = {
  id: number
  db_connection_id: number
  db_connection_name: string
  database_name?: string | null
  schema_name?: string | null
  redis_db_index?: number | null
  sql_content: string
  duration_ms: number
  created_at: string
}

export type SavedQuery = {
  id: number
  label: string
  db_connection_id: number
  db_connection_name: string
  database_name?: string | null
  schema_name?: string | null
  redis_db_index?: number | null
  sql_content: string
  created_at: string
  updated_at: string
}

export type MetadataLevel = 'database' | 'schema' | 'table' | 'redis_db'

export type MetadataItemKind = 'database' | 'schema' | 'table' | 'redis_db'

export type MetadataItem = {
  kind: MetadataItemKind
  name: string
  database?: string
  schema?: string
  engine?: string
  row_count?: number
  data_size_bytes?: number
  index_size_bytes?: number
  comment?: string
}

export type MetadataResponse = {
  db_type: string
  level: MetadataLevel
  database?: string
  schema?: string
  items: MetadataItem[]
}

export type MetadataColumn = {
  name: string
  data_type: string
  column_type: string
  is_nullable: string
  default?: string
  comment: string
}

export type MetadataDefinition = {
  database?: string
  schema: string
  table: string
  definition: string
}
