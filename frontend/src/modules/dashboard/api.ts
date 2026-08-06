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

export type DashboardUserCount = {
  user_id?: number | null
  username?: string | null
  count: number
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
  granted_via: string
  expires_at?: string | null
  remaining_days?: number | null
  source_ticket_id?: number | null
  source_ticket_no?: string | null
  expiring_soon: boolean
  renew_ticket_path: string
}

export type DashboardQueueStats = {
  pending_review: number
  pending_execution: number
  executing: number
  needs_admin_attention: number
  failed_today: number
  failed_7d: number
  long_pending: number
}

export type DashboardAgingStats = {
  avg_review_age_minutes?: number | null
  avg_execution_wait_age_minutes?: number | null
  avg_execution_duration_ms?: number | null
  avg_review_duration_7d_minutes?: number | null
  avg_execution_wait_7d_minutes?: number | null
  avg_completion_time_7d_minutes?: number | null
  max_review_age_minutes?: number | null
  max_execution_wait_age_minutes?: number | null
}

export type DashboardExecutionRiskStats = {
  recent_failed: number
  manually_stopped: number
  service_shutdown: number
  outcome_unknown: number
  not_sent: number
  db_explicit_error: number
  by_outcome: DashboardCount[]
  by_interruption: DashboardCount[]
}

export type DashboardAccessGovernanceStats = {
  expiring_soon: number
  long_lived: number
  never_expires: number
  recently_revoked: number
  sensitive_requests_7d: number
  sql_export_requests_7d: number
  active_rules: number
}

export type DashboardMetadataJob = {
  job_name: string
  last_scheduled_at?: string | null
  last_started_at?: string | null
  last_finished_at?: string | null
  last_success_at?: string | null
  status: string
  error_message?: string | null
  updated_at: string
}

export type DashboardDBMetadataHealth = {
  db_type_counts: DashboardCount[]
  enabled_metadata_connection_count: number
  object_snapshot_connection_count: number
  stale_object_connection_count: number
  object_count: number
  inventory_job?: DashboardMetadataJob | null
  object_job?: DashboardMetadataJob | null
  object_sync_failed: boolean
}

export type DashboardNotificationHealth = {
  lark_failed_7d: number
  interactive_callback_failed_7d: number
  retry_or_failure_7d: number
  missing_lark_recipient_7d: number
  recipient_conflict_7d: number
  by_type: DashboardCount[]
}

export type DashboardTopUsage = {
  submitters: DashboardUserCount[]
  db_connections_by_tickets: DashboardCount[]
  failed_db_connections: DashboardCount[]
  sql_exports_by_user: DashboardUserCount[]
}

export type DashboardPlatform = {
  ticket_summary: DashboardTicketSummary
  queue: DashboardQueueStats
  aging: DashboardAgingStats
  execution_risk: DashboardExecutionRiskStats
  access_governance: DashboardAccessGovernanceStats
  db_metadata_health: DashboardDBMetadataHealth
  notification_health: DashboardNotificationHealth
  top_usage: DashboardTopUsage
  recent_attention: Ticket[]
  long_pending_tickets: Ticket[]
  recent_failed_tickets: Ticket[]
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

const emptyQueue: DashboardQueueStats = {
  pending_review: 0,
  pending_execution: 0,
  executing: 0,
  needs_admin_attention: 0,
  failed_today: 0,
  failed_7d: 0,
  long_pending: 0,
}

const emptyExecutionRisk: DashboardExecutionRiskStats = {
  recent_failed: 0,
  manually_stopped: 0,
  service_shutdown: 0,
  outcome_unknown: 0,
  not_sent: 0,
  db_explicit_error: 0,
  by_outcome: [],
  by_interruption: [],
}

const emptyAccessGovernance: DashboardAccessGovernanceStats = {
  expiring_soon: 0,
  long_lived: 0,
  never_expires: 0,
  recently_revoked: 0,
  sensitive_requests_7d: 0,
  sql_export_requests_7d: 0,
  active_rules: 0,
}

const emptyDBMetadataHealth: DashboardDBMetadataHealth = {
  db_type_counts: [],
  enabled_metadata_connection_count: 0,
  object_snapshot_connection_count: 0,
  stale_object_connection_count: 0,
  object_count: 0,
  object_sync_failed: false,
}

const emptyNotificationHealth: DashboardNotificationHealth = {
  lark_failed_7d: 0,
  interactive_callback_failed_7d: 0,
  retry_or_failure_7d: 0,
  missing_lark_recipient_7d: 0,
  recipient_conflict_7d: 0,
  by_type: [],
}

const emptyTopUsage: DashboardTopUsage = {
  submitters: [],
  db_connections_by_tickets: [],
  failed_db_connections: [],
  sql_exports_by_user: [],
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
          queue: { ...emptyQueue, ...response.platform.queue },
          aging: response.platform.aging ?? {},
          execution_risk: {
            ...emptyExecutionRisk,
            ...response.platform.execution_risk,
            by_outcome: Array.isArray(response.platform.execution_risk?.by_outcome) ? response.platform.execution_risk.by_outcome : [],
            by_interruption: Array.isArray(response.platform.execution_risk?.by_interruption) ? response.platform.execution_risk.by_interruption : [],
          },
          access_governance: { ...emptyAccessGovernance, ...response.platform.access_governance },
          db_metadata_health: {
            ...emptyDBMetadataHealth,
            ...response.platform.db_metadata_health,
            db_type_counts: Array.isArray(response.platform.db_metadata_health?.db_type_counts) ? response.platform.db_metadata_health.db_type_counts : [],
          },
          notification_health: {
            ...emptyNotificationHealth,
            ...response.platform.notification_health,
            by_type: Array.isArray(response.platform.notification_health?.by_type) ? response.platform.notification_health.by_type : [],
          },
          top_usage: {
            ...emptyTopUsage,
            ...response.platform.top_usage,
            submitters: Array.isArray(response.platform.top_usage?.submitters) ? response.platform.top_usage.submitters : [],
            db_connections_by_tickets: Array.isArray(response.platform.top_usage?.db_connections_by_tickets) ? response.platform.top_usage.db_connections_by_tickets : [],
            failed_db_connections: Array.isArray(response.platform.top_usage?.failed_db_connections) ? response.platform.top_usage.failed_db_connections : [],
            sql_exports_by_user: Array.isArray(response.platform.top_usage?.sql_exports_by_user) ? response.platform.top_usage.sql_exports_by_user : [],
          },
          recent_attention: Array.isArray(response.platform.recent_attention) ? response.platform.recent_attention : [],
          long_pending_tickets: Array.isArray(response.platform.long_pending_tickets) ? response.platform.long_pending_tickets : [],
          recent_failed_tickets: Array.isArray(response.platform.recent_failed_tickets) ? response.platform.recent_failed_tickets : [],
          db_connection_failures: Array.isArray(response.platform.db_connection_failures) ? response.platform.db_connection_failures : [],
        }
      : null,
  }
}
