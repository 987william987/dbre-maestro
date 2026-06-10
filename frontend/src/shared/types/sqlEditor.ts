export type QueryResult = {
  columns: string[]
  rows: Array<Array<string | number | boolean | null>>
  row_count: number
  duration_ms: number
}

export type MetadataTable = {
  schema: string
  name: string
  engine: string
  row_count: number
  data_size_bytes: number
  index_size_bytes: number
  comment: string
}

export type MetadataColumn = {
  name: string
  data_type: string
  column_type: string
  is_nullable: string
  default?: string
  comment: string
}
