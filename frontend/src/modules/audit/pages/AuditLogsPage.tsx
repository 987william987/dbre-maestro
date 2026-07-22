import { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { CalendarDays, ChevronLeft, ChevronRight, Download, X } from 'lucide-react'
import { ApiError } from '@/shared/api/client'
import { useAuth } from '@/shared/auth/AuthContext'
import { getBrowserTimeZone } from '@/shared/lib/format'
import { useDebouncedValue } from '@/shared/lib/useDebouncedValue'
import type { AuditLog } from '@/shared/types/audit'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { exportAuditLogs, listAuditLogs } from '@/modules/audit/api'
import { useToast } from '@/shared/ui/ToastContext'
import { SearchInput } from '@/shared/ui/SearchInput'
import {
  DataTable,
  DataTableBody,
  DataTableCell,
  DataTableHead,
  DataTableHeaderCell,
  DataTableRow,
  DataTableScroll,
  DataTableSurface,
} from '@/shared/ui/DataTable'

const PAGE_SIZE = 20

const ACTION_OPTIONS = [
  { value: '', label: 'All Actions' },
  { value: 'login', label: 'Login' },
  { value: 'login_failed', label: 'Login Failed' },
  { value: 'logout', label: 'Logout' },
  { value: 'mfa_enable', label: 'Enable MFA' },
  { value: 'mfa_failed', label: 'MFA Failed' },
  { value: 'refresh_token_reuse_detected', label: 'Refresh Token Reuse Detected' },
  { value: 'session_revoke', label: 'Revoke Session' },
  { value: 'session_revoke_all', label: 'Revoke All Sessions' },
  { value: 'auth_rate_limited', label: 'Auth Rate Limited' },
  { value: 'auth_group_security_denied', label: 'Auth Group Security Denied' },
  { value: 'user_security_denied', label: 'User Security Denied' },
  { value: 'user_create', label: 'Create User' },
  { value: 'user_update', label: 'Update User' },
  { value: 'user_delete', label: 'Delete User' },
  { value: 'user_session_revoke', label: 'Revoke User Session' },
  { value: 'user_session_revoke_all', label: 'Revoke All User Sessions' },
  { value: 'user_mfa_reset', label: 'Reset User MFA' },
  { value: 'user_mfa_reset_break_glass', label: 'Break Glass MFA Reset' },
  { value: 'user_membership_add', label: 'Add Group Membership' },
  { value: 'user_membership_remove', label: 'Remove Group Membership' },
  { value: 'user_permission_add', label: 'Add User Permission' },
  { value: 'setting_change', label: 'Setting Change' },
  { value: 'settings_update', label: 'Update Settings' },
  { value: 'workflow_rules_update', label: 'Update Workflow Rules' },
  { value: 'ticket_submit', label: 'Submit Ticket' },
  { value: 'ticket_approve', label: 'Approve Ticket' },
  { value: 'ticket_auto_approve', label: 'Auto Approve Ticket' },
  { value: 'ticket_reject', label: 'Reject Ticket' },
  { value: 'ticket_withdraw', label: 'Withdraw Ticket' },
  { value: 'ticket_execute_start', label: 'Start Execution' },
  { value: 'ticket_execute_complete', label: 'Execution Complete' },
  { value: 'ticket_execute_failed', label: 'Execution Failed' },
  { value: 'ticket_schedule', label: 'Schedule Execution' },
  { value: 'ticket_stop', label: 'Stop Ticket' },
  { value: 'ticket_revoke', label: 'Revoke Ticket Access' },
  { value: 'ticket_forbidden_access', label: 'Forbidden Ticket Access' },
  { value: 'workflow_resolution_failed', label: 'Workflow Resolution Failed' },
  { value: 'workflow_resolution_retry', label: 'Retry Workflow Resolution' },
  { value: 'workflow_resolution_retry_failed', label: 'Workflow Resolution Retry Failed' },
  { value: 'workflow_auto_execute_start', label: 'Workflow Auto Execute Start' },
  { value: 'workflow_auto_execute_complete', label: 'Workflow Auto Execute Complete' },
  { value: 'workflow_auto_execute_failed', label: 'Workflow Auto Execute Failed' },
  { value: 'query_execute', label: 'Execute Query' },
  { value: 'query_blocked', label: 'Query Blocked' },
  { value: 'export_create', label: 'Create Export' },
  { value: 'export_approve', label: 'Approve Export' },
  { value: 'export_reject', label: 'Reject Export' },
  { value: 'export_download', label: 'Download Export' },
  { value: 'export_download_failed', label: 'Download Export Failed' },
  { value: 'audit_export', label: 'Export Audit Log' },
  { value: 'notification_delivery', label: 'Notification Delivery' },
  { value: 'notification_failure', label: 'Notification Failure' },
  { value: 'query_access_rule_create', label: 'Create Query Access Rule' },
  { value: 'query_access_rule_update', label: 'Update Query Access Rule' },
  { value: 'query_access_rule_revoke', label: 'Revoke Query Access Rule' },
  { value: 'scheduled_sql_report_create', label: 'Create Scheduled SQL Report' },
  { value: 'scheduled_sql_report_update', label: 'Update Scheduled SQL Report' },
  { value: 'scheduled_sql_report_delete', label: 'Delete Scheduled SQL Report' },
  { value: 'scheduled_sql_report_run', label: 'Run Scheduled SQL Report' },
  { value: 'scheduled_sql_report_run_failed', label: 'Scheduled SQL Report Run Failed' },
  { value: 'scheduled_sql_report_delivery_failed', label: 'Scheduled SQL Report Delivery Failed' },
] as const

const RESOURCE_OPTIONS = [
  { value: '', label: 'All Resources' },
  { value: 'db_connection', label: 'DB Connection' },
  { value: 'ticket', label: 'Ticket' },
  { value: 'user', label: 'User' },
  { value: 'export', label: 'Export' },
  { value: 'audit_log', label: 'Audit Log' },
  { value: 'query_access_rule', label: 'Query Access Rule' },
] as const

export function AuditLogsPage() {
  const { user } = useAuth()
  const { pushToast } = useToast()
  const [logs, setLogs] = useState<AuditLog[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [filters, setFilters] = useState({
    actionType: '',
    actorKeyword: '',
    resourceType: '',
    resourceKeyword: '',
    from: '',
    to: '',
  })
  const [offset, setOffset] = useState(0)
  const [selectedLog, setSelectedLog] = useState<AuditLog | null>(null)
  const canExport = user?.permissions.includes('audit_logs.write') ?? false
  const query = useMemo(() => ({ filters, offset }), [filters, offset])
  const debouncedQuery = useDebouncedValue(query, 300)

  async function loadLogs(nextFilters: typeof filters, nextOffset: number) {
    setLoading(true)
    setError('')
    const resourceFilter = resolveResourceFilter(nextFilters.resourceType, nextFilters.resourceKeyword)
    try {
      const response = await listAuditLogs({
        actionType: nextFilters.actionType,
        actorName: nextFilters.actorKeyword.trim(),
        resourceType: resourceFilter.resourceType,
        resourceName: resourceFilter.resourceName,
        from: toRFC3339(nextFilters.from),
        to: toRFC3339(nextFilters.to),
        offset: nextOffset,
        limit: PAGE_SIZE,
      })
      setLogs(response.logs)
      setTotal(response.total)
    } catch (loadError) {
      setError(loadError instanceof ApiError ? loadError.message : 'Failed to load audit logs.')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void loadLogs(debouncedQuery.filters, debouncedQuery.offset)
  }, [debouncedQuery])

  function updateFilters(patch: Partial<typeof filters>) {
    setFilters((current) => ({ ...current, ...patch }))
    setOffset(0)
  }

  async function handleExport() {
    const resourceFilter = resolveResourceFilter(filters.resourceType, filters.resourceKeyword)
    try {
      const response = await exportAuditLogs({
        actionType: filters.actionType,
        actorName: filters.actorKeyword.trim(),
        resourceType: resourceFilter.resourceType,
        resourceName: resourceFilter.resourceName,
        from: toRFC3339(filters.from),
        to: toRFC3339(filters.to),
      })
      const blob = await response.blob()
      const url = window.URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = getDownloadFilename(response.headers.get('content-disposition')) ?? 'audit-logs.csv'
      document.body.appendChild(anchor)
      anchor.click()
      document.body.removeChild(anchor)
      window.URL.revokeObjectURL(url)
      pushToast('Audit log export started.', 'success', { placement: 'center' })
    } catch (exportError) {
      pushToast(exportError instanceof ApiError ? exportError.message : 'Failed to export audit logs.', 'error', { placement: 'center', durationMs: 3600 })
    }
  }

  return (
    <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
      <section>
        <div className="py-1">
          <div className="flex flex-wrap items-end gap-2.5">
            <FilterHint
              hint="Select a common action event, e.g. login, logout, setting change."
              className="min-w-[180px] flex-1 xl:max-w-[210px]"
            >
              <SelectField
                value={filters.actionType}
                onChange={(value) => updateFilters({ actionType: value })}
                options={ACTION_OPTIONS}
              />
            </FilterHint>
            <FilterHint
              hint="Narrow by resource type, e.g. DB connection, ticket, or user."
              className="min-w-[180px] flex-1 xl:max-w-[210px]"
            >
              <SelectField
                value={filters.resourceType}
                onChange={(value) => updateFilters({ resourceType: value })}
                options={RESOURCE_OPTIONS}
              />
            </FilterHint>
            <FilterHint
              hint="Filter by actor name keyword, e.g. admin or william."
              className="min-w-[160px] flex-1 xl:max-w-[180px]"
            >
              <SearchInput
                value={filters.actorKeyword}
                onChange={(event) => updateFilters({ actorKeyword: event.target.value })}
                placeholder="Actor"
              />
            </FilterHint>
            <FilterHint
              hint="Filter by resource name keyword, e.g. a connection name or ticket title."
              className="min-w-[170px] flex-1 xl:max-w-[190px]"
            >
              <SearchInput
                value={filters.resourceKeyword}
                onChange={(event) => updateFilters({ resourceKeyword: event.target.value })}
                placeholder="Resource"
              />
            </FilterHint>
            <FilterHint
              hint="Set the start date and time. Only events after this point will be shown."
              className="min-w-[210px] flex-1 xl:max-w-[250px]"
            >
              <DateTimeField
                value={filters.from}
                onChange={(value) => updateFilters({ from: value })}
                placeholder="Start date and time"
                presets={FROM_DATE_PRESETS}
              />
            </FilterHint>
            <FilterHint
              hint="Set the end date and time. Only events before this point will be shown."
              className="min-w-[210px] flex-1 xl:max-w-[250px]"
            >
              <DateTimeField
                value={filters.to}
                onChange={(value) => updateFilters({ to: value })}
                placeholder="End date and time"
                presets={TO_DATE_PRESETS}
              />
            </FilterHint>

            {canExport ? (
              <button
                type="button"
                onClick={handleExport}
                className="inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg border border-border bg-white px-4 text-[13px] font-bold text-ink transition hover:bg-page xl:ml-auto"
              >
                <Download className="h-4 w-4" />
                Export
              </button>
            ) : null}
          </div>
        </div>
      </section>

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      <DataTableSurface>
        {loading ? (
          <LoadingBlock message="Loading audit logs..." className="h-60 rounded-none border-0" />
        ) : logs.length === 0 ? (
          <div className="flex h-60 items-center justify-center text-sm text-muted">No matching audit logs.</div>
        ) : (
          <DataTableScroll>
            <DataTable>
              <DataTableHead>
                <tr>
                  <DataTableHeaderCell>Created</DataTableHeaderCell>
                  <DataTableHeaderCell>Actor</DataTableHeaderCell>
                  <DataTableHeaderCell>Action</DataTableHeaderCell>
                  <DataTableHeaderCell>Resource</DataTableHeaderCell>
                  <DataTableHeaderCell>IP</DataTableHeaderCell>
                  <DataTableHeaderCell>Details</DataTableHeaderCell>
                </tr>
              </DataTableHead>
              <DataTableBody>
                {logs.map((log) => (
                  <DataTableRow key={log.id}>
                    <DataTableCell className="whitespace-nowrap">{formatAuditDateTime(log.created_at, true)}</DataTableCell>
                    <DataTableCell className="whitespace-nowrap">{formatActor(log)}</DataTableCell>
                    <DataTableCell className="whitespace-nowrap">{formatActionType(log.action_type)}</DataTableCell>
                    <DataTableCell className="max-w-[360px]">
                      <div className="truncate" title={formatResource(log)}>
                        {formatResource(log)}
                      </div>
                    </DataTableCell>
                    <DataTableCell className="whitespace-nowrap">{formatIPAddress(log.ip_address)}</DataTableCell>
                    <DataTableCell>
                      <button
                        type="button"
                        onClick={() => setSelectedLog(log)}
                        className="inline-flex h-8 items-center justify-center rounded-md border border-border bg-panel-soft px-3 text-[12px] font-medium text-muted transition hover:bg-page hover:text-ink"
                      >
                        View
                      </button>
                    </DataTableCell>
                  </DataTableRow>
                ))}
              </DataTableBody>
            </DataTable>
          </DataTableScroll>
        )}
      </DataTableSurface>

      <div className="flex items-center justify-between px-1">
        <p className="text-[12px] text-muted">
          {total} total, showing {logs.length === 0 ? 0 : offset + 1} - {Math.min(offset + logs.length, total)}
        </p>
        <div className="flex gap-2">
          <button
            type="button"
            disabled={offset === 0}
            onClick={() => setOffset((current) => Math.max(0, current - PAGE_SIZE))}
            className="inline-flex h-9 items-center justify-center rounded-md border border-border bg-panel px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:opacity-50"
          >
            Previous
          </button>
          <button
            type="button"
            disabled={offset + PAGE_SIZE >= total}
            onClick={() => setOffset((current) => current + PAGE_SIZE)}
            className="inline-flex h-9 items-center justify-center rounded-md border border-border bg-panel px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:opacity-50"
          >
            Next
          </button>
        </div>
      </div>

      {selectedLog ? createPortal(
        <div className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-950/28 px-3 py-3 sm:px-4 sm:py-4">
          <div
            role="dialog"
            aria-modal="true"
            aria-labelledby="audit-log-detail-title"
            className="flex max-h-[min(720px,calc(100vh-2rem))] w-full max-w-[760px] flex-col overflow-hidden rounded-xl border border-border bg-panel shadow-[0_22px_60px_rgba(15,23,42,0.18)]"
          >
            <div className="flex items-start justify-between border-b border-border/80 px-5 py-4">
              <div>
                <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">Audit Detail</p>
                <h3 id="audit-log-detail-title" className="mt-1 text-[22px] font-bold tracking-[-0.03em] text-ink">
                  {formatActionType(selectedLog.action_type)}
                </h3>
              </div>
              <button
                type="button"
                onClick={() => setSelectedLog(null)}
                className="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-border bg-panel-soft text-muted transition hover:bg-page hover:text-ink"
                aria-label="Close"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            <div className="grid gap-4 overflow-y-auto px-5 py-4">
              <section className="grid gap-3 sm:grid-cols-2">
                <InfoBox label="Timestamp" value={formatAuditDateTime(selectedLog.created_at, true)} />
                <InfoBox label="Actor" value={formatActor(selectedLog)} />
                <InfoBox label="Resource" value={formatResource(selectedLog)} />
                <InfoBox label="Source IP" value={formatIPAddress(selectedLog.ip_address)} />
              </section>

              <section className="rounded-xl border border-border bg-panel shadow-soft">
                <div className="border-b border-border/80 px-4 py-3">
                  <p className="text-[13px] font-semibold text-ink">Summary</p>
                </div>
                <div className="grid gap-2 px-4 py-4 sm:grid-cols-2">
                  {buildDetailSummary(selectedLog).map((item) => (
                    <InfoBox key={item.label} label={item.label} value={item.value} />
                  ))}
                </div>
              </section>

              <section className="rounded-xl border border-border bg-panel shadow-soft">
                <div className="border-b border-border/80 px-4 py-3">
                  <p className="text-[13px] font-semibold text-ink">Raw Details</p>
                </div>
                <div className="px-4 py-4">
                  <pre className="overflow-x-auto rounded-lg bg-panel-soft px-3 py-3 text-[12px] text-muted">
                    {selectedLog.details ? JSON.stringify(selectedLog.details, null, 2) : '—'}
                  </pre>
                </div>
              </section>
            </div>
          </div>
        </div>,
        document.body,
      ) : null}
    </div>
  )
}

function SelectField({
  value,
  onChange,
  options,
}: {
  value: string
  onChange: (value: string) => void
  options: ReadonlyArray<{ value: string; label: string }>
}) {
  return (
    <DropdownSelect
      ariaLabel="Audit filter select"
      value={value}
      onChange={onChange}
      options={options}
    />
  )
}

function FilterHint({
  hint,
  className,
  children,
}: {
  hint: string
  className?: string
  children: React.ReactNode
}) {
  return (
    <div className={`group relative ${className ?? ''}`}>
      {children}
      <div className="pointer-events-none absolute left-0 top-[calc(100%+8px)] z-20 hidden w-64 rounded-md border border-border bg-white px-3 py-2 text-[11px] font-medium text-muted shadow-soft group-hover:block">
        {hint}
      </div>
    </div>
  )
}

function InfoBox({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-border bg-panel-soft px-3 py-3">
      <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">{label}</p>
      <p className="mt-1 whitespace-pre-wrap break-words text-[13px] text-ink">{value || '—'}</p>
    </div>
  )
}

function formatActor(log: AuditLog) {
  if (log.actor_name?.trim()) {
    return log.actor_name
  }
  if (log.actor_id) {
    return `User #${log.actor_id}`
  }
  return 'System'
}

function formatActionType(actionType: string) {
  const option = ACTION_OPTIONS.find((item) => item.value === actionType)
  if (option) {
    return option.label
  }
  return actionType
    .split('_')
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}

function formatResourceType(resourceType?: string | null) {
  if (!resourceType) {
    return 'Unspecified Resource'
  }
  const option = RESOURCE_OPTIONS.find((item) => item.value === resourceType)
  return option?.label ?? resourceType
}

function normalizeResourceSearchValue(value: string) {
  return value.trim().toLowerCase().replace(/[\s_-]+/g, ' ')
}

function resolveResourceFilter(selectedResourceType: string, resourceKeyword: string) {
  const normalizedKeyword = normalizeResourceSearchValue(resourceKeyword)
  const matchedType = RESOURCE_OPTIONS.find((option) => option.value && normalizeResourceSearchValue(option.label) === normalizedKeyword)?.value ?? ''

  if (matchedType) {
    return {
      resourceType: selectedResourceType || matchedType,
      resourceName: '',
    }
  }

  return {
    resourceType: selectedResourceType,
    resourceName: resourceKeyword.trim(),
  }
}

function formatResource(log: AuditLog) {
  const label = formatResourceType(log.resource_type)
  const detailsRecord = parseAuditDetails(log.details)
  const detailName = typeof detailsRecord?.name === 'string' && detailsRecord.name.trim() ? detailsRecord.name.trim() : ''
  const ticketNo = stringDetail(detailsRecord, 'ticket_no') || stringDetail(detailsRecord, 'resource_ref')
  const connectionName = stringDetail(detailsRecord, 'connection_name')
  const exportID = stringDetail(detailsRecord, 'export_id')

  if (detailName) {
    return `${label} · ${detailName}`
  }
  if (ticketNo) {
    return `${label} · ${ticketNo}`
  }
  if (exportID) {
    return `${label} · Export #${exportID}`
  }
  if (connectionName) {
    return `${label} · ${connectionName}`
  }
  if (label === 'Unspecified Resource' && log.resource_id) {
    return `Unspecified Resource · ${log.resource_id}`
  }
  return label
}

function formatIPAddress(ipAddress?: string | null) {
  return ipAddress?.trim() ? ipAddress : 'System Event'
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function parseAuditDetails(value: unknown): Record<string, unknown> | null {
  if (isRecord(value)) {
    return value
  }
  if (typeof value !== 'string') {
    return null
  }
  const trimmed = value.trim()
  if (!trimmed.startsWith('{') || !trimmed.endsWith('}')) {
    return null
  }
  try {
    const parsed = JSON.parse(trimmed)
    return isRecord(parsed) ? parsed : null
  } catch {
    return null
  }
}

function buildDetailSummary(log: AuditLog) {
  const details = parseAuditDetails(log.details) ?? {}
  const usedKeys = new Set<string>()
  const items = [
    { label: 'Action', value: formatActionType(log.action_type) },
    { label: 'Resource', value: formatResource(log) },
    ...buildStandardDetailSummary(details, usedKeys),
    ...buildGenericDetailSummary(details, usedKeys),
  ].filter((item) => item.value)
  if (items.length > 0) {
    return items
  }
  return [{ label: 'Details', value: formatUnknownDetail(log.details) }]
}

function buildStandardDetailSummary(details: Record<string, unknown>, usedKeys: Set<string>) {
  const definitions: Array<{ label: string; keys: string[]; format?: (details: Record<string, unknown>) => string }> = [
    { label: 'Export', keys: ['export_id', 'status'], format: formatExport },
    { label: 'Ticket', keys: ['ticket_no', 'resource_ref', 'ticket_id'] },
    { label: 'Title', keys: ['ticket_title', 'title', 'name'] },
    { label: 'Type', keys: ['ticket_type', 'type', 'notification_type'] },
    { label: 'Connection', keys: ['connection_name', 'connection_id', 'db_connection_id'] },
    { label: 'DB Type', keys: ['db_type'] },
    { label: 'Database', keys: ['database_name', 'default_database_name', 'database_pattern'] },
    { label: 'Schema', keys: ['schema_name'] },
    { label: 'Table', keys: ['table_name', 'table_pattern'] },
    { label: 'Column', keys: ['column_name', 'column_pattern'] },
    { label: 'Redis DB', keys: ['redis_db_index'] },
    { label: 'Actor ID', keys: ['actor_id'] },
    { label: 'Submitter', keys: ['submitter_name', 'submitter_id'] },
    { label: 'Requester', keys: ['requester_name', 'requester_id'] },
    { label: 'Approver', keys: ['approver_name', 'approver_id'] },
    { label: 'Reviewer', keys: ['reviewer_name', 'reviewer_id'] },
    { label: 'Executor', keys: ['executor_name', 'executor_id'] },
    { label: 'Target User', keys: ['target_username', 'target_user_id', 'user_id', 'username'] },
    { label: 'Auth Group', keys: ['auth_group', 'auth_group_name', 'group_key', 'group_name'] },
    { label: 'Permission', keys: ['permission', 'permission_key'] },
    { label: 'Subject', keys: ['subject_name', 'subject_type', 'subject_id'], format: formatSubject },
    { label: 'Recipients', keys: ['delivered_recipients_names', 'delivered_recipients_ids', 'intended_recipients_names', 'intended_recipients_ids'], format: formatRecipients },
    { label: 'Workflow Rule', keys: ['workflow_rule_name', 'workflow_rule_id'] },
    { label: 'Execution Mode', keys: ['execution_mode'] },
    { label: 'Status', keys: ['status', 'lark_status'] },
    { label: 'Reason', keys: ['reason', 'export_reason', 'error_code', 'error_message'] },
    { label: 'Error', keys: ['error', 'err', 'lark_error'] },
    { label: 'Sensitive', keys: ['contains_sensitive'] },
    { label: 'Rows', keys: ['rows', 'row_count', 'rows_affected'] },
    { label: 'Duration', keys: ['duration_ms'], format: formatDuration },
    { label: 'Attempts', keys: ['attempts', 'lark_attempts'] },
    { label: 'Channel', keys: ['channel', 'notification_channel'] },
    { label: 'SQL', keys: ['sql', 'sql_content', 'sql_stmt'] },
  ]

  return definitions
    .map((definition) => {
      const value = definition.format ? definition.format(details) : firstStringDetail(details, definition.keys)
      if (value) {
        definition.keys.forEach((key) => usedKeys.add(key))
      }
      return { label: definition.label, value }
    })
    .filter((item) => item.value)
}

function formatSubject(details: Record<string, unknown>) {
  const name = stringDetail(details, 'subject_name')
  const type = stringDetail(details, 'subject_type')
  const id = stringDetail(details, 'subject_id')
  if (name && type) {
    return `${name} (${type})`
  }
  return name || id
}

function formatExport(details: Record<string, unknown>) {
  const id = stringDetail(details, 'export_id')
  if (!id) {
    return ''
  }
  const status = stringDetail(details, 'status')
  return status ? `Export #${id} (${status})` : `Export #${id}`
}

function formatRecipients(details: Record<string, unknown>) {
  const names = stringArrayDetail(details, 'delivered_recipients_names')
  if (names.length > 0) {
    return names.join(', ')
  }
  const ids = stringArrayDetail(details, 'delivered_recipients_ids')
  if (ids.length > 0) {
    return ids.join(', ')
  }
  return ''
}

function formatDuration(details: Record<string, unknown>) {
  const value = details.duration_ms
  if (typeof value === 'number') {
    return `${value} ms`
  }
  if (typeof value === 'string' && value.trim()) {
    return `${value.trim()} ms`
  }
  return ''
}

function firstStringDetail(details: Record<string, unknown>, keys: string[]) {
  for (const key of keys) {
    const value = stringDetail(details, key)
    if (value) {
      return value
    }
  }
  return ''
}

function stringDetail(details: Record<string, unknown> | null, key: string) {
  if (!details) {
    return ''
  }
  const value = details[key]
  if (value === null || value === undefined) {
    return ''
  }
  if (typeof value === 'string') {
    return value.trim()
  }
  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value)
  }
  return ''
}

function stringArrayDetail(details: Record<string, unknown>, key: string) {
  const value = details[key]
  if (!Array.isArray(value)) {
    return []
  }
  return value.map((item) => String(item)).filter(Boolean)
}

function buildGenericDetailSummary(details: Record<string, unknown>, usedKeys: Set<string>) {
  return Object.entries(details)
    .filter(([key, value]) => !usedKeys.has(key) && value !== null && value !== undefined && value !== '')
    .slice(0, 24)
    .map(([key, value]) => ({
      label: formatDetailKey(key),
      value: formatDetailValue(value),
    }))
}

function formatDetailKey(key: string) {
  return key
    .split('_')
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}

function formatDetailValue(value: unknown): string {
  if (Array.isArray(value)) {
    return value.map((item) => formatDetailValue(item)).join(', ')
  }
  if (isRecord(value)) {
    return JSON.stringify(value, null, 2)
  }
  return String(value)
}

function formatUnknownDetail(value: unknown) {
  if (value === null || value === undefined || value === '') {
    return '—'
  }
  if (typeof value === 'string') {
    return value
  }
  return JSON.stringify(value, null, 2)
}

function toRFC3339(value: string) {
  if (!value) {
    return ''
  }
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '' : date.toISOString()
}

function getDownloadFilename(contentDisposition: string | null) {
  if (!contentDisposition) {
    return null
  }
  const matched = contentDisposition.match(/filename="([^"]+)"/)
  return matched?.[1] ?? null
}

