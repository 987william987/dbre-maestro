export type SQLReviewRule = {
  id: number
  rule_name: string
  category?: string
  severity?: 'error' | 'warning'
  enabled: boolean
  threshold?: number | null
  description: string
  updated_by?: number | null
  updated_at: string
}
