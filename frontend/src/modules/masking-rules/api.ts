import { apiClient } from '@/shared/api/client'
import type { MaskingRule } from '@/shared/types/maskingRule'

type MaskingRulesResponse = {
  rules: MaskingRule[]
}

type CreateMaskingRulePayload = {
  db_connection_id?: number | null
  table_name: string
  column_name: string
  mask_mode: 'full' | 'partial' | 'hash'
}

export async function listMaskingRules() {
  const response = await apiClient.get<MaskingRulesResponse>('/masking-rules')
  return {
    ...response,
    rules: Array.isArray(response.rules) ? response.rules : [],
  }
}

export function createMaskingRule(payload: CreateMaskingRulePayload) {
  return apiClient.post<MaskingRule>('/masking-rules', payload)
}

export function deleteMaskingRule(id: number) {
  return apiClient.delete<void>(`/masking-rules/${id}`)
}
