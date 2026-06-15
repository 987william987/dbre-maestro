import { apiClient } from '@/shared/api/client'
import type { MaskingRule, MaskingWhitelist } from '@/shared/types/maskingRule'

type MaskingRulesResponse = {
  rules: MaskingRule[]
}

type MaskingWhitelistResponse = {
  whitelist: MaskingWhitelist[]
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
  table_name: string
  column_name: string
}

export async function listMaskingRules() {
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

export async function listMaskingWhitelists() {
  const response = await apiClient.get<MaskingWhitelistResponse>('/masking-whitelist')
  return {
    ...response,
    whitelist: Array.isArray(response.whitelist) ? response.whitelist : [],
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
