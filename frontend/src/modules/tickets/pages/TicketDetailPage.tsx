import { startTransition, useEffect, useRef, useState } from 'react'
import { ArrowLeft, Check, ChevronDown, Download, Loader2, Minus, Play, Plus, RotateCcw, ShieldCheck, ShieldX, Square, X } from 'lucide-react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { format as formatSQL } from 'sql-formatter'
import { cn } from '@/lib/utils'
import { useAuth } from '@/shared/auth/AuthContext'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import { MAESTRO_REALTIME_EVENT } from '@/shared/realtime/events'
import type { QueryAccessTicketItem, Ticket, TicketDetail, TicketScope, TicketWorkflowParticipants, TicketWorkflowResolution, TicketWorkflowTrace } from '@/shared/types/ticket'
import type { TicketStatus } from '@/shared/types/ticket'
import type { CurrentUser } from '@/shared/types/auth'
import type { AuditLog } from '@/shared/types/audit'
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog'
import { DataTable, DataTableBody, DataTableCell, DataTableHead, DataTableHeaderCell, DataTableRow } from '@/shared/ui/DataTable'
import { ExpandableSql, isExpandableSql } from '@/shared/ui/ExpandableSql'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { StatusBadge } from '@/shared/ui/StatusBadge'
import { useToast } from '@/shared/ui/ToastContext'
import { approveTicket, createRollbackTicket, downloadTicketExport, executeTicket, executeTicketStatement, getTicket, previewRollbackTicket, rejectTicket, retryWorkflowResolution, revokeTicket, stopTicketStatement, withdrawTicket } from '@/modules/tickets/api'
import type { RollbackPreviewItem } from '@/modules/tickets/api'

function DetailTable({
  headers,
  rows,
}: {
  headers: string[]
  rows: Array<Array<React.ReactNode>>
}) {
  return (
    <div className="mt-3 overflow-x-auto rounded-xl border border-border">
      <DataTable>
        <DataTableHead>
          <tr>
            {headers.map((header) => (
              <DataTableHeaderCell key={header}>
                {header}
              </DataTableHeaderCell>
            ))}
          </tr>
        </DataTableHead>
        <DataTableBody>
          {rows.map((row, rowIndex) => (
            <DataTableRow key={rowIndex}>
              {row.map((cell, cellIndex) => (
                <DataTableCell key={`${rowIndex}-${cellIndex}`} className="align-middle">
                  {cell}
                </DataTableCell>
              ))}
            </DataTableRow>
          ))}
        </DataTableBody>
      </DataTable>
    </div>
  )
}

function formatTicketActor(name: string | null | undefined, id: number | null | undefined) {
  if (name && name.trim()) {
    return name
  }
  if (id === 0) {
    return 'System'
  }
  if (id != null) {
    return String(id)
  }
  return '—'
}

function ticketUsesExecutor(ticket: Ticket) {
  return ticket.ticket_type === 'ddl' || ticket.ticket_type === 'dml' || ticket.ticket_type === 'redis_command'
}

function formatTicketReviewerActor(ticket: Ticket, workflowResolution?: TicketWorkflowResolution | null) {
  if (workflowResolution && !workflowResolution.approval_enabled) {
    return 'System'
  }
  return formatTicketActor(ticket.reviewer_name, ticket.reviewer_id ?? null)
}

function formatTicketExecutorActor(ticket: Ticket, workflowResolution?: TicketWorkflowResolution | null) {
  if (ticketUsesExecutor(ticket) && workflowResolution?.execution_mode === 'auto_after_approval') {
    return 'System'
  }
  return formatTicketActor(ticket.executor_name, ticket.executor_id ?? null)
}

function formatTicketDatabaseScope(ticket: Ticket, fallback: string) {
  if (ticket.ticket_type === 'query_access') {
    return fallback
  }
  const database = ticket.database_name || '—'
  if (!ticket.schema_name) {
    return database
  }
  return `${database} / ${ticket.schema_name}`
}

function isAdminUser(user: CurrentUser) {
  return user.username === 'admin' || user.protected || user.authGroups.includes('admin') || user.authGroupDetails.some((group) => group.group_key === 'admin')
}

function formatResolvedUsers(ids: number[], names: string[]) {
  const visibleNames = names.map((name) => name.trim()).filter(Boolean)
  if (visibleNames.length > 0) {
    const suffix = ids.length > visibleNames.length ? ` (${ids.length} users)` : ''
    return `${visibleNames.join(', ')}${suffix}`
  }
  if (ids.length > 0) {
    return `${ids.length} user${ids.length > 1 ? 's' : ''}`
  }
  return '—'
}

function formatTraceUsers(users: Array<{ id: number; username: string }> | undefined, ids: number[], fallbackNames: string[]) {
  if (users && users.length > 0) {
    return users.map((item) => `${item.username} (#${item.id})`).join(', ')
  }
  return formatResolvedUsers(ids, fallbackNames)
}

function formatTraceGroups(groups: Array<{ group_key: string; name: string }> | undefined, fallbackKeys: string[]) {
  if (groups && groups.length > 0) {
    return groups.map((group) => `${group.name} (${group.group_key})`).join(', ')
  }
  return fallbackKeys.length > 0 ? fallbackKeys.join(', ') : '—'
}

function getTraceStringArray(trace: unknown, key: string) {
  if (!trace || typeof trace !== 'object' || Array.isArray(trace)) {
    return []
  }
  const value = (trace as Record<string, unknown>)[key]
  if (!Array.isArray(value)) {
    return []
  }
  return value.filter((item): item is string => typeof item === 'string' && item.trim() !== '')
}

function formatMissingGroups(trace: TicketWorkflowTrace) {
  const approvalGroups = getTraceStringArray(trace.resolution_trace, 'missing_approval_groups')
  const executorGroups = getTraceStringArray(trace.resolution_trace, 'missing_executor_groups')
  const parts = [
    approvalGroups.length > 0 || (trace.missing_approval_groups?.length ?? 0) > 0
      ? `Approval: ${formatTraceGroups(trace.missing_approval_groups, approvalGroups)}`
      : '',
    executorGroups.length > 0 || (trace.missing_executor_groups?.length ?? 0) > 0
      ? `Execution: ${formatTraceGroups(trace.missing_executor_groups, executorGroups)}`
      : '',
  ].filter(Boolean)
  return parts.length > 0 ? parts.join(' | ') : '—'
}

function annotateIDList(ids: number[], users?: Array<{ id: number; username: string }>) {
  const byID = new Map((users ?? []).map((user) => [user.id, user.username] as const))
  return ids.map((id) => {
    const username = byID.get(id)
    return username ? `${id} (${username})` : id
  })
}

function annotateGroupList(keys: string[], groups?: Array<{ group_key: string; name: string }>) {
  const byKey = new Map((groups ?? []).map((group) => [group.group_key, group.name] as const))
  return keys.map((key) => {
    const name = byKey.get(key)
    return name && name !== key ? `${key} (${name})` : key
  })
}

function buildReadableResolutionTrace(trace: TicketWorkflowTrace, ticket: Ticket) {
  const raw = trace.resolution_trace && typeof trace.resolution_trace === 'object' && !Array.isArray(trace.resolution_trace)
    ? { ...(trace.resolution_trace as Record<string, unknown>) }
    : {}
  return {
    ...raw,
    rule_id: trace.workflow_rule_id ?? raw.rule_id,
    rule_name: trace.workflow_rule_name || raw.rule_name,
    db_connection_id: ticket.db_connection_id != null && ticket.db_connection_name
      ? `${ticket.db_connection_id} (${ticket.db_connection_name})`
      : raw.db_connection_id ?? ticket.db_connection_id ?? null,
    approval_user_ids: annotateIDList(trace.approval_user_ids, trace.approval_users),
    executor_user_ids: annotateIDList(trace.executor_user_ids, trace.executor_users),
    admin_user_ids: annotateIDList(trace.admin_user_ids, trace.admin_users),
    missing_approval_groups: annotateGroupList(getTraceStringArray(trace.resolution_trace, 'missing_approval_groups'), trace.missing_approval_groups),
    missing_executor_groups: annotateGroupList(getTraceStringArray(trace.resolution_trace, 'missing_executor_groups'), trace.missing_executor_groups),
  }
}

function DebugWorkflowResolutionTrace({
  trace,
  participants,
  ticket,
}: {
  trace: TicketWorkflowTrace
  participants: TicketWorkflowParticipants
  ticket: Ticket
}) {
  const readableTrace = buildReadableResolutionTrace(trace, ticket)
  return (
    <div className="mt-3">
      <DetailTable
        headers={['Rule', 'Approval Required', 'Reviewers', 'Executors', 'Admin Escalation', 'Missing Groups', 'Error', 'Resolved At']}
        rows={[[
          trace.workflow_rule_name || trace.workflow_rule_id || '—',
          trace.approval_enabled ? 'Yes' : 'No',
          formatTraceUsers(trace.approval_users, trace.approval_user_ids, participants.reviewers),
          formatTraceUsers(trace.executor_users, trace.executor_user_ids, participants.executors),
          formatTraceUsers(trace.admin_users, trace.admin_user_ids, []),
          formatMissingGroups(trace),
          trace.error_message || trace.error_code || '—',
          formatDateTime(trace.resolved_at, true),
        ]]}
      />
      {trace.resolution_trace ? (
        <details className="mt-3 rounded-xl border border-border bg-panel-soft px-4 py-3">
          <summary className="cursor-pointer text-[12px] font-semibold text-muted">Raw resolution trace</summary>
          <pre className="mt-3 max-h-64 overflow-auto font-mono text-[12px] leading-6 text-ink">
            {JSON.stringify(readableTrace, null, 2)}
          </pre>
        </details>
      ) : null}
    </div>
  )
}

