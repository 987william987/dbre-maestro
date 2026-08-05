import { apiClient } from '@/shared/api/client'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { Ticket } from '@/shared/types/ticket'

export type DashboardCount = {
  key: string
  count: number
}

export type DashboardTicketSummary = {
  total: number
  completed: number
  failed: number
  active: number
  by_type: DashboardCount[]
  by_status: DashboardCount[]
}

export type DashboardDBScope = {
  id: number
  name: string
  db_type: string
}

export type DashboardQueryAccessScope = {
  id: number
  connection_id: number
  connection_name: string
  subject_type: string
  effect: string
  database_pattern: string
  table_pattern: string
  expires_at?: string | null
  remaining_days?: number | null
  source_ticket_id?: number | null
  source_ticket_no?: string | null
  expiring_soon: boolean
  renew_ticket_path: string
}

export type DashboardPlatform = {
  ticket_summary: DashboardTicketSummary
  recent_attention: Ticket[]
  db_connection_failures: DBConnection[]
}

export type DashboardResponse = {
  personal: {
    ticket_summary: DashboardTicketSummary
    active_tickets: Ticket[]
    recent_tickets: Ticket[]
    db_scopes: DashboardDBScope[]
    query_access_scopes: DashboardQueryAccessScope[]
  }
  platform: DashboardPlatform | null
}

function normalizeTicketSummary(summary?: Partial<DashboardTicketSummary> | null): DashboardTicketSummary {
  return {
    total: summary?.total ?? 0,
    completed: summary?.completed ?? 0,
    failed: summary?.failed ?? 0,
    active: summary?.active ?? 0,
    by_type: Array.isArray(summary?.by_type) ? summary.by_type : [],
    by_status: Array.isArray(summary?.by_status) ? summary.by_status : [],
  }
}

export async function getDashboard() {
  const response = await apiClient.get<DashboardResponse>('/dashboard')
  return {
    personal: {
      ticket_summary: normalizeTicketSummary(response.personal?.ticket_summary),
      active_tickets: Array.isArray(response.personal?.active_tickets) ? response.personal.active_tickets : [],
      recent_tickets: Array.isArray(response.personal?.recent_tickets) ? response.personal.recent_tickets : [],
      db_scopes: Array.isArray(response.personal?.db_scopes) ? response.personal.db_scopes : [],
      query_access_scopes: Array.isArray(response.personal?.query_access_scopes) ? response.personal.query_access_scopes : [],
    },
    platform: response.platform
      ? {
          ticket_summary: normalizeTicketSummary(response.platform.ticket_summary),
          recent_attention: Array.isArray(response.platform.recent_attention) ? response.platform.recent_attention : [],
          db_connection_failures: Array.isArray(response.platform.db_connection_failures) ? response.platform.db_connection_failures : [],
        }
      : null,
  }
}
