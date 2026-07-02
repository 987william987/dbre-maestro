import { apiClient } from '@/shared/api/client'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { MaskingRule, MaskingWhitelist, RedisSensitiveKeyPrefix } from '@/shared/types/maskingRule'
import type { MetadataColumn, MetadataResponse } from '@/shared/types/sqlEditor'

type MaskingRulesResponse = {
  rules: MaskingRule[]
}

type MaskingWhitelistResponse = {
  whitelist: MaskingWhitelist[]
}

type RedisSensitiveKeyPrefixesResponse = {
  prefixes: RedisSensitiveKeyPrefix[]
}

type CreateMaskingRulePayload = {
  column_name: string
  match_type: 'exact' | 'regex'
  mask_mode: 'full' | 'partial' | 'hash' | 'email' | 'fixed' | 'numeric' | 'datetime' | 'ip'
  mask_config?: Record<string, unknown>
}

type CreateMaskingWhitelistPayload = {
  db_connection_id: number
  database_name: string
  schema_name?: string
  table_name: string
  column_name: string
}

type RedisSensitiveKeyPrefixPayload = {
  db_connection_id: number
  redis_db_index?: number | null
  key_prefix: string
  reason?: string | null
  is_active: boolean
}

type MaskingConnectionsResponse = {
  connections: DBConnection[]
}

type ColumnsResponse = {
  database?: string
  schema: string
  table: string
  columns: MetadataColumn[]
}

export async function listMaskingRules(): Promise<MaskingRulesResponse> {
  const response = await apiClient.get<MaskingRulesResponse>('/masking-rules')
  return {
    ...response,
    rules: Array.isArray(response.rules)
      ? response.rules.map((rule) => ({
          ...rule,
          match_type: rule.match_type === 'regex' ? 'regex' : 'exact',
          mask_config: rule.mask_config && typeof rule.mask_config === 'object' && !Array.isArray(rule.mask_config) ? rule.mask_config : {},
        }))
      : [],
  }
}

export function createMaskingRule(payload: CreateMaskingRulePayload) {
  return apiClient.post<MaskingRule>('/masking-rules', payload)
}

export function patchMaskingRule(id: number, payload: CreateMaskingRulePayload) {
  return apiClient.patch<MaskingRule>(`/masking-rules/${id}`, payload)
}

export function deleteMaskingRule(id: number) {
  return apiClient.delete<void>(`/masking-rules/${id}`)
}

export async function listRedisSensitiveKeyPrefixes() {
  const response = await apiClient.get<RedisSensitiveKeyPrefixesResponse>('/masking-rules/redis-prefixes')
  return {
    ...response,
    prefixes: Array.isArray(response.prefixes) ? response.prefixes : [],
  }
}

export function createRedisSensitiveKeyPrefix(payload: RedisSensitiveKeyPrefixPayload) {
  return apiClient.post<RedisSensitiveKeyPrefix>('/masking-rules/redis-prefixes', payload)
}

export function patchRedisSensitiveKeyPrefix(id: number, payload: RedisSensitiveKeyPrefixPayload) {
  return apiClient.patch<RedisSensitiveKeyPrefix>(`/masking-rules/redis-prefixes/${id}`, payload)
}

export function deleteRedisSensitiveKeyPrefix(id: number) {
  return apiClient.delete<void>(`/masking-rules/redis-prefixes/${id}`)
}

export async function listMaskingWhitelists() {
  const response = await apiClient.get<MaskingWhitelistResponse>('/masking-whitelist')
  return {
    ...response,
    whitelist: Array.isArray(response.whitelist) ? response.whitelist : [],
  }
}

export function listMaskingConnections() {
  return apiClient.get<MaskingConnectionsResponse>('/masking-whitelist/connections').then((response) => ({
    ...response,
    connections: Array.isArray(response.connections) ? response.connections : [],
  }))
}

type MaskingMetadataParams = {
  database?: string
  schema?: string
}

export async function listMaskingMetadata(connectionId: number, params?: MaskingMetadataParams) {
  const searchParams = new URLSearchParams()
  if (params?.database) {
    searchParams.set('database', params.database)
  }
  if (params?.schema) {
    searchParams.set('schema', params.schema)
  }
  const query = searchParams.toString()
  const response = await apiClient.get<MetadataResponse>(
    `/masking-whitelist/connections/${connectionId}/metadata${query ? `?${query}` : ''}`,
  )
  return {
    ...response,
    items: Array.isArray(response.items) ? response.items : [],
  }
}

export async function listMaskingMetadataColumns(connectionId: number, schema: string, table: string, database?: string) {
  const encodedSchema = encodeURIComponent(schema)
  const encodedTable = encodeURIComponent(table)
  const query = database ? `?database=${encodeURIComponent(database)}` : ''
  const response = await apiClient.get<ColumnsResponse>(
    `/masking-whitelist/connections/${connectionId}/metadata/${encodedSchema}/${encodedTable}/columns${query}`,
  )
  return {
    ...response,
    columns: Array.isArray(response.columns) ? response.columns : [],
  }
}

export function createMaskingWhitelist(payload: CreateMaskingWhitelistPayload) {
  return apiClient.post<MaskingWhitelist>('/masking-whitelist', payload)
}

export function patchMaskingWhitelist(id: number, payload: CreateMaskingWhitelistPayload) {
  return apiClient.patch<MaskingWhitelist>(`/masking-whitelist/${id}`, payload)
}

export function deleteMaskingWhitelist(id: number) {
  return apiClient.delete<void>(`/masking-whitelist/${id}`)
}