function formatExecutionDuration(startedAt?: string | null, completedAt?: string | null) {
  if (!startedAt || !completedAt) {
    return '—'
  }
  const start = new Date(startedAt).getTime()
  const end = new Date(completedAt).getTime()
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) {
    return '—'
  }
  return `${((end - start) / 1000).toFixed(3)}s`
}

function statementStatusToTicketStatus(status: string): TicketStatus | null {
  switch (status) {
    case 'pending':
      return 'pending_execution'
    case 'running':
      return 'executing'
    case 'completed':
      return 'completed'
    case 'failed':
      return 'failed'
    case 'stopped':
      return 'stopped'
    default:
      return null
  }
}

function StatementExecutionBadge({ status }: { status: string | null }) {
  if (!status) {
    return <span className="text-muted">—</span>
  }
  const badgeStatus = statementStatusToTicketStatus(status)
  if (!badgeStatus) {
    return <span className="text-muted">{status}</span>
  }
  return <StatusBadge status={badgeStatus} className="px-2 py-0.5 text-[10px] leading-4 tracking-normal" />
}

function RollbackStatusCell({
  rollback,
}: {
  rollback: TicketDetail['execution_rollbacks'][number] | null
}) {
  if (!rollback) {
    return <span className="text-muted">—</span>
  }
  const label = formatRollbackStatus(rollback.status)
  const title = [
    rollback.generator ? `Generator: ${rollback.generator}` : '',
    rollback.unsupported_reason ? `Reason: ${rollback.unsupported_reason}` : '',
    rollback.failure_message ? `Failure: ${rollback.failure_message}` : '',
    rollback.warning_message ? `Warning: ${rollback.warning_message}` : '',
  ].filter(Boolean).join('\n') || label
  if (rollback.status === 'generated') {
    return <span className="text-[12px] font-semibold text-emerald-700" title={title}>{label}</span>
  }
  if (rollback.status === 'submitted') {
    return <span className="text-[12px] font-semibold text-primary" title={title}>{label}</span>
  }
  return <span className="text-[12px] font-semibold text-muted" title={title}>{label}</span>
}

function formatRollbackStatus(status: string) {
  switch (status) {
    case 'unsupported':
      return 'Unavailable'
    case 'generating':
      return 'Generating'
    case 'generated':
      return 'Generated'
    case 'failed':
      return 'Generation Failed'
    case 'submitted':
      return 'Ticket Created'
    default:
      return status || '—'
  }
}

function formatTicketTypeLabel(ticketType: string) {
  switch (ticketType) {
    case 'ddl':
      return 'DDL'
    case 'dml':
      return 'DML'
    case 'redis_command':
      return 'Redis'
    case 'query_access':
      return 'Query Access'
    case 'sql_export':
      return 'SQL Export'
    case 'sensitive_query_access':
      return 'Sensitive Access'
    default:
      return ticketType
  }
}

function formatTicketSQLForDisplay(sql: string, ticketType: Ticket['ticket_type']) {
  if (ticketType === 'redis_command' || sql.trim() === '') {
    return sql
  }
  try {
    return formatSQL(sql, {
      language: 'sql',
      keywordCase: 'upper',
    }).trimEnd()
  } catch {
    return sql
  }
}

function formatQueryAccessDuration(minutes?: number | null) {
  switch (minutes) {
    case 1440:
      return '1 day'
    case 10080:
      return '1 week'
    case 43200:
      return '1 month'
    case 525600:
      return '1 year'
    case 1576800:
      return '3 years'
    default:
      return minutes != null ? `${minutes} minutes` : '—'
  }
}

function formatQueryAccessPattern(value?: string | null, allLabel = 'All') {
  if (!value || value === '*') {
    return allLabel
  }
  return value
}

function formatQueryAccessConnection(item: QueryAccessTicketItem) {
  return item.db_connection_name || `Connection #${item.connection_id}`
}

function formatQueryAccessRuleSummary(item: QueryAccessTicketItem) {
  const action = item.effect === 'deny' ? 'Exclude' : 'Grant'
  const database = formatQueryAccessPattern(item.database_pattern || item.database_name, 'all databases')
  const table = formatQueryAccessPattern(item.table_pattern || item.table_name, 'all tables')
  return `${action} ${formatQueryAccessConnection(item)} / ${database} / ${table}`
}

function summarizeQueryAccessConnections(items: QueryAccessTicketItem[]) {
  const names = Array.from(new Set(items.map(formatQueryAccessConnection)))
  if (names.length === 0) {
    return '—'
  }
  if (names.length === 1) {
    return names[0]
  }
  return `${names[0]} + ${names.length - 1} more`
}

function summarizeQueryAccessScope(items: QueryAccessTicketItem[]) {
  if (items.length === 0) {
    return '—'
  }
  const allowInstanceCount = items.filter((item) => item.effect !== 'deny' && (item.database_pattern || item.database_name) === '*' && (item.table_pattern || item.table_name || '*') === '*').length
  const denyCount = items.filter((item) => item.effect === 'deny').length
  const parts = [`${items.length} rule${items.length > 1 ? 's' : ''}`]
  if (allowInstanceCount > 0) {
    parts.push(`${allowInstanceCount} instance-level grant${allowInstanceCount > 1 ? 's' : ''}`)
  }
  if (denyCount > 0) {
    parts.push(`${denyCount} exclusion${denyCount > 1 ? 's' : ''}`)
  }
  return parts.join(', ')
}

function formatActivityAction(actionType: string) {
  switch (actionType) {
    case 'ticket_submit':
      return 'Submitted'
    case 'ticket_approve':
      return 'Approved'
    case 'ticket_reject':
      return 'Rejected'
    case 'ticket_withdraw':
      return 'Withdrawn'
    case 'ticket_execute_start':
      return 'Execution Started'
    case 'ticket_execute_complete':
      return 'Execution Completed'
    case 'ticket_execute_failed':
      return 'Execution Failed'
    case 'ticket_revoke':
      return 'Access Revoked'
    case 'ticket_schedule':
      return 'Execution Scheduled'
    case 'ticket_execution_recovered':
      return 'Execution Recovered'
    default:
      return actionType
    }
}

function parseAuditDetails(details: unknown) {
  if (!details || typeof details !== 'object' || Array.isArray(details)) {
    return null
  }
  return details as Record<string, unknown>
}

function formatActivityDetail(log: AuditLog) {
  const details = parseAuditDetails(log.details)
  switch (log.action_type) {
    case 'ticket_submit':
      return 'Ticket submitted and waiting for review.'
    case 'ticket_approve':
      return typeof details?.comment === 'string' && details.comment.trim()
        ? `Review comment: ${details.comment.trim()}`
        : 'Ticket approved.'
    case 'ticket_reject':
      return typeof details?.reason === 'string' && details.reason.trim()
        ? `Reject reason: ${details.reason.trim()}`
        : 'Ticket rejected.'
    case 'ticket_withdraw':
      return typeof details?.reason === 'string' && details.reason.trim()
        ? `Withdraw reason: ${details.reason.trim()}`
        : 'Ticket withdrawn by submitter.'
    case 'ticket_execute_start':
      return typeof details?.comment === 'string' && details.comment.trim()
        ? `Execution started. Comment: ${details.comment.trim()}`
        : 'Ticket execution started.'
    case 'ticket_execute_complete':
      return 'Execution result: completed successfully.'
    case 'ticket_execute_failed':
      return 'Execution result: failed.'
    case 'ticket_revoke':
      return 'Access was revoked early.'
    case 'ticket_schedule':
      return 'Ticket was scheduled for execution.'
    case 'ticket_execution_recovered': {
      const reason = typeof details?.reason === 'string' && details.reason.trim()
        ? details.reason.trim()
        : 'Service restarted while the ticket was executing.'
      const failedIDs = Array.isArray(details?.failed_execution_ids)
        ? details.failed_execution_ids.filter((id) => typeof id === 'number' || typeof id === 'string')
        : []
      if (failedIDs.length > 0) {
        return `${reason} Affected statement execution IDs: ${failedIDs.join(', ')}.`
      }
      return reason
    }
    default:
      return details ? JSON.stringify(details) : '—'
  }
}

function extractRealtimeTicketID(detail: unknown): string | null {
  if (!detail || typeof detail !== 'object') {
    return null
  }

  const eventDetail = detail as {
    event?: string
    data?: {
      ticket_id?: number
      notification?: {
        resource_type?: string | null
        resource_id?: number | null
        resource_ref?: string | null
      } | null
    } | null
  }

  if (eventDetail.event === 'ticket.updated') {
    const ticketID = eventDetail.data?.ticket_id
    return ticketID == null ? null : String(ticketID)
  }

  if (eventDetail.event === 'notification.created') {
    const notification = eventDetail.data?.notification
    if (notification?.resource_type === 'ticket' && notification.resource_ref) {
      return notification.resource_ref
    }
    if (notification?.resource_type === 'ticket' && notification.resource_id != null) {
      return String(notification.resource_id)
    }
  }

  return null
}

type StatementResultRow = {
  executionID: number | null
  seq: number
  sql: string
  tables: ReviewStatementTable[]
  scanRows: number | null
  reviewStatus: string | null
  reviewMessage: string | null
  rowsAffected: number | null
  executionStatus: string | null
  duration: string | null
  errorMessage: string | null
  sentToDBAt: string | null
  dbProcessType: string | null
  dbProcessID: number | null
  interruptionReason: string | null
  outcomeConfidence: string | null
  rollback: TicketDetail['execution_rollbacks'][number] | null
}

