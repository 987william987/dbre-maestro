export type InventorySnapshot = {
  id: number
  snapshot_at: string
  provider: string
  engine: string
  region: string
  az?: string | null
  db_identifier: string
  cluster_identifier?: string | null
  instance_identifier?: string | null
  role?: string | null
  engine_version?: string | null
  instance_class?: string | null
  storage_type?: string | null
  cluster_endpoint?: string | null
  cluster_reader_endpoint?: string | null
  instance_endpoint?: string | null
  mapping_status: 'matched' | 'unmatched' | 'ambiguous' | string
  mapping_connections?: string[]
}

export type DBObjectSnapshot = {
  id: number
  snapshot_at: string
  db_connection_id: number
  connection_name: string
  engine: string
  cluster_name?: string | null
  node_name?: string | null
  database_name: string
  schema_name: string
  table_name: string
  data_size_bytes: number
  index_size_bytes: number
}
