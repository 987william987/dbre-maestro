import { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { CalendarDays, ChevronLeft, ChevronRight, Download, Search, X } from 'lucide-react'
import { ApiError } from '@/shared/api/client'
import { useAuth } from '@/shared/auth/AuthContext'
import type { AuditLog } from '@/shared/types/audit'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { PageIntro } from '@/shared/ui/PageIntro'
import { exportAuditLogs, listAuditLogs } from '@/modules/audit/api'
import { useToast } from '@/shared/ui/ToastContext'

const PAGE_SIZE = 20

const ACTION_OPTIONS = [
  { value: '', label: 'All Actions' },
  { value: 'login', label: 'Login' },
  { value: 'logout', label: 'Logout' },
  { value: 'setting_change', label: 'Setting Change' },
  { value: 'user_create', label: 'Create User' },
  { value: 'user_update', label: 'Update User' },
  { value: 'user_delete', label: 'Delete User' },
  { value: 'user_membership_add', label: 'Add Group Membership' },
  { value: 'user_membership_remove', label: 'Remove Group Membership' },
  { value: 'user_permission_add', label: 'Add User Permission' },
  { value: 'ticket_submit', label: 'Submit Ticket' },
  { value: 'ticket_approve', label: 'Approve Ticket' },
  { value: 'ticket_reject', label: 'Reject Ticket' },
  { value: 'ticket_request_execution', label: 'Request Execution' },
  { value: 'ticket_execute_start', label: 'Start Execution' },
  { value: 'ticket_execute_complete', label: 'Execution Complete' },
  { value: 'ticket_execute_failed', label: 'Execution Failed' },
  { value: 'ticket_schedule', label: 'Schedule Execution' },
  { value: 'ticket_stop', label: 'Stop Ticket' },
  { value: 'query_execute', label: 'Execute Query' },
  { value: 'export_create', label: 'Create Export' },
  { value: 'export_approve', label: 'Approve Export' },
  { value: 'export_reject', label: 'Reject Export' },
  { value: 'export_download', label: 'Download Export' },
  { value: 'audit_export', label: 'Export Audit Log' },
  { value: 'notification_failure', label: 'Notification Failure' },
] as const

const RESOURCE_OPTIONS = [
  { value: '', label: 'All Resources' },
  { value: 'db_connection', label: 'DB Connection' },
  { value: 'ticket', label: 'Ticket' },
  { value: 'user', label: 'User' },
  { value: 'export', label: 'Export' },
  { value: 'audit_log', label: 'Audit Log' },
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

  async function loadLogs(nextOffset: number) {
    setLoading(true)
    setError('')
    try {
      const response = await listAuditLogs({
        actionType: filters.actionType,
        actorName: filters.actorKeyword.trim(),
        resourceType: filters.resourceType,
        resourceName: filters.resourceKeyword.trim(),
        from: toRFC3339(filters.from),
        to: toRFC3339(filters.to),
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
    void loadLogs(offset)
  }, [offset])

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setOffset(0)
    await loadLogs(0)
  }

  const filterHint = useMemo(() => {
    const active: string[] = []
    if (filters.actionType) active.push(`Action: ${formatActionType(filters.actionType)}`)
    if (filters.resourceType) active.push(`Resource: ${formatResourceType(filters.resourceType)}`)
    if (filters.actorKeyword) active.push(`Actor: ${filters.actorKeyword}`)
    if (filters.resourceKeyword) active.push(`Resource Name: ${filters.resourceKeyword}`)
    if (filters.from || filters.to) active.push(`Time Range: ${formatDateFilter(filters.from) || 'Any'} to ${formatDateFilter(filters.to) || 'Any'}`)
    return active
  }, [filters])

  async function handleExport() {
    try {
      const response = await exportAuditLogs({
        actionType: filters.actionType,
        actorName: filters.actorKeyword.trim(),
        resourceType: filters.resourceType,
        resourceName: filters.resourceKeyword.trim(),
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
      <PageIntro
        title="Audit Logs"
        description="View operation records for logins, tickets, exports, and configuration changes. Common filters are provided as selects; complex details are shown in the detail panel."
      />

      <section>
        <form className="py-1" onSubmit={handleSubmit}>
          <div className="flex flex-wrap items-end gap-2.5">
            <FilterHint
              hint="Select a common action event, e.g. login, logout, setting change."
              className="min-w-[180px] flex-1 xl:max-w-[210px]"
            >
              <SelectField
                value={filters.actionType}
                onChange={(value) => setFilters((current) => ({ ...current, actionType: value }))}
                options={ACTION_OPTIONS}
              />
            </FilterHint>
            <FilterHint
              hint="Narrow by resource type, e.g. DB connection, ticket, or user."
              className="min-w-[180px] flex-1 xl:max-w-[210px]"
            >
              <SelectField
                value={filters.resourceType}
                onChange={(value) => setFilters((current) => ({ ...current, resourceType: value }))}
                options={RESOURCE_OPTIONS}
              />
            </FilterHint>
            <FilterHint
              hint="Filter by actor name keyword, e.g. admin or william."
              className="min-w-[160px] flex-1 xl:max-w-[180px]"
            >
              <input
                value={filters.actorKeyword}
                onChange={(event) => setFilters((current) => ({ ...current, actorKeyword: event.target.value }))}
                className="h-10 w-full rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                placeholder="Actor"
              />
            </FilterHint>
            <FilterHint
              hint="Filter by resource name keyword, e.g. a connection name or ticket title."
              className="min-w-[170px] flex-1 xl:max-w-[190px]"
            >
              <input
                value={filters.resourceKeyword}
                onChange={(event) => setFilters((current) => ({ ...current, resourceKeyword: event.target.value }))}
                className="h-10 w-full rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                placeholder="Resource"
              />
            </FilterHint>
            <FilterHint
              hint="Set the start date and time. Only events after this point will be shown."
              className="min-w-[210px] flex-1 xl:max-w-[250px]"
            >
              <DateTimeField
                value={filters.from}
                onChange={(value) => setFilters((current) => ({ ...current, from: value }))}
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
                onChange={(value) => setFilters((current) => ({ ...current, to: value }))}
                placeholder="End date and time"
                presets={TO_DATE_PRESETS}
              />
            </FilterHint>

            {canExport ? (
              <button
                type="button"
                onClick={handleExport}
                className="inline-flex h-10 shrink-0 items-center justify-center gap-2 rounded-lg border border-border bg-white px-4 text-[13px] font-bold text-ink transition hover:bg-page xl:ml-auto"
              >
                <Download className="h-4 w-4" />
                Export
              </button>
            ) : null}
            <button
              type="submit"
              className="inline-flex h-10 shrink-0 items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white transition hover:bg-slate-800"
            >
              <Search className="h-4 w-4" />
              Apply
            </button>
          </div>

          <div className="mt-2 flex flex-wrap items-center gap-2 text-[12px] text-muted">
            {filterHint.length > 0 ? <span>Active filters: {filterHint.join(', ')}</span> : null}
          </div>
        </form>
      </section>

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      <section className="overflow-hidden rounded-xl border border-border bg-panel shadow-soft">
        {loading ? (
          <LoadingBlock message="Loading audit logs..." className="h-60 rounded-none border-0" />
        ) : logs.length === 0 ? (
          <div className="flex h-60 items-center justify-center text-sm text-muted">No matching audit logs.</div>
        ) : (
          <div className="overflow-x-auto">
            <table className="min-w-full border-collapse">
              <thead className="bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                <tr>
                  <th className="px-3 py-3">Created</th>
                  <th className="px-3 py-3">Actor</th>
                  <th className="px-3 py-3">Action</th>
                  <th className="px-3 py-3">Resource</th>
                  <th className="px-3 py-3">IP</th>
                  <th className="px-3 py-3">Details</th>
                </tr>
              </thead>
              <tbody>
                {logs.map((log) => (
                  <tr key={log.id} className="border-t border-border text-sm text-ink transition-colors hover:bg-slate-50/70">
                    <td className="px-3 py-2.5 text-[12px] text-muted whitespace-nowrap">{formatAuditDateTime(log.created_at, true)}</td>
                    <td className="px-3 py-2.5 text-[13px] font-semibold whitespace-nowrap">{formatActor(log)}</td>
                    <td className="px-3 py-2.5 text-[12px] whitespace-nowrap">{formatActionType(log.action_type)}</td>
                    <td className="max-w-[360px] px-3 py-2.5 text-[12px] text-muted">
                      <div className="truncate" title={formatResource(log)}>
                        {formatResource(log)}
                      </div>
                    </td>
                    <td className="px-3 py-2.5 text-[12px] text-muted whitespace-nowrap">{formatIPAddress(log.ip_address)}</td>
                    <td className="px-3 py-2.5">
                      <button
                        type="button"
                        onClick={() => setSelectedLog(log)}
                        className="inline-flex h-8 items-center justify-center rounded-md border border-border bg-panel-soft px-3 text-[12px] font-semibold text-ink transition hover:bg-page"
                      >
                        View
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

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
                  <p className="text-[13px] font-semibold text-ink">Full Details</p>
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
      <p className="mt-1 text-[13px] text-ink">{value || '—'}</p>
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

function formatResource(log: AuditLog) {
  const label = formatResourceType(log.resource_type)
  const detailsRecord = isRecord(log.details) ? log.details : null
  const detailName = typeof detailsRecord?.name === 'string' && detailsRecord.name.trim() ? detailsRecord.name.trim() : ''

  if (detailName) {
    return `${label} · ${detailName}`
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

function toRFC3339(value: string) {
  if (!value) {
    return ''
  }
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '' : date.toISOString()
}

function formatDateFilter(value: string) {
  if (!value) {
    return ''
  }
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : formatAuditDateTime(date.toISOString(), true)
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
  const containerRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    if (!open) {
      setViewDate(parseLocalDateTime(value) ?? new Date())
    }
  }, [open, value])

  useEffect(() => {
    function handlePointerDown(event: MouseEvent) {
      const target = event.target as Node
      if (!containerRef.current?.contains(target)) {
        setOpen(false)
      }
    }

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        setOpen(false)
      }
    }

    document.addEventListener('mousedown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('mousedown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [])

  const parsed = parseLocalDateTime(value)
  const displayValue = parsed ? formatAuditLocalDateTime(parsed) : placeholder

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen((current) => !current)}
        className="flex h-10 w-full items-center justify-between rounded-lg border border-border bg-panel-soft px-3 text-left text-[13px] text-ink transition hover:border-slate-300"
      >
        <span className={parsed ? 'text-ink' : 'text-muted'}>{displayValue}</span>
        <CalendarDays className="h-4 w-4 text-faint" />
      </button>

      {open ? (
        <div className="absolute left-0 top-[calc(100%+8px)] z-30 w-[min(720px,calc(100vw-2rem))] overflow-hidden rounded-xl border border-border bg-white shadow-[0_22px_45px_rgba(15,23,42,0.14)]">
          <div className="grid gap-0 md:grid-cols-[180px_minmax(0,1fr)]">
            <div className="border-b border-border p-3 md:border-b-0 md:border-r">
              <p className="mb-3 text-[12px] font-semibold text-muted">Quick Range</p>
              <div className="grid gap-1">
                {presets.map((preset) => (
                  <button
                    key={preset.label}
                    type="button"
                    onClick={() => {
                      onChange(preset.getValue())
                      setOpen(false)
                    }}
                    className="rounded-md px-3 py-2 text-left text-[12px] font-medium text-ink transition hover:bg-panel-soft"
                  >
                    {preset.label}
                  </button>
                ))}
              </div>
            </div>

            <div className="p-3">
              <div className="mb-3 flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => setViewDate((current) => addMonths(current, -1))}
                  className="inline-flex h-9 w-9 items-center justify-center rounded-md border border-border bg-white text-ink transition hover:bg-panel-soft"
                  aria-label="Previous month"
                >
                  <ChevronLeft className="h-4 w-4" />
                </button>
                <div className="flex-1 text-center text-[18px] font-semibold text-ink">
                  {MONTH_NAMES[viewDate.getMonth()]} {viewDate.getFullYear()}
                </div>
                <button
                  type="button"
                  onClick={() => setViewDate((current) => addMonths(current, 1))}
                  className="inline-flex h-9 w-9 items-center justify-center rounded-md border border-border bg-white text-ink transition hover:bg-panel-soft"
                  aria-label="Next month"
                >
                  <ChevronRight className="h-4 w-4" />
                </button>
              </div>

              <div className="grid grid-cols-7 gap-1 text-center text-[11px] font-semibold uppercase tracking-[0.08em] text-muted">
                {WEEKDAY_NAMES.map((day) => <span key={day}>{day}</span>)}
              </div>
              <div className="mt-2 grid grid-cols-7 gap-1">
                {buildCalendarDays(viewDate).map((day) => {
                  const selected = parsed && isSameDay(parsed, day.date)
                  return (
                    <button
                      key={day.key}
                      type="button"
                      disabled={!day.inCurrentMonth}
                      onClick={() => {
                        const next = mergeDatePart(value, day.date)
                        onChange(next)
                        setOpen(false)
                      }}
                      className={`h-10 rounded-md text-[13px] transition ${
                        selected
                          ? 'bg-ink font-semibold text-white'
                          : day.inCurrentMonth
                            ? 'text-ink hover:bg-panel-soft'
                            : 'text-faint'
                      }`}
                    >
                      {day.date.getDate()}
                    </button>
                  )
                })}
              </div>

              <div className="mt-3 flex items-center gap-2">
                <TimeSelect
                  value={getTimePart(value)}
                  onChange={(timeValue) => onChange(updateTimePart(value, timeValue, viewDate))}
                />
                <button
                  type="button"
                  onClick={() => {
                    onChange('')
                    setOpen(false)
                  }}
                  className="inline-flex h-9 items-center justify-center rounded-md border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-panel-soft"
                >
                  Clear
                </button>
              </div>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  )
}

function TimeSelect({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  return (
    <DropdownSelect
      ariaLabel="Select time"
      value={value}
      onChange={onChange}
      options={TIME_OPTIONS}
      className="min-w-[160px]"
      triggerClassName="bg-white"
      menuClassName="max-h-64 overflow-y-auto"
    />
  )
}

const TIME_OPTIONS = Array.from({ length: 48 }, (_, index) => {
  const hour = String(Math.floor(index / 2)).padStart(2, '0')
  const minute = index % 2 === 0 ? '00' : '30'
  return { value: `${hour}:${minute}`, label: `${hour}:${minute}` }
})

function buildCalendarDays(viewDate: Date) {
  const firstDay = new Date(viewDate.getFullYear(), viewDate.getMonth(), 1)
  const start = addDays(firstDay, -firstDay.getDay())
  return Array.from({ length: 42 }, (_, index) => {
    const date = addDays(start, index)
    return {
      key: `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}`,
      date,
      inCurrentMonth: date.getMonth() === viewDate.getMonth(),
    }
  })
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
  }).format(date).replace(',', '')
}

function formatAuditLocalDateTime(date: Date) {
  return `${MONTH_NAMES[date.getMonth()]} ${String(date.getDate()).padStart(2, '0')}, ${date.getFullYear()} ${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`
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

function mergeDatePart(existingValue: string, nextDate: Date) {
  const base = parseLocalDateTime(existingValue) ?? new Date()
  const merged = new Date(nextDate.getFullYear(), nextDate.getMonth(), nextDate.getDate(), base.getHours(), base.getMinutes(), 0, 0)
  return toLocalInputValue(merged)
}

function updateTimePart(existingValue: string, timeValue: string, fallbackDate: Date) {
  const [hours, minutes] = timeValue.split(':').map((part) => Number.parseInt(part, 10))
  const base = parseLocalDateTime(existingValue) ?? fallbackDate
  const merged = new Date(base.getFullYear(), base.getMonth(), base.getDate(), hours, minutes, 0, 0)
  return toLocalInputValue(merged)
}

function getTimePart(value: string) {
  const parsed = parseLocalDateTime(value)
  if (!parsed) {
    return '00:00'
  }
  return `${String(parsed.getHours()).padStart(2, '0')}:${String(parsed.getMinutes()).padStart(2, '0')}`
}

function addDays(date: Date, amount: number) {
  const next = new Date(date)
  next.setDate(next.getDate() + amount)
  return next
}

function addMonths(date: Date, amount: number) {
  return new Date(date.getFullYear(), date.getMonth() + amount, 1)
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