type ReviewStatementTable = {
  key: string
  label: string
  rowCount: number | null
  dataSizeBytes: number | null
}

function statementResultKey(row: StatementResultRow) {
  return `${row.seq}:${row.sql}`
}

function reviewTableLabel(table: { database_name?: string | null; schema_name?: string | null; table_name: string }) {
  const databaseName = table.database_name?.trim() ?? ''
  const schemaName = table.schema_name?.trim() ?? ''
  const tableName = table.table_name.trim()
  if (schemaName && databaseName && schemaName !== databaseName) {
    return `${schemaName}.${tableName}`
  }
  return tableName
}

function mergeReviewTables(
  current: ReviewStatementTable[] | undefined,
  tables: Array<{ database_name?: string | null; schema_name?: string | null; table_name: string; row_count?: number | null; data_size_bytes?: number | null }> | undefined,
) {
  const next = [...(current ?? [])]
  if (!Array.isArray(tables)) {
    return next
  }
  tables.forEach((table) => {
    const key = `${table.database_name ?? ''}:${table.schema_name ?? ''}:${table.table_name}`
    if (next.some((item) => item.key === key)) {
      return
    }
    next.push({
      key,
      label: reviewTableLabel(table),
      rowCount: typeof table.row_count === 'number' ? table.row_count : null,
      dataSizeBytes: typeof table.data_size_bytes === 'number' ? table.data_size_bytes : null,
    })
  })
  return next
}

function formatReviewRows(rows: number | null) {
  if (rows == null || !Number.isFinite(rows)) {
    return '—'
  }
  return Math.round(rows).toLocaleString()
}

function formatReviewBytes(bytes: number | null) {
  if (bytes == null || !Number.isFinite(bytes)) {
    return '—'
  }
  if (bytes <= 0) {
    return '0.00 GB'
  }
  const gb = bytes / 1024 / 1024 / 1024
  return `${gb.toLocaleString(undefined, {
    minimumFractionDigits: gb < 10 ? 2 : 1,
    maximumFractionDigits: gb < 10 ? 2 : 1,
  })} GB`
}

function formatReviewTableRows(tables: ReviewStatementTable[]) {
  if (tables.length === 0) {
    return '—'
  }
  if (tables.length === 1) {
    return formatReviewRows(tables[0].rowCount)
  }
  return tables.map((table) => `${table.label}: ${formatReviewRows(table.rowCount)}`).join('\n')
}

function formatReviewTableSizes(tables: ReviewStatementTable[]) {
  if (tables.length === 0) {
    return '—'
  }
  if (tables.length === 1) {
    return formatReviewBytes(tables[0].dataSizeBytes)
  }
  return tables.map((table) => `${table.label}: ${formatReviewBytes(table.dataSizeBytes)}`).join('\n')
}

function isFullTicketExecutionRunMode(mode?: string | null) {
  return mode === 'batch' || mode === 'workflow_auto'
}

function buildStatementResults(detail: TicketDetail) {
  const rows = new Map<number, StatementResultRow>()
  const hidePendingExecutionStatus = detail.ticket.status === 'rejected' || detail.ticket.status === 'withdrawn'

  detail.review_results.forEach((result) => {
    if (result.phase && result.phase !== 'validation') {
      return
    }
    const existing = rows.get(result.seq)
    const nextMessage = [existing?.reviewMessage, result.message]
      .filter((item): item is string => Boolean(item && item.trim()))
      .join(' | ')
    rows.set(result.seq, {
      executionID: existing?.executionID ?? null,
      seq: result.seq,
      sql: result.sql_stmt,
      tables: mergeReviewTables(existing?.tables, result.tables),
      scanRows: Math.max(existing?.scanRows ?? 0, result.scan_rows),
      reviewStatus: existing?.reviewStatus === 'error' || result.status === 'error' ? 'error' : result.status,
      reviewMessage: nextMessage || null,
      rowsAffected: existing?.rowsAffected ?? null,
      executionStatus: existing?.executionStatus ?? null,
      duration: existing?.duration ?? null,
      errorMessage: existing?.errorMessage ?? null,
      sentToDBAt: existing?.sentToDBAt ?? null,
      dbProcessType: existing?.dbProcessType ?? null,
      dbProcessID: existing?.dbProcessID ?? null,
      interruptionReason: existing?.interruptionReason ?? null,
      outcomeConfidence: existing?.outcomeConfidence ?? null,
      rollback: existing?.rollback ?? null,
    })
  })

  const rollbacksByExecutionID = new Map(detail.execution_rollbacks.map((item) => [item.execution_id, item]))
  detail.executions.forEach((execution) => {
    const existing = rows.get(execution.seq)
    rows.set(execution.seq, {
      executionID: execution.id,
      seq: execution.seq,
      sql: existing?.sql || execution.sql_stmt,
      tables: existing?.tables ?? [],
      scanRows: existing?.scanRows ?? null,
      reviewStatus: existing?.reviewStatus ?? null,
      reviewMessage: existing?.reviewMessage ?? null,
      rowsAffected: execution.rows_affected ?? 0,
      executionStatus: hidePendingExecutionStatus && execution.status === 'pending' ? null : execution.status,
      duration: execution.status === 'stopped' || Boolean(execution.interruption_reason)
        ? null
        : typeof execution.duration_ms === 'number'
          ? `${(execution.duration_ms / 1000).toFixed(3)}s`
          : formatExecutionDuration(execution.started_at, execution.completed_at),
      errorMessage: formatExecutionOutcomeMessage(execution.outcome_confidence, execution.interruption_reason, execution.error_msg),
      sentToDBAt: execution.sent_to_db_at ?? null,
      dbProcessType: execution.db_process_type ?? null,
      dbProcessID: execution.db_process_id ?? null,
      interruptionReason: execution.interruption_reason ?? null,
      outcomeConfidence: execution.outcome_confidence ?? null,
      rollback: rollbacksByExecutionID.get(execution.id) ?? existing?.rollback ?? null,
    })
  })

  return Array.from(rows.values()).sort((a, b) => a.seq - b.seq)
}

function formatExecutionOutcomeMessage(outcome?: string | null, reason?: string | null, errorMessage?: string | null) {
  if (outcome === 'not_sent') {
    return errorMessage
      ? `SQL was not sent to DB. Connection/setup failed before execution: ${errorMessage}`
      : 'SQL was not sent to DB. Connection/setup failed before execution.'
  }
  if (outcome === 'outcome_unknown') {
    return errorMessage
      ? `SQL was sent to DB, then the connection was interrupted. DB outcome is unknown; verify on target DB: ${errorMessage}`
      : 'SQL was sent to DB, then the connection was interrupted. DB outcome is unknown; verify on target DB.'
  }
  if (outcome === 'manually_stopped' || reason === 'manually_stopped') {
    return 'Manually stopped.'
  }
  if (outcome === 'service_shutdown' || reason === 'service_shutdown') {
    return errorMessage
      ? `Service shutdown during execution: ${errorMessage}`
      : 'Service shutdown during execution.'
  }
  if (reason === 'service_restart') {
    return errorMessage
      ? `Service restarted during execution. DB outcome may require verification: ${errorMessage}`
      : 'Service restarted during execution. DB outcome may require verification.'
  }
  if (reason === 'execution_panic') {
    return errorMessage
      ? `Platform execution process failed: ${errorMessage}`
      : 'Platform execution process failed.'
  }
  return errorMessage ?? null
}

function formatExecutionRuntimeProcess(row: StatementResultRow) {
  if (!row.dbProcessType || row.dbProcessID == null) {
    return '—'
  }
  return `${row.dbProcessType}: ${row.dbProcessID}`
}

type WorkflowStepTone = 'done' | 'current' | 'upcoming' | 'failed'

type WorkflowStep = {
  key: string
  title: string
  actor: string
  tone: WorkflowStepTone
  running?: boolean
}

function joinParticipantNames(names: string[], fallback: string) {
  const normalized = names.map((item) => item.trim()).filter(Boolean)
  if (normalized.length === 0) {
    return fallback
  }
  return normalized.join(', ')
}

