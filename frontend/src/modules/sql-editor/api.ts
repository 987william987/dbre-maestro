import { apiClient } from '@/shared/api/client'
import type { MetadataColumn, MetadataDefinition, MetadataResponse, QueryHistoryEntry, QueryResult, SavedQuery } from '@/shared/types/sqlEditor'

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
    raw_columns: Array.isArray(response.raw_columns) ? response.raw_columns : [],
    sensitive_column_indexes: Array.isArray(response.sensitive_column_indexes) ? response.sensitive_column_indexes : [],
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

export async function listMetadataDefinition(connectionId: number, schema: string, table: string, database?: string) {
  const encodedSchema = encodeURIComponent(schema)
  const encodedTable = encodeURIComponent(table)
  const query = database ? `?database=${encodeURIComponent(database)}` : ''
  return apiClient.get<MetadataDefinition>(
    `/db-connections/${connectionId}/metadata/${encodedSchema}/${encodedTable}/definition${query}`,
  )
}

type QueryHistoryResponse = {
  history: QueryHistoryEntry[]
}

type SavedQueriesResponse = {
  saved_queries: SavedQuery[]
}

type CreateSavedQueryPayload = {
  label: string
  db_connection_id: number
  database?: string
  schema?: string
  redis_db_index?: number
  sql_content: string
}

type CreateSensitiveAccessPayload = {
  db_connection_id: number
  sql_content: string
  database_name?: string
  schema_name?: string
  approved_duration_minutes?: number
}

export type CreateSensitiveAccessResponse = {
  ticket_id: number
  ticket_no: string
  status: 'pending_review' | 'approved' | 'rejected'
  scope_count: number
}

export function listQueryHistory(limit = 20) {
  return apiClient.get<QueryHistoryResponse>(`/query/history?limit=${limit}`).then((response) => ({
    history: Array.isArray(response.history) ? response.history : [],
  }))
}

export function listSavedQueries() {
  return apiClient.get<SavedQueriesResponse>('/query/saved-queries').then((response) => ({
    saved_queries: Array.isArray(response.saved_queries) ? response.saved_queries : [],
  }))
}

export function createSavedQuery(payload: CreateSavedQueryPayload) {
  return apiClient.post<SavedQuery>('/query/saved-queries', payload)
}

export function deleteSavedQuery(id: number) {
  return apiClient.delete<void>(`/query/saved-queries/${id}`)
}

export function createSensitiveAccessTicket(payload: CreateSensitiveAccessPayload) {
  return apiClient.post<CreateSensitiveAccessResponse>('/query/sensitive-access', payload)
}
