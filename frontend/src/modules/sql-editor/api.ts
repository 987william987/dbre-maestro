import { apiClient } from '@/shared/api/client'
import type { MetadataColumn, MetadataResponse, QueryResult } from '@/shared/types/sqlEditor'

type QueryPayload = {
  db_connection_id: number
  sql: string
  limit?: number
  database?: string
  schema?: string
  redis_db_index?: number
}

type ColumnsResponse = {
  database?: string
  schema: string
  table: string
  columns: MetadataColumn[]
}

type LegacyTablesResponse = {
  tables?: Array<{
    schema: string
    name: string
    engine?: string
    row_count?: number
    data_size_bytes?: number
    index_size_bytes?: number
    comment?: string
  }>
}

export function executeQuery(payload: QueryPayload) {
  return apiClient.post<QueryResult>('/query', payload).then((response) => ({
    ...response,
    columns: Array.isArray(response.columns) ? response.columns : [],
    rows: Array.isArray(response.rows)
      ? response.rows.map((row) => (Array.isArray(row) ? row : []))
      : [],
  }))
}

type MetadataParams = {
  database?: string
  schema?: string
}

export async function listMetadata(connectionId: number, params?: MetadataParams) {
  const searchParams = new URLSearchParams()
  if (params?.database) {
    searchParams.set('database', params.database)
  }
  if (params?.schema) {
    searchParams.set('schema', params.schema)
  }

  const query = searchParams.toString()
  const response = await apiClient.get<MetadataResponse & LegacyTablesResponse>(
    `/db-connections/${connectionId}/metadata${query ? `?${query}` : ''}`,
  )

  if (Array.isArray(response.tables) && response.level === undefined) {
    return {
      db_type: 'mysql',
      level: 'table' as const,
      database: params?.database,
      schema: params?.schema,
      items: response.tables.map((table) => ({
        kind: 'table' as const,
        name: table.name,
        database: params?.database || table.schema,
        schema: table.schema,
        engine: table.engine,
        row_count: table.row_count,
        data_size_bytes: table.data_size_bytes,
        index_size_bytes: table.index_size_bytes,
        comment: table.comment,
      })),
    }
  }

  return {
    ...response,
    items: Array.isArray(response.items) ? response.items : [],
  }
}

export async function listMetadataColumns(connectionId: number, schema: string, table: string, database?: string) {
  const encodedSchema = encodeURIComponent(schema)
  const encodedTable = encodeURIComponent(table)
  const query = database ? `?database=${encodeURIComponent(database)}` : ''
  const response = await apiClient.get<ColumnsResponse>(
    `/db-connections/${connectionId}/metadata/${encodedSchema}/${encodedTable}/columns${query}`,
  )

  return {
    ...response,
    columns: Array.isArray(response.columns) ? response.columns : [],
  }
}