function buildWorkflowSteps(
  ticket: Ticket,
  workflowParticipants: TicketWorkflowParticipants,
  workflowResolution?: TicketWorkflowResolution | null,
  activityLogs: AuditLog[] = [],
): WorkflowStep[] {
  const submitter = formatTicketActor(ticket.submitter_name, ticket.submitter_id)
  const reviewer = workflowResolution && !workflowResolution.approval_enabled
    ? 'System'
    : (ticket.reviewer_id != null || ticket.reviewer_name)
      ? formatTicketActor(ticket.reviewer_name, ticket.reviewer_id ?? null)
      : joinParticipantNames(workflowParticipants.reviewers, 'Pending reviewer assignment')
  const usesExecutor = ticketUsesExecutor(ticket)
  const executor = usesExecutor && workflowResolution?.execution_mode === 'auto_after_approval'
    ? 'System'
    : (ticket.executor_id != null || ticket.executor_name)
      ? formatTicketActor(ticket.executor_name, ticket.executor_id ?? null)
      : joinParticipantNames(workflowParticipants.executors, 'Pending executor assignment')
  const rejectedAfterReview = usesExecutor && ticket.status === 'rejected' && (
    ticket.executor_id != null ||
    Boolean(ticket.executor_name) ||
    activityLogs.some((log) => log.action_type === 'ticket_approve')
  )

  const reviewerTone: WorkflowStepTone =
    ticket.status === 'pending_review' ? 'current'
      : ticket.status === 'rejected' || ticket.status === 'withdrawn' ? rejectedAfterReview ? 'done' : 'failed'
      : 'done'
  const executorTone: WorkflowStepTone = !usesExecutor
    ? 'upcoming'
    : rejectedAfterReview ? 'failed'
      : ticket.status === 'rejected' || ticket.status === 'withdrawn' ? 'upcoming'
      : ticket.status === 'approved' || ticket.status === 'pending_execution' || ticket.status === 'executing' ? 'current'
        : ticket.status === 'completed' ? 'done'
          : ticket.status === 'failed' || ticket.status === 'stopped' || ticket.status === 'interrupted' ? 'failed'
            : 'upcoming'
  const completionTone: WorkflowStepTone = usesExecutor
    ? ticket.status === 'completed' ? 'done'
      : ticket.status === 'failed' || ticket.status === 'stopped' || ticket.status === 'interrupted' || ticket.status === 'rejected' || ticket.status === 'withdrawn' ? 'failed'
        : 'upcoming'
    : ticket.status === 'approved' || ticket.status === 'completed' ? 'done'
      : ticket.status === 'failed' || ticket.status === 'stopped' || ticket.status === 'interrupted' || ticket.status === 'rejected' || ticket.status === 'withdrawn' ? 'failed'
        : 'upcoming'
  const steps: WorkflowStep[] = [
    {
      key: 'submitter',
      title: 'Submitted',
      actor: submitter,
      tone: 'done',
    },
    {
      key: 'reviewer',
      title: 'Review',
      actor: reviewer,
      tone: reviewerTone,
    },
  ]

  if (usesExecutor) {
    steps.push({
      key: 'executor',
      title: 'Execution',
      actor: executor,
      tone: executorTone,
      running: ticket.status === 'executing',
    })
  }

  steps.push({
    key: 'complete',
    title: 'Complete',
    actor: usesExecutor ? 'System status update' : 'Approval outcome',
    tone: completionTone,
  })

  return steps
}

function WorkflowStepIcon({ tone, running, label }: { tone: WorkflowStepTone; running?: boolean; label: string }) {
  if (running) {
    return (
      <span className="inline-flex h-9 w-9 items-center justify-center rounded-full border border-slate-200 bg-slate-100 text-slate-700" aria-label={`${label}: executing`}>
        <Loader2 className="h-5 w-5 animate-spin" />
      </span>
    )
  }
  if (tone === 'done') {
    return (
      <span className="inline-flex h-9 w-9 items-center justify-center rounded-full bg-emerald-500 text-white" aria-label={`${label}: completed`}>
        <Check className="h-5 w-5" />
      </span>
    )
  }
  if (tone === 'current') {
    return (
      <span className="inline-flex h-9 w-9 items-center justify-center rounded-full border-[6px] border-accent bg-white text-accent" aria-label={`${label}: current`} />
    )
  }
  if (tone === 'failed') {
    return (
      <span className="inline-flex h-9 w-9 items-center justify-center rounded-full bg-rose-500 text-white" aria-label={`${label}: failed`}>
        <X className="h-5 w-5" />
      </span>
    )
  }
  return (
    <span className="inline-flex h-9 w-9 items-center justify-center rounded-full bg-slate-300 text-white text-sm font-bold" aria-label={`${label}: upcoming`}>
      •
    </span>
  )
}

