import { apiClient } from '@/shared/api/client'
import type { SQLReviewRule } from '@/shared/types/sqlReviewRule'

type SQLReviewRulesResponse = {
  rules: SQLReviewRule[]
}

type PatchSQLReviewRulePayload = {
  enabled?: boolean
  threshold?: number | null
}

export async function listSQLReviewRules() {
  const response = await apiClient.get<SQLReviewRulesResponse>('/sql-review-rules')
  return {
    ...response,
    rules: Array.isArray(response.rules) ? response.rules : [],
  }
}

export function patchSQLReviewRule(name: string, payload: PatchSQLReviewRulePayload) {
  return apiClient.patch<SQLReviewRule>(`/sql-review-rules/${encodeURIComponent(name)}`, payload)
}
