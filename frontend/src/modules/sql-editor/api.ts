import { apiClient } from '@/shared/api/client'
import type { DBConnection } from '@/shared/types/dbConnection'
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

type QueryConnectionsResponse = {
  connections: DBConnection[]
}

type QueryConstraintsResponse = {
  default_limit: number
  max_limit: number
  app_timeout_seconds: number
  mysql_max_execution_time_ms: number
  postgres_statement_timeout_ms: number
}

export function executeQuery(payload: QueryPayload): Promise<QueryResult> {
  return apiClient.post<QueryResult>('/query', payload).then((response) => ({
    ...response,
    columns: Array.isArray(response.columns) ? response.columns : [],
    raw_columns: Array.isArray(response.raw_columns) ? response.raw_columns : [],
    sensitive_column_indexes: Array.isArray(response.sensitive_column_indexes) ? response.sensitive_column_indexes : [],
    query_context_token: typeof response.query_context_token === 'string' ? response.query_context_token : '',
    rows: Array.isArray(response.rows)
      ? response.rows.map((row) => (Array.isArray(row) ? row : []))
      : [],
  }))
}

export function listQueryConnections() {
  return apiClient.get<QueryConnectionsResponse>('/query/connections').then((response) => ({
    ...response,
    connections: Array.isArray(response.connections) ? response.connections : [],
  }))
}

export function getQueryConstraints() {
  return apiClient.get<QueryConstraintsResponse>('/query/constraints').then((response) => ({
    default_limit: typeof response.default_limit === 'number' ? response.default_limit : 200,
    max_limit: typeof response.max_limit === 'number' ? response.max_limit : 1000,
    app_timeout_seconds: typeof response.app_timeout_seconds === 'number' ? response.app_timeout_seconds : 30,
    mysql_max_execution_time_ms:
      typeof response.mysql_max_execution_time_ms === 'number' ? response.mysql_max_execution_time_ms : 25000,
    postgres_statement_timeout_ms:
      typeof response.postgres_statement_timeout_ms === 'number' ? response.postgres_statement_timeout_ms : 25000,
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
  query_context_token?: string
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