function WorkflowTimeline({
  ticket,
  workflowParticipants,
  workflowResolution,
  activityLogs,
  highlight,
}: {
  ticket: Ticket
  workflowParticipants: TicketWorkflowParticipants
  workflowResolution?: TicketWorkflowResolution | null
  activityLogs?: AuditLog[]
  highlight?: boolean
}) {
  const steps = buildWorkflowSteps(ticket, workflowParticipants, workflowResolution, activityLogs)

  return (
    <section
      className={cn(
        'rounded-xl border border-border bg-panel shadow-soft transition-all duration-300',
        highlight ? 'border-accent/30 shadow-[0_0_0_3px_rgba(59,130,246,0.10)]' : '',
      )}
    >
      <div className="border-b border-border/80 px-4 py-3">
        <div className="flex items-center justify-between gap-3">
          <p className="text-[13px] font-semibold text-ink">Approval Flow</p>
        </div>
      </div>

      <div className="overflow-x-auto px-4 py-4">
        <div className="grid min-w-[720px] gap-4" style={{ gridTemplateColumns: `repeat(${steps.length}, minmax(0, 1fr))` }}>
          {steps.map((step, index) => (
            <div key={step.key} className="relative pr-4 last:pr-0">
              {index < steps.length - 1 ? (
                <span className="absolute left-[44px] right-0 top-[18px] h-px bg-border" aria-hidden="true" />
              ) : null}
              <div className="relative">
                <WorkflowStepIcon tone={step.tone} running={step.running} label={step.title} />
                <div className="mt-3">
                  <p className="text-[13px] font-semibold text-ink">{step.title}</p>
                  <p className="mt-1 text-[12px] font-medium text-ink">{step.actor}</p>
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}

export function TicketDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { user } = useAuth()
  const { pushToast } = useToast()
  const [detail, setDetail] = useState<TicketDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [comment, setComment] = useState('')
  const [reason, setReason] = useState('')
  const [acting, setActing] = useState<'approve' | 'reject' | 'withdraw' | 'execute' | 'revoke' | 'retry_workflow' | null>(null)
  const [actingExecutionID, setActingExecutionID] = useState<number | null>(null)
  const [rollbackPreviewOpen, setRollbackPreviewOpen] = useState(false)
  const [rollbackPreviewItems, setRollbackPreviewItems] = useState<RollbackPreviewItem[]>([])
  const [selectedRollbackIDs, setSelectedRollbackIDs] = useState<Set<number>>(() => new Set())
  const [rollbackPreviewLoading, setRollbackPreviewLoading] = useState(false)
  const [rollbackCreateLoading, setRollbackCreateLoading] = useState(false)
  const [confirmAction, setConfirmAction] = useState<'withdraw' | 'execute' | 'revoke' | null>(null)
  const [downloadingExport, setDownloadingExport] = useState(false)
  const [otherDetailsOpen, setOtherDetailsOpen] = useState(false)
  const [debugTraceOpen, setDebugTraceOpen] = useState(false)
  const [statusTransitioning, setStatusTransitioning] = useState(false)
  const [expandedStatementSQLs, setExpandedStatementSQLs] = useState<Set<string>>(() => new Set())
  const previousStatusRef = useRef<string | null>(null)

  useEffect(() => {
    let active = true

    async function loadTicket() {
      if (!id) {
        setError('Missing ticket number')
        setLoading(false)
        return
      }

      setLoading(true)
      setError('')

      try {
        const nextDetail = await getTicket(id)
        if (active) {
          startTransition(() => {
            setDetail(nextDetail)
          })
          if (id !== nextDetail.ticket.ticket_no) {
            navigate(`/tickets/${nextDetail.ticket.ticket_no}`, { replace: true })
          }
        }
      } catch (loadError) {
        if (active) {
          setError(loadError instanceof ApiError ? loadError.message : 'Failed to load ticket. Please try again later.')
        }
      } finally {
        if (active) {
          setLoading(false)
        }
      }
    }

    void loadTicket()

    return () => {
      active = false
    }
  }, [id, navigate])

  useEffect(() => {
    if (!id) {
      return
    }

    const handleRealtime = (event: Event) => {
      const realtimeEvent = event as CustomEvent<unknown>
      const realtimeTicketRef = extractRealtimeTicketID(realtimeEvent.detail)
      const currentTicket = detail?.ticket
      const matchesTicket = realtimeTicketRef === id ||
        (currentTicket != null && (realtimeTicketRef === String(currentTicket.id) || realtimeTicketRef === currentTicket.ticket_no))
      if (!matchesTicket) {
        return
      }
      void reloadTicket({ background: true })
    }

    window.addEventListener(MAESTRO_REALTIME_EVENT, handleRealtime)
    return () => {
      window.removeEventListener(MAESTRO_REALTIME_EVENT, handleRealtime)
    }
  }, [detail?.ticket, id])

  useEffect(() => {
    const nextStatus = detail?.ticket.status ?? null
    const previousStatus = previousStatusRef.current
    previousStatusRef.current = nextStatus

    if (!nextStatus || !previousStatus || nextStatus === previousStatus) {
      return
    }

    setStatusTransitioning(true)
    const timer = window.setTimeout(() => {
      setStatusTransitioning(false)
    }, 900)
    return () => {
      window.clearTimeout(timer)
    }
  }, [detail?.ticket.status])

  useEffect(() => {
    setExpandedStatementSQLs(new Set())
  }, [detail?.ticket.id])

  if (!user) {
    return null
  }

  const ticket = detail?.ticket ?? null
  const canReview = detail?.capabilities?.can_review ?? false
  const canReject = detail?.capabilities?.can_reject ?? false
  const canWithdraw = detail?.capabilities?.can_withdraw ?? false
  const canExecute = detail?.capabilities?.can_execute ?? false
  const canRevoke = detail?.capabilities?.can_revoke ?? false
  const canRetryWorkflow = detail?.capabilities?.can_retry_workflow_resolution ?? false
  const exportDownloadURL = detail?.export_request?.download_url ?? null
  const statementResults = detail ? buildStatementResults(detail) : []
  const displaySQLContent = ticket ? formatTicketSQLForDisplay(ticket.sql_content, ticket.ticket_type) : ''
  const displayStatementResults = statementResults.map((row) => ({
    ...row,
    sql: formatTicketSQLForDisplay(row.sql, ticket?.ticket_type ?? 'dml'),
  }))
  const showReviewMessageColumn = displayStatementResults.some((row) => Boolean(row.reviewMessage?.trim()))
  const showErrorMessageColumn = displayStatementResults.some((row) => Boolean(row.errorMessage?.trim()))
  const showStatementTableMetadata = ticket?.ticket_type === 'ddl' || ticket?.ticket_type === 'dml'
  const showStatementScanRows = ticket?.ticket_type === 'dml'
  const showStatementRowsAffected = ticket?.ticket_type !== 'ddl'
  const showStatementRollback = displayStatementResults.some((row) => row.rollback != null)
  const executionRuntimeRows = displayStatementResults.filter((row) => (
    row.executionID != null ||
    row.sentToDBAt ||
    row.outcomeConfidence ||
    row.interruptionReason ||
    row.dbProcessType ||
    row.dbProcessID != null
  ))
  const expandableStatementKeys = displayStatementResults
    .filter((row) => isExpandableSql(row.sql))
    .map(statementResultKey)
  const allStatementSQLsExpanded = expandableStatementKeys.length > 0 &&
    expandableStatementKeys.every((key) => expandedStatementSQLs.has(key))
  const queryAccessItems = detail?.query_access_items ?? []
  const queryAccessConnections = summarizeQueryAccessConnections(queryAccessItems)
  const queryAccessScopeSummary = summarizeQueryAccessScope(queryAccessItems)
  const hasActionPanel = canReview || canWithdraw || canExecute || canReject || canRevoke || canRetryWorkflow || (ticket?.ticket_type === 'sql_export' && detail?.capabilities.can_download_export && exportDownloadURL)
  const shouldShowActionPanel = hasActionPanel && !['completed', 'failed', 'rejected', 'withdrawn', 'stopped', 'interrupted'].includes(ticket?.status ?? '')
  const showExecutionActions = (ticket?.status === 'approved' && canReject) ||
    (ticket?.status === 'pending_execution' && (canExecute || canReject)) ||
    ((ticket?.ticket_type === 'sensitive_query_access' || ticket?.ticket_type === 'query_access') && ticket?.status === 'approved' && canRevoke)
  const canViewWorkflowTrace = isAdminUser(user) && ticket?.status === 'needs_admin_attention'
  const canReapplyTicket = Boolean(
    ticket &&
    ['completed', 'failed', 'interrupted', 'rejected', 'withdrawn'].includes(ticket.status) &&
    (ticket.ticket_type === 'ddl' || ticket.ticket_type === 'dml' || ticket.ticket_type === 'redis_command') &&
    user.permissions.includes('tickets.apply'),
  )
  const generatedRollbacks = detail?.execution_rollbacks.filter((item) => item.status === 'generated') ?? []
  const canCreateRollbackTicket = Boolean(ticket && generatedRollbacks.length > 0 && user.permissions.includes('tickets.apply'))

  async function reloadTicket(_options?: { background?: boolean }) {
    if (!id) {
      return
    }
    const nextDetail = await getTicket(id)
    startTransition(() => {
      setDetail(nextDetail)
    })
  }

  function setStatementSQLExpanded(key: string, expanded: boolean) {
    setExpandedStatementSQLs((current) => {
      const next = new Set(current)
      if (expanded) {
        next.add(key)
      } else {
        next.delete(key)
      }
      return next
    })
  }

  function toggleAllStatementSQLs() {
    setExpandedStatementSQLs((current) => {
      if (allStatementSQLsExpanded) {
        return new Set()
      }
      const next = new Set(current)
      expandableStatementKeys.forEach((key) => next.add(key))
      return next
    })
  }

  async function runAction(
    type: 'approve' | 'reject' | 'withdraw' | 'execute' | 'revoke' | 'retry_workflow',
    action: () => Promise<Ticket | void>,
  ) {
    setActing(type)
    setError('')
    try {
      await action()
      await reloadTicket({ background: true })
      setComment('')
      setReason('')
      pushToast('Ticket updated', 'success')
    } catch (actionError) {
      setError(actionError instanceof ApiError ? actionError.message : 'Action failed. Please try again later.')
    } finally {
      setActing(null)
    }
  }

  async function runStatementAction(executionID: number, action: () => Promise<Ticket | void>) {
    setActingExecutionID(executionID)
    setError('')
    try {
      await action()
      await reloadTicket({ background: true })
    } catch (actionError) {
      setError(actionError instanceof ApiError ? actionError.message : 'Statement action failed. Please try again later.')
    } finally {
      setActingExecutionID(null)
    }
  }

  async function openRollbackPreview() {
    if (!ticket) {
      return
    }
    setRollbackPreviewLoading(true)
    setError('')
    try {
      const response = await previewRollbackTicket(ticket.ticket_no)
      setRollbackPreviewItems(response.items)
      setSelectedRollbackIDs(new Set(response.items.map((item) => item.rollback.id)))
      setRollbackPreviewOpen(true)
    } catch (actionError) {
      setError(actionError instanceof ApiError ? actionError.message : 'Load rollback preview failed. Please try again later.')
    } finally {
      setRollbackPreviewLoading(false)
    }
  }

  async function runRollbackAction() {
    if (!ticket || selectedRollbackIDs.size === 0) {
      return
    }
    setRollbackCreateLoading(true)
    setError('')
    try {
      const response = await createRollbackTicket(ticket.ticket_no, Array.from(selectedRollbackIDs))
      await reloadTicket({ background: true })
      setRollbackPreviewOpen(false)
      pushToast(`Rollback ticket ${response.ticket.ticket_no} created`, 'success')
    } catch (actionError) {
      setError(actionError instanceof ApiError ? actionError.message : 'Create rollback ticket failed. Please try again later.')
    } finally {
      setRollbackCreateLoading(false)
    }
  }

  async function handleDownloadExport() {
    if (!exportDownloadURL || downloadingExport) {
      return
    }

    setDownloadingExport(true)
    try {
      await downloadTicketExport(exportDownloadURL)
    } catch (downloadError) {
      pushToast(downloadError instanceof ApiError ? downloadError.message : 'Failed to download export.', 'error', {
        placement: 'center',
        durationMs: 3600,
      })
    } finally {
      setDownloadingExport(false)
    }
  }

  function handleReapplyTicket() {
    if (!ticket || !canReapplyTicket) {
      return
    }
    navigate('/tickets/new', {
      state: {
        reapplyTicket: {
          title: ticket.title,
          description: ticket.description ?? '',
          ticketType: ticket.ticket_type,
          dbConnectionId: ticket.db_connection_id ?? null,
          databaseName: ticket.database_name ?? '',
          sqlContent: ticket.sql_content,
        },
      },
    })
  }

  return (
    <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <p className="min-w-0 truncate text-[14px] font-semibold text-ink">{ticket ? ticket.title : 'Ticket Detail'}</p>
            {ticket ? (
              <StatusBadge
                status={ticket.status}
                className={cn(
                  statusTransitioning ? 'scale-[1.03] ring-4 ring-accent/15' : '',
                )}
              />
            ) : null}
          </div>
          {ticket ? <p className="mt-1 truncate font-mono text-[12px] font-semibold text-accent">{ticket.ticket_no}</p> : null}
        </div>
        <div className="flex shrink-0 flex-wrap items-center gap-2">
          {canCreateRollbackTicket ? (
            <button
              type="button"
              onClick={() => void openRollbackPreview()}
              disabled={rollbackPreviewLoading}
              className="inline-flex h-9 items-center gap-2 rounded-lg border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-panel-soft disabled:cursor-not-allowed disabled:opacity-60"
            >
              {rollbackPreviewLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
              Rollback
            </button>
          ) : null}
          {canReapplyTicket ? (
            <button
              type="button"
              onClick={handleReapplyTicket}
              className="inline-flex h-9 items-center gap-2 rounded-lg bg-brand px-3 text-[12px] font-semibold text-white transition hover:bg-slate-800"
            >
              <RotateCcw className="h-4 w-4" />
              Resubmit
            </button>
          ) : null}
          <Link
            to="/tickets"
            className="inline-flex h-9 items-center gap-2 rounded-lg border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-panel-soft"
          >
            <ArrowLeft className="h-4 w-4" />
            Back to list
          </Link>
        </div>
      </div>

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      {loading ? (
        <LoadingBlock message="Loading ticket…" className="min-h-[420px] rounded-xl border-border bg-panel" />
      ) : !ticket || !detail ? (
        <div className="rounded-xl border border-border bg-panel p-6 text-sm text-muted shadow-soft">Ticket not found.</div>
      ) : (
        <div className="space-y-3">
          <WorkflowTimeline
            ticket={ticket}
            workflowParticipants={detail.workflow_participants}
            workflowResolution={detail.workflow_resolution}
            activityLogs={detail.activity_logs}
            highlight={statusTransitioning}
          />
          <section className="rounded-xl border border-border bg-panel shadow-soft">
            <div className="px-4 py-4">
              <p className="text-[12px] font-semibold text-faint">Overview</p>
              <DetailTable
                headers={['Ticket Type', 'DB Connection', 'Database / Schema', 'Submitter', 'Reviewer', 'Executor', 'Description', 'Current Status']}
                rows={[[
                  formatTicketTypeLabel(ticket.ticket_type),
                  ticket.ticket_type === 'query_access' ? queryAccessConnections : ticket.db_connection_name || ticket.db_connection_id || 'Not specified',
                  formatTicketDatabaseScope(ticket, queryAccessScopeSummary),
                  formatTicketActor(ticket.submitter_name, ticket.submitter_id),
                  formatTicketReviewerActor(ticket, detail.workflow_resolution),
                  formatTicketExecutorActor(ticket, detail.workflow_resolution),
                  ticket.description || '—',
                  <StatusBadge status={ticket.status} />,
                ]]}
              />
            </div>

            {ticket.ticket_type === 'query_access' ? (
              <div className="px-4 pb-4">
                <p className="text-[12px] font-semibold text-faint">Query Access Details</p>
                <DetailTable
                  headers={['Rule Count', 'Access Duration', 'Approved Until', 'Revoked At', 'Revoked By']}
                  rows={[[
                    queryAccessItems.length,
                    formatQueryAccessDuration(ticket.approved_duration_minutes),
                    ticket.approved_until ? formatDateTime(ticket.approved_until, true) : '—',
                    ticket.revoked_at ? formatDateTime(ticket.revoked_at, true) : '—',
                    formatTicketActor(ticket.revoked_by_name, ticket.revoked_by ?? null),
                  ]]}
                />
                <div className="mt-3 overflow-x-auto rounded-xl border border-border">
                  <DataTable>
                    <DataTableHead>
                      <tr>
                        <DataTableHeaderCell>ID</DataTableHeaderCell>
                        <DataTableHeaderCell>Effect</DataTableHeaderCell>
                        <DataTableHeaderCell>Connection</DataTableHeaderCell>
                        <DataTableHeaderCell>Database</DataTableHeaderCell>
                        <DataTableHeaderCell>Table</DataTableHeaderCell>
                        <DataTableHeaderCell>Summary</DataTableHeaderCell>
                      </tr>
                    </DataTableHead>
                    <DataTableBody>
                      {queryAccessItems.map((item) => (
                        <DataTableRow key={item.id}>
                          <DataTableCell className="align-top">{item.id}</DataTableCell>
                          <DataTableCell className="align-top">
                            <span className={`inline-flex rounded-full px-2 py-1 text-[11px] font-semibold ${
                              item.effect === 'deny' ? 'bg-red-50 text-danger' : 'bg-emerald-50 text-emerald-700'
                            }`}>
                              {item.effect === 'deny' ? 'Deny' : 'Allow'}
                            </span>
                          </DataTableCell>
                          <DataTableCell className="align-top">{formatQueryAccessConnection(item)}</DataTableCell>
                          <DataTableCell className="align-top">{item.database_pattern === '*' ? 'All Databases' : item.database_pattern || item.database_name}</DataTableCell>
                          <DataTableCell className="align-top">{item.table_pattern === '*' ? 'All Tables' : item.table_pattern || item.table_name || '—'}</DataTableCell>
                          <DataTableCell className="align-top">{formatQueryAccessRuleSummary(item)}</DataTableCell>
                        </DataTableRow>
                      ))}
                    </DataTableBody>
                  </DataTable>
                </div>
              </div>
            ) : null}

            {ticket.ticket_type === 'query_access' ? null : statementResults.length === 0 ? (
              <div className="px-4 pb-4">
                <p className="text-[12px] font-semibold text-faint">SQL Content</p>
                <pre className="mt-2 overflow-x-auto rounded-xl border border-border bg-panel-soft p-4 font-mono text-[13px] leading-7 text-ink">
                  <code>{displaySQLContent}</code>
                </pre>
              </div>
            ) : (
              <div className="px-4 pb-4">
                <div>
                  <p className="text-[12px] font-semibold text-faint">Statement Results</p>
                  {expandableStatementKeys.length > 0 ? (
                    <button
                      type="button"
                      onClick={toggleAllStatementSQLs}
                      className="mt-1 inline-flex h-6 items-center gap-1 text-[11px] font-semibold text-muted transition hover:text-primary focus:outline-none focus:ring-2 focus:ring-primary/20"
                    >
                      {allStatementSQLsExpanded ? 'Collapse all SQL' : 'Show all SQL'}
                      <ChevronDown className={`h-3.5 w-3.5 transition-transform ${allStatementSQLsExpanded ? 'rotate-180' : ''}`} />
                    </button>
                  ) : null}
                </div>
                <div className="mt-3 overflow-x-auto rounded-xl border border-border">
                  <DataTable className="w-max min-w-0 table-auto">
                    <colgroup>
                      <col className="w-[28px]" />
                      <col className="w-[36px]" />
                      <col className="w-auto" />
                      {showStatementTableMetadata ? <col className="w-[150px]" /> : null}
                      {showStatementTableMetadata ? <col className="w-[150px]" /> : null}
                      {showStatementScanRows ? <col className="w-[140px]" /> : null}
                      <col className="w-[140px]" />
                      {showReviewMessageColumn ? <col className="w-[180px]" /> : null}
                      {showStatementRowsAffected ? <col className="w-[150px]" /> : null}
                      <col className="w-[150px]" />
                      {showStatementRollback ? <col className="w-[210px]" /> : null}
                      <col className="w-[90px]" />
                      {showErrorMessageColumn ? <col className="w-[220px]" /> : null}
                      <col className="w-[120px]" />
                    </colgroup>
                    <DataTableHead>
                      <tr>
                        <DataTableHeaderCell className="pl-2 pr-1" aria-label="Expand SQL" />
                        <DataTableHeaderCell className="pl-1 pr-2">ID</DataTableHeaderCell>
                        <DataTableHeaderCell className="pl-1 pr-2">SQL</DataTableHeaderCell>
                        {showStatementTableMetadata ? <DataTableHeaderCell>Table Rows</DataTableHeaderCell> : null}
                        {showStatementTableMetadata ? <DataTableHeaderCell>Table Size</DataTableHeaderCell> : null}
                        {showStatementScanRows ? <DataTableHeaderCell>Scan Rows</DataTableHeaderCell> : null}
                        <DataTableHeaderCell>Review Status</DataTableHeaderCell>
                        {showReviewMessageColumn ? <DataTableHeaderCell>Review Message</DataTableHeaderCell> : null}
                        {showStatementRowsAffected ? <DataTableHeaderCell>Rows Affected</DataTableHeaderCell> : null}
                        <DataTableHeaderCell>Execution Status</DataTableHeaderCell>
                        {showStatementRollback ? <DataTableHeaderCell>Rollback</DataTableHeaderCell> : null}
                        <DataTableHeaderCell>Duration</DataTableHeaderCell>
                        {showErrorMessageColumn ? <DataTableHeaderCell>Error Message</DataTableHeaderCell> : null}
                        <DataTableHeaderCell>Action</DataTableHeaderCell>
                      </tr>
                    </DataTableHead>
                    <DataTableBody>
                      {displayStatementResults.map((row) => {
                        const rowKey = statementResultKey(row)
                        const rowExpanded = expandedStatementSQLs.has(rowKey)
                        const rowExpandable = isExpandableSql(row.sql)
                        const rowActionBusy = actingExecutionID === row.executionID
                        const rowCanExecute = Boolean(canExecute && !isFullTicketExecutionRunMode(ticket.execution_run_mode) && (ticket.ticket_type === 'ddl' || ticket.ticket_type === 'dml') && ticket.status !== 'completed' && ticket.status !== 'failed' && row.executionID && row.executionStatus === 'pending')
                        const rowCanStop = Boolean(canExecute && row.executionID && row.executionStatus === 'running')
                        return (
                          <DataTableRow
                            key={rowKey}
                            className={cn(
                              'transition-colors duration-500',
                              row.executionStatus === 'running' ? 'bg-blue-50/35 hover:bg-blue-50/60' : '',
                            )}
                          >
                            <DataTableCell className="pl-2 pr-1 align-middle">
                              {rowExpandable ? (
                                <button
                                  type="button"
                                  onClick={() => setStatementSQLExpanded(rowKey, !rowExpanded)}
                                  className="inline-flex h-6 w-5 shrink-0 items-center justify-center rounded-md text-primary transition hover:bg-panel-soft focus:outline-none focus:ring-2 focus:ring-primary/20"
                                  aria-expanded={rowExpanded}
                                  aria-label={`${rowExpanded ? 'Collapse' : 'Show full'} SQL statement ${row.seq}`}
                                >
                                  {rowExpanded ? <Minus className="h-3.5 w-3.5" /> : <Plus className="h-3.5 w-3.5" />}
                                </button>
                              ) : null}
                            </DataTableCell>
                            <DataTableCell className="pl-1 pr-2 align-middle leading-6">
                              {row.seq}
                            </DataTableCell>
                            <DataTableCell className="w-fit max-w-[600px] min-w-0 overflow-hidden pl-1 pr-2 align-middle">
                              <div className="inline-block w-fit min-w-0 max-w-[590px] overflow-hidden align-middle">
                                <ExpandableSql
                                  value={row.sql}
                                  expanded={rowExpanded}
                                  onExpandedChange={(expanded) => setStatementSQLExpanded(rowKey, expanded)}
                                  showToggle={false}
                                  expandedMaxHeight={false}
                                />
                              </div>
                            </DataTableCell>
                            {showStatementTableMetadata ? (
                              <DataTableCell className="break-words whitespace-pre-line align-middle leading-6 tabular-nums">{formatReviewTableRows(row.tables)}</DataTableCell>
                            ) : null}
                            {showStatementTableMetadata ? (
                              <DataTableCell className="break-words whitespace-pre-line align-middle leading-6">{formatReviewTableSizes(row.tables)}</DataTableCell>
                            ) : null}
                            {showStatementScanRows ? (
                              <DataTableCell className="break-words align-middle leading-6">{formatReviewRows(row.scanRows)}</DataTableCell>
                            ) : null}
                            <DataTableCell className="break-words align-middle leading-6">{row.reviewStatus ?? '—'}</DataTableCell>
                            {showReviewMessageColumn ? (
                              <DataTableCell className="break-words align-middle leading-6 text-muted">{row.reviewMessage || '—'}</DataTableCell>
                            ) : null}
                            {showStatementRowsAffected ? (
                              <DataTableCell className="break-words align-middle leading-6">{row.rowsAffected ?? '—'}</DataTableCell>
                            ) : null}
                            <DataTableCell className="break-words align-middle leading-6"><StatementExecutionBadge status={row.executionStatus} /></DataTableCell>
                            {showStatementRollback ? (
                              <DataTableCell className="align-middle leading-6">
                                <RollbackStatusCell rollback={row.rollback} />
                              </DataTableCell>
                            ) : null}
                            <DataTableCell className="break-words align-middle leading-6">{row.duration ?? '—'}</DataTableCell>
                            {showErrorMessageColumn ? (
                              <DataTableCell className="break-words align-middle leading-6 text-muted">{row.errorMessage || '—'}</DataTableCell>
                            ) : null}
                            <DataTableCell className="align-middle">
                              {rowCanExecute && row.executionID ? (
                                <button
                                  type="button"
                                  onClick={() => void runStatementAction(row.executionID!, () => executeTicketStatement(ticket.ticket_no, row.executionID!))}
                                  disabled={rowActionBusy}
                                  className="inline-flex h-8 items-center gap-1.5 rounded-md border border-border bg-panel px-2.5 text-[12px] font-semibold text-ink transition hover:bg-panel-soft disabled:cursor-not-allowed disabled:opacity-60"
                                >
                                  {rowActionBusy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />}
                                  Execute
                                </button>
                              ) : rowCanStop && row.executionID ? (
                                <button
                                  type="button"
                                  onClick={() => void runStatementAction(row.executionID!, () => stopTicketStatement(ticket.ticket_no, row.executionID!))}
                                  disabled={rowActionBusy}
                                  className="inline-flex h-8 items-center gap-1.5 rounded-md border border-rose-200 bg-rose-50 px-2.5 text-[12px] font-semibold text-rose-700 transition hover:bg-rose-100 disabled:cursor-not-allowed disabled:opacity-60"
                                >
                                  {rowActionBusy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Square className="h-3.5 w-3.5" />}
                                  Stop
                                </button>
                              ) : (
                                <span className="text-muted">—</span>
                              )}
                            </DataTableCell>
                          </DataTableRow>
                        )
                      })}
                    </DataTableBody>
                  </DataTable>
                </div>
              </div>
            )}

            {detail.scopes.length > 0 ? (
              <div className="px-4 pb-4">
                <p className="text-[12px] font-semibold text-faint">Scopes</p>
                <div className="mt-3 space-y-2">
                  {detail.scopes.map((scope) => (
                    <ScopeRow key={scope.id} scope={scope} ticket={ticket} />
                  ))}
                </div>
              </div>
            ) : null}

            {shouldShowActionPanel ? (
              <div className="px-4 pb-4">
                <p className="text-[12px] font-semibold text-faint">Actions</p>
                <div className="mt-3 space-y-4">
                  {(canReview || canWithdraw) && ticket.status === 'pending_review' ? (
                    <div className="p-0">
                      <div className="flex flex-col gap-2">
                        {canReview || canWithdraw ? (
                          <textarea
                            value={comment}
                            onChange={(event) => setComment(event.target.value)}
                            className="min-h-[96px] rounded-lg border border-border bg-white px-3 py-2 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                            placeholder={canReview ? 'Review comment or rejection reason' : 'Withdraw reason (optional)'}
                            disabled={acting !== null}
                          />
                        ) : null}
                        <div className="flex flex-wrap items-center gap-2">
                          {canReview ? (
                            <>
                              <button
                                type="button"
                                disabled={acting !== null}
                                onClick={() => void runAction('approve', () => approveTicket(ticket.ticket_no, comment))}
                                className="inline-flex h-9 w-auto items-center justify-center gap-2 rounded-md bg-brand px-3 text-[12px] font-semibold text-white transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
                              >
                                {acting === 'approve' ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldCheck className="h-4 w-4" />}
                                Approve
                              </button>
                              <button
                                type="button"
                                disabled={acting !== null || comment.trim() === ''}
                                onClick={() => void runAction('reject', () => rejectTicket(ticket.ticket_no, comment.trim()))}
                                className="inline-flex h-9 w-auto items-center justify-center gap-2 rounded-md border border-danger/20 bg-red-50 px-3 text-[12px] font-semibold text-danger transition hover:bg-red-100 disabled:cursor-not-allowed disabled:opacity-50"
                              >
                                {acting === 'reject' ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldX className="h-4 w-4" />}
                                Reject
                              </button>
                            </>
                          ) : null}
                          {canWithdraw ? (
                            <button
                              type="button"
                              disabled={acting !== null}
                              onClick={() => setConfirmAction('withdraw')}
                              className="inline-flex h-9 w-auto items-center justify-center gap-2 rounded-md border border-danger/20 bg-red-50 px-3 text-[12px] font-semibold text-danger transition hover:bg-red-100 disabled:cursor-not-allowed disabled:opacity-50"
                            >
                              {acting === 'withdraw' ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldX className="h-4 w-4" />}
                              Withdraw Ticket
                            </button>
                          ) : null}
                        </div>
                      </div>
                    </div>
                  ) : null}

                  {canRetryWorkflow && ticket.status === 'needs_admin_attention' ? (
                    <div className="p-0">
                      <button
                        type="button"
                        disabled={acting !== null}
                        onClick={() => void runAction('retry_workflow', () => retryWorkflowResolution(ticket.ticket_no).then((response) => response.ticket))}
                        className="inline-flex h-9 w-auto items-center justify-center gap-2 rounded-md border border-orange-200 bg-orange-50 px-3 text-[12px] font-semibold text-orange-700 transition hover:bg-orange-100 disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        {acting === 'retry_workflow' ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldCheck className="h-4 w-4" />}
                        Retry Workflow Resolution
                      </button>
                    </div>
                  ) : null}

                  {showExecutionActions ? (
                    <div className="p-0">
                      <div className="flex flex-col gap-2">
                        {canReject && (ticket.status === 'approved' || ticket.status === 'pending_execution') ? (
                          <>
                            <label className="flex flex-col gap-1.5">
                              <textarea
                                value={reason}
                                onChange={(event) => setReason(event.target.value)}
                                className="min-h-[96px] rounded-lg border border-border bg-white px-3 py-2 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                                placeholder="Execution comment or rejection reason"
                                disabled={acting !== null}
                              />
                            </label>
                          </>
                        ) : null}
                        {ticket.status === 'pending_execution' ? (
                          <div className="flex flex-wrap items-center gap-2">
                            <button
                              type="button"
                              disabled={acting !== null || !canExecute}
                              onClick={() => setConfirmAction('execute')}
                              className="inline-flex h-9 w-auto items-center justify-center gap-2 rounded-md bg-brand px-3 text-[12px] font-semibold text-white transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
                            >
                              {acting === 'execute' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}
                              Execute
                            </button>
                            {canReject ? (
                              <button
                                type="button"
                                disabled={acting !== null || reason.trim() === ''}
                                onClick={() => void runAction('reject', () => rejectTicket(ticket.ticket_no, reason.trim()))}
                                className="inline-flex h-9 w-auto items-center justify-center gap-2 rounded-md border border-danger/20 bg-red-50 px-3 text-[12px] font-semibold text-danger transition hover:bg-red-100 disabled:cursor-not-allowed disabled:opacity-50"
                              >
                                {acting === 'reject' ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldX className="h-4 w-4" />}
                                Reject
                              </button>
                            ) : null}
                          </div>
                        ) : null}
                        {canRevoke ? (
                          <button
                            type="button"
                            disabled={acting !== null || ticket.status !== 'approved' || (ticket.ticket_type !== 'sensitive_query_access' && ticket.ticket_type !== 'query_access')}
                            onClick={() => setConfirmAction('revoke')}
                            className="inline-flex h-9 w-auto self-start items-center justify-center gap-2 rounded-md border border-danger/20 bg-red-50 px-3 text-[12px] font-semibold text-danger transition hover:bg-red-100 disabled:cursor-not-allowed disabled:opacity-50"
                          >
                            {acting === 'revoke' ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldX className="h-4 w-4" />}
                            {ticket.ticket_type === 'query_access' ? 'Revoke Query Access' : 'Revoke Access'}
                          </button>
                        ) : null}
                      </div>
                    </div>
                  ) : null}

                  {ticket.ticket_type === 'sql_export' && detail.capabilities.can_download_export && exportDownloadURL ? (
                    <div className="p-0">
                      <button
                        type="button"
                        onClick={() => void handleDownloadExport()}
                        disabled={downloadingExport}
                        className="inline-flex h-9 w-auto items-center justify-center gap-2 rounded-md bg-brand px-3 text-[12px] font-semibold text-white transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        {downloadingExport ? <Loader2 className="h-4 w-4 animate-spin" /> : <Download className="h-4 w-4" />}
                        {downloadingExport ? 'Downloading…' : 'Download Export'}
                      </button>
                      <p className="mt-3 text-[12px] text-muted">
                        Expires: {detail.export_request?.expires_at ? formatDateTime(detail.export_request.expires_at, true) : '—'}
                      </p>
                      {detail.export_request?.downloaded_at ? (
                        <p className="mt-1 text-[12px] text-muted">
                          First downloaded: {formatDateTime(detail.export_request.downloaded_at, true)}
                        </p>
                      ) : null}
                    </div>
                  ) : null}
                </div>
              </div>
            ) : null}

            <div className="px-4 pb-4">
              <button
                type="button"
                onClick={() => setOtherDetailsOpen((current) => !current)}
                className="inline-flex items-center gap-1.5 text-left"
                aria-expanded={otherDetailsOpen}
              >
                <span className="text-[12px] font-semibold text-faint">Other Details</span>
                <ChevronDown className={`h-4 w-4 text-muted transition-transform ${otherDetailsOpen ? 'rotate-180' : ''}`} />
              </button>
              {otherDetailsOpen ? (
                <div className="space-y-4">
                  {executionRuntimeRows.length > 0 ? (
                    <div>
                      <p className="mt-3 text-[12px] font-semibold text-faint">Statement Runtime</p>
                      <DetailTable
                        headers={['ID', 'Sent To DB', 'DB Process', 'Outcome', 'Reason']}
                        rows={executionRuntimeRows.map((row) => [
                          row.seq,
                          row.sentToDBAt ? formatDateTime(row.sentToDBAt, true) : '—',
                          formatExecutionRuntimeProcess(row),
                          row.outcomeConfidence || '—',
                          row.interruptionReason || '—',
                        ])}
                      />
                    </div>
                  ) : null}
                  <div>
                    <p className="mt-3 text-[12px] font-semibold text-faint">Activity Log</p>
                    <DetailTable
                      headers={['Action', 'Actor', 'Timestamp', 'Detail']}
                      rows={detail.activity_logs.map((log) => [
                        formatActivityAction(log.action_type),
                        log.actor_name?.trim() ? log.actor_name : log.actor_id ? String(log.actor_id) : 'System',
                        formatDateTime(log.created_at, true),
                        formatActivityDetail(log),
                      ])}
                    />
                  </div>
                </div>
              ) : null}
            </div>

            {canViewWorkflowTrace && detail.workflow_resolution_trace ? (
              <div className="px-4 pb-4">
                <button
                  type="button"
                  onClick={() => setDebugTraceOpen((current) => !current)}
                  className="inline-flex items-center gap-1.5 text-left"
                  aria-expanded={debugTraceOpen}
                >
                  <span className="text-[12px] font-semibold text-faint">Debug / Resolution Trace</span>
                  <ChevronDown className={`h-4 w-4 text-muted transition-transform ${debugTraceOpen ? 'rotate-180' : ''}`} />
                </button>
                {debugTraceOpen ? (
                  <DebugWorkflowResolutionTrace
                    trace={detail.workflow_resolution_trace}
                    participants={detail.workflow_participants}
                    ticket={ticket}
                  />
                ) : null}
              </div>
            ) : null}
          </section>
        </div>
      )}

      <ConfirmDialog
        open={rollbackPreviewOpen}
        title="Create Rollback Ticket"
        panelClassName="max-w-5xl"
        description={(
          <div className="grid max-h-[70vh] gap-3 overflow-y-auto pr-1">
            <p className="text-[12px] leading-5 text-muted">
              Review generated rollback SQL before creating a new DML ticket. Select one or more statements to include.
            </p>
            {rollbackPreviewItems.length === 0 ? (
              <div className="rounded-lg border border-border bg-panel-soft px-3 py-4 text-[12px] text-muted">
                No generated rollback SQL is available.
              </div>
            ) : (
              <div className="grid gap-3">
                {rollbackPreviewItems.map((item) => {
                  const selected = selectedRollbackIDs.has(item.rollback.id)
                  return (
                    <div key={item.rollback.id} className="grid gap-2 rounded-lg border border-border bg-white p-3 text-left">
                      <label className="flex cursor-pointer items-center gap-2 text-[12px] font-semibold text-ink">
                        <input
                          type="checkbox"
                          checked={selected}
                          onChange={(event) => {
                            setSelectedRollbackIDs((current) => {
                              const next = new Set(current)
                              if (event.target.checked) {
                                next.add(item.rollback.id)
                              } else {
                                next.delete(item.rollback.id)
                              }
                              return next
                            })
                          }}
                        />
                        Statement #{item.rollback.seq}
                      </label>
                      <div className="grid gap-2 lg:grid-cols-2">
                        <div>
                          <p className="mb-1 text-[11px] font-semibold uppercase text-faint">Original SQL</p>
                          <pre className="max-h-40 overflow-auto rounded-lg border border-border bg-panel-soft p-2 font-mono text-[11px] leading-5 text-ink">{item.original_sql || '—'}</pre>
                        </div>
                        <div>
                          <p className="mb-1 text-[11px] font-semibold uppercase text-faint">Rollback SQL</p>
                          <pre className="max-h-40 overflow-auto rounded-lg border border-border bg-panel-soft p-2 font-mono text-[11px] leading-5 text-ink">{item.rollback_sql}</pre>
                        </div>
                      </div>
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        )}
        confirmLabel="Create Ticket"
        loading={rollbackCreateLoading}
        confirmDisabled={rollbackPreviewItems.length === 0 || selectedRollbackIDs.size === 0}
        onCancel={() => {
          if (!rollbackCreateLoading) {
            setRollbackPreviewOpen(false)
          }
        }}
        onConfirm={() => {
          void runRollbackAction()
        }}
      />

      <ConfirmDialog
        open={confirmAction !== null}
        title={
          confirmAction === 'withdraw'
            ? 'Withdraw Ticket'
            : confirmAction === 'execute'
                ? 'Execute Ticket'
                : ticket?.ticket_type === 'query_access'
                  ? 'Revoke Query Access'
                  : 'Revoke Sensitive Access'
        }
        description={
          confirmAction === 'withdraw'
            ? 'Withdraw this ticket now? Reviewers will no longer process it.'
            : confirmAction === 'execute'
              ? 'Execute statements in submission order. Execution stops if any statement fails.'
              : ticket?.ticket_type === 'query_access'
                ? 'Revoke this query access ticket early? The granted query scope will be invalidated from the next query onwards.'
                : 'Revoke this sensitive access ticket early? Access will be invalidated from the next query onwards.'
        }
        confirmLabel={
          confirmAction === 'withdraw'
            ? 'Withdraw'
            : confirmAction === 'execute'
                ? 'Execute'
                : 'Revoke'
        }
        loading={confirmAction !== null && acting === confirmAction}
        onCancel={() => setConfirmAction(null)}
        onConfirm={() => {
          if (!ticket) return
          if (confirmAction === 'withdraw') {
            void runAction('withdraw', () => withdrawTicket(ticket.ticket_no, comment.trim())).finally(() => setConfirmAction(null))
          }
          if (confirmAction === 'execute') {
            void runAction('execute', () => executeTicket(ticket.ticket_no, reason)).finally(() => setConfirmAction(null))
          }
          if (confirmAction === 'revoke') {
            void runAction('revoke', () => revokeTicket(ticket.ticket_no)).finally(() => setConfirmAction(null))
          }
        }}
      />
    </div>
  )
}

function ScopeRow({ scope, ticket }: { scope: TicketScope; ticket: Ticket }) {
  if ((ticket.ticket_type === 'sql_export' || ticket.ticket_type === 'sensitive_query_access') && scope.is_sensitive) {
    return (
      <div className="rounded-lg border border-border bg-panel-soft px-3 py-2 text-[12px] text-ink">
        <div className="flex flex-wrap items-center gap-2.5">
          <span className="font-mono text-[12px] text-ink">{scope.column_name}</span>
          <span className="rounded-full border border-rose-200 bg-rose-50 px-2 py-0.5 text-[10px] font-semibold text-rose-700">Sensitive column</span>
        </div>
      </div>
    )
  }

  const connectionLabel = scope.connection_id === ticket.db_connection_id && ticket.db_connection_name
    ? ticket.db_connection_name
    : `Connection #${scope.connection_id}`
  const scopeParts = [
    { label: 'Connection', value: connectionLabel },
    { label: 'Database', value: scope.database_name || '—' },
    ...(scope.schema_name ? [{ label: 'Schema', value: scope.schema_name }] : []),
    { label: 'Table', value: scope.table_name || '—' },
    { label: 'Column', value: scope.column_name },
  ]

  return (
    <div className="rounded-lg border border-border bg-panel-soft px-3 py-2 text-[12px] text-ink">
      <div className="flex flex-wrap items-center gap-2.5">
        {scopeParts.map((part) => (
          <span key={part.label} className="inline-flex items-center gap-1.5">
            <span className="text-[10px] font-semibold uppercase tracking-[0.08em] text-faint">{part.label}</span>
            <span className="font-mono text-[12px] text-ink">{part.value}</span>
          </span>
        ))}
        {scope.is_sensitive ? (
          <span className="rounded-full border border-rose-200 bg-rose-50 px-2 py-0.5 text-[10px] font-semibold text-rose-700">Sensitive column</span>
        ) : null}
        <span className="rounded-full border border-border bg-white px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted">
          {scope.source_kind}
        </span>
      </div>
    </div>
  )
}
