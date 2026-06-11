export type QueryResult = {
  columns: string[]
  rows: Array<Array<string | number | boolean | null>>
  row_count: number
  duration_ms: number
  sensitive_override_active?: boolean
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