const MONTH_NAMES = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'] as const
const WEEKDAY_NAMES = ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa'] as const

const FROM_DATE_PRESETS = [
  { label: 'Today', getValue: () => toLocalInputValue(startOfDay(new Date())) },
  { label: 'Yesterday', getValue: () => toLocalInputValue(startOfDay(addDays(new Date(), -1))) },
  { label: 'Last 7 Days', getValue: () => toLocalInputValue(startOfDay(addDays(new Date(), -6))) },
  { label: 'This Month', getValue: () => toLocalInputValue(startOfMonth(new Date())) },
  { label: 'Clear', getValue: () => '' },
] as const

const TO_DATE_PRESETS = [
  { label: 'Now', getValue: () => toLocalInputValue(new Date()) },
  { label: 'End of Today', getValue: () => toLocalInputValue(endOfDay(new Date())) },
  { label: 'End of Yesterday', getValue: () => toLocalInputValue(endOfDay(addDays(new Date(), -1))) },
  { label: 'End of This Month', getValue: () => toLocalInputValue(endOfMonth(new Date())) },
  { label: 'Clear', getValue: () => '' },
] as const

function DateTimeField({
  value,
  onChange,
  placeholder,
  presets,
}: {
  value: string
  onChange: (value: string) => void
  placeholder: string
  presets: ReadonlyArray<{ label: string; getValue: () => string }>
}) {
  const [open, setOpen] = useState(false)
  const [viewDate, setViewDate] = useState(() => parseLocalDateTime(value) ?? new Date())
  const rootRef = useRef<HTMLDivElement | null>(null)
  const selectedDate = parseLocalDateTime(value)
  const valueTime = selectedDate ? formatTimePart(selectedDate) : ''

  useEffect(() => {
    if (!open) {
      setViewDate(parseLocalDateTime(value) ?? new Date())
    }
  }, [open, value])

  useEffect(() => {
    if (!open) {
      return
    }
    function handlePointerDown(event: MouseEvent) {
      if (rootRef.current?.contains(event.target as Node)) {
        return
      }
      setOpen(false)
    }

    function handleEscape(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        setOpen(false)
      }
    }

    window.addEventListener('mousedown', handlePointerDown)
    window.addEventListener('keydown', handleEscape)
    return () => {
      window.removeEventListener('mousedown', handlePointerDown)
      window.removeEventListener('keydown', handleEscape)
    }
  }, [open])

  const monthLabel = `${MONTH_NAMES[viewDate.getMonth()]} ${viewDate.getFullYear()}`
  const cells = buildCalendarCells(viewDate)

  function applyDate(nextDate: Date) {
    const base = selectedDate ?? nextDate
    const merged = new Date(nextDate)
    merged.setHours(base.getHours(), base.getMinutes(), 0, 0)
    onChange(toLocalInputValue(merged))
    setViewDate(merged)
  }

  function applyTime(nextTime: string) {
    const base = selectedDate ?? viewDate
    const [hoursText, minutesText] = nextTime.split(':')
    const hours = Number(hoursText)
    const minutes = Number(minutesText)
    if (Number.isNaN(hours) || Number.isNaN(minutes)) {
      return
    }
    const merged = new Date(base)
    merged.setHours(hours, minutes, 0, 0)
    onChange(toLocalInputValue(merged))
  }

  function clearValue() {
    onChange('')
    setOpen(false)
  }

  return (
    <div ref={rootRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen((current) => !current)}
        aria-label={placeholder}
        className="inline-flex h-9 w-full items-center justify-between gap-2 rounded-lg border border-border bg-panel-soft px-3 text-left text-[13px] text-ink outline-none transition hover:bg-white focus:border-accent focus:ring-2 focus:ring-accent/20"
      >
        <span className={value ? 'text-ink' : 'text-muted'}>{value ? formatDateTimeSummary(value) : placeholder}</span>
        <CalendarDays className="h-4 w-4 text-muted" />
      </button>

      {open ? (
        <div className="absolute left-0 top-[calc(100%+8px)] z-30 w-[320px] rounded-xl border border-border bg-white p-3 shadow-[0_18px_50px_rgba(15,23,42,0.15)]">
          <div className="mb-3 flex items-center justify-between">
            <button
              type="button"
              onClick={() => setViewDate((current) => addMonths(current, -1))}
              className="inline-flex h-8 w-8 items-center justify-center rounded-md border border-border bg-panel-soft text-muted transition hover:bg-page hover:text-ink"
              aria-label="Previous month"
            >
              <ChevronLeft className="h-4 w-4" />
            </button>
            <p className="text-[13px] font-semibold text-ink">{monthLabel}</p>
            <button
              type="button"
              onClick={() => setViewDate((current) => addMonths(current, 1))}
              className="inline-flex h-8 w-8 items-center justify-center rounded-md border border-border bg-panel-soft text-muted transition hover:bg-page hover:text-ink"
              aria-label="Next month"
            >
              <ChevronRight className="h-4 w-4" />
            </button>
          </div>

          <div className="grid grid-cols-7 gap-1 text-center text-[11px] font-semibold text-faint">
            {WEEKDAY_NAMES.map((name) => (
              <span key={name} className="py-1">{name}</span>
            ))}
          </div>

          <div className="mt-1 grid grid-cols-7 gap-1">
            {cells.map((cell) => {
              const isSelected = selectedDate != null && isSameDay(cell.date, selectedDate)
              const isCurrentMonth = cell.date.getMonth() === viewDate.getMonth()
              return (
                <button
                  key={cell.key}
                  type="button"
                  onClick={() => applyDate(cell.date)}
                  className={`h-9 rounded-md text-[12px] transition ${
                    isSelected
                      ? 'bg-brand font-semibold text-white'
                      : isCurrentMonth
                        ? 'text-ink hover:bg-page'
                        : 'text-faint hover:bg-page'
                  }`}
                >
                  {cell.date.getDate()}
                </button>
              )
            })}
          </div>

          <div className="mt-3 flex items-center gap-2">
            <input
              type="time"
              value={valueTime}
              onChange={(event) => applyTime(event.target.value)}
              className="h-9 flex-1 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
            />
            {value ? (
              <button
                type="button"
                onClick={clearValue}
                className="inline-flex h-9 items-center justify-center rounded-lg border border-border bg-panel-soft px-3 text-[12px] font-semibold text-muted transition hover:bg-page hover:text-ink"
              >
                Clear
              </button>
            ) : null}
          </div>

          <div className="mt-3 flex flex-wrap gap-2">
            {presets.map((preset) => (
              <button
                key={preset.label}
                type="button"
                onClick={() => {
                  onChange(preset.getValue())
                  setOpen(false)
                }}
                className="inline-flex h-8 items-center justify-center rounded-full border border-border bg-panel-soft px-3 text-[11px] font-semibold text-muted transition hover:bg-page hover:text-ink"
              >
                {preset.label}
              </button>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  )
}

function formatAuditDateTime(value?: string | null, withSeconds = false) {
  if (!value) {
    return '—'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return new Intl.DateTimeFormat('en-CA', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: withSeconds ? '2-digit' : undefined,
    hour12: false,
    timeZone: getBrowserTimeZone(),
  }).format(date).replace(',', '')
}

function parseLocalDateTime(value: string) {
  if (!value) {
    return null
  }
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
}

function toLocalInputValue(date: Date) {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  const hour = String(date.getHours()).padStart(2, '0')
  const minute = String(date.getMinutes()).padStart(2, '0')
  return `${year}-${month}-${day}T${hour}:${minute}`
}

function addDays(date: Date, amount: number) {
  const next = new Date(date)
  next.setDate(next.getDate() + amount)
  return next
}

function buildCalendarCells(viewDate: Date) {
  const start = startOfWeek(startOfMonth(viewDate))
  return Array.from({ length: 42 }, (_, index) => {
    const date = addDays(start, index)
    return {
      key: `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}`,
      date,
    }
  })
}

function startOfWeek(date: Date) {
  const value = new Date(date)
  value.setHours(0, 0, 0, 0)
  value.setDate(value.getDate() - value.getDay())
  return value
}

function addMonths(date: Date, amount: number) {
  return new Date(date.getFullYear(), date.getMonth() + amount, 1, date.getHours(), date.getMinutes(), 0, 0)
}

function startOfDay(date: Date) {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate(), 0, 0, 0, 0)
}

function endOfDay(date: Date) {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate(), 23, 30, 0, 0)
}

function startOfMonth(date: Date) {
  return new Date(date.getFullYear(), date.getMonth(), 1, 0, 0, 0, 0)
}

function endOfMonth(date: Date) {
  return new Date(date.getFullYear(), date.getMonth() + 1, 0, 23, 30, 0, 0)
}

function isSameDay(left: Date, right: Date) {
  return left.getFullYear() === right.getFullYear()
    && left.getMonth() === right.getMonth()
    && left.getDate() === right.getDate()
}

function formatTimePart(date: Date) {
  const hours = String(date.getHours()).padStart(2, '0')
  const minutes = String(date.getMinutes()).padStart(2, '0')
  return `${hours}:${minutes}`
}

function formatDateTimeSummary(value: string) {
  const date = parseLocalDateTime(value)
  if (!date) {
    return value
  }
  return formatAuditDateTime(date.toISOString())
}
