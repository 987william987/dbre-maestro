import { apiClient } from '@/shared/api/client'
import type { MetadataColumn, MetadataTable, QueryResult } from '@/shared/types/sqlEditor'

type QueryPayload = {
  db_connection_id: number
  sql: string
  limit?: number
}

type TablesResponse = {
  tables: MetadataTable[]
}

type ColumnsResponse = {
  schema: string
  table: string
  columns: MetadataColumn[]
}

export function executeQuery(payload: QueryPayload) {
  return apiClient.post<QueryResult>('/query', payload)
}

export async function listMetadataTables(connectionId: number) {
  const response = await apiClient.get<TablesResponse>(`/db-connections/${connectionId}/metadata`)
  return {
    ...response,
    tables: Array.isArray(response.tables) ? response.tables : [],
  }
}

export async function listMetadataColumns(connectionId: number, schema: string, table: string) {
  const encodedSchema = encodeURIComponent(schema)
  const encodedTable = encodeURIComponent(table)
  const response = await apiClient.get<ColumnsResponse>(
    `/db-connections/${connectionId}/metadata/${encodedSchema}/${encodedTable}/columns`,
  )

  return {
    ...response,
    columns: Array.isArray(response.columns) ? response.columns : [],
  }
}
