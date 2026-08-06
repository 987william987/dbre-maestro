import { useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { AlertTriangle, ArrowRight, CheckCircle2, Clock3, Database, FilePlus2, KeyRound, ShieldCheck, TicketIcon, XCircle } from 'lucide-react'
import { Link } from 'react-router-dom'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import type { Ticket, TicketType } from '@/shared/types/ticket'
import { DataTable, DataTableBody, DataTableCell, DataTableHead, DataTableHeaderCell, DataTableRow } from '@/shared/ui/DataTable'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { StatusBadge } from '@/shared/ui/StatusBadge'
import { getDashboard, type DashboardCount, type DashboardQueryAccessScope, type DashboardResponse, type DashboardTicketSummary, type DashboardUserCount } from '@/modules/dashboard/api'

const TICKET_TYPE_LABELS: Record<TicketType, string> = {
  ddl: 'DDL',
  dml: 'DML',
  redis_command: 'Redis',
  query_access: 'Query Access',
  sql_export: 'SQL Export',
  sensitive_query_access: 'Sensitive Access',
}

const ACTIVE_TICKET_STATUSES = new Set(['pending_review', 'pending_execution', 'executing', 'needs_admin_attention'])

function formatCount(value: number) {
  return new Intl.NumberFormat('en-US').format(value)
}

function formatTicketType(type: string) {
  return TICKET_TYPE_LABELS[type as TicketType] ?? type
}

function formatRemaining(scope: DashboardQueryAccessScope) {
  if (!scope.expires_at) {
    return 'Never'
  }
  if (scope.remaining_days == null) {
    return '—'
  }
  return scope.remaining_days === 0 ? 'Today' : `${scope.remaining_days}d`
}

function formatMinutes(value?: number | null) {
  if (value == null) {
    return '—'
  }
  if (value < 60) {
    return `${Math.round(value)}m`
  }
  if (value < 24 * 60) {
    return `${(value / 60).toFixed(1)}h`
  }
  return `${(value / 1440).toFixed(1)}d`
}

function formatDurationMs(value?: number | null) {
  if (value == null) {
    return '—'
  }
  if (value < 1000) {
    return `${Math.round(value)}ms`
  }
  if (value < 60_000) {
    return `${(value / 1000).toFixed(1)}s`
  }
  return `${(value / 60_000).toFixed(1)}m`
}

function KpiCard({
  title,
  value,
  helper,
  icon: Icon,
  tone = 'neutral',
}: {
  title: string
  value: number
  helper: string
  icon: typeof TicketIcon
  tone?: 'neutral' | 'success' | 'warning' | 'danger'
}) {
  const toneClass = {
    neutral: 'bg-slate-100 text-slate-700',
    success: 'bg-emerald-50 text-emerald-700',
    warning: 'bg-amber-50 text-amber-700',
    danger: 'bg-rose-50 text-rose-700',
  }[tone]
  return (
    <section className="rounded-xl border border-border bg-panel p-4 shadow-soft">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-[12px] font-semibold text-muted">{title}</p>
          <p className="mt-2 text-[28px] font-semibold tracking-normal text-ink">{formatCount(value)}</p>
          <p className="mt-1 text-[12px] text-muted">{helper}</p>
        </div>
        <span className={`inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-lg ${toneClass}`}>
          <Icon className="h-5 w-5" />
        </span>
      </div>
    </section>
  )
}

function MetricGrid({ items }: { items: Array<{ label: string; value: string | number; tone?: 'neutral' | 'warning' | 'danger' | 'success' }> }) {
  const toneClass = {
    neutral: 'bg-panel-soft text-ink',
    warning: 'bg-amber-50 text-amber-800',
    danger: 'bg-rose-50 text-rose-800',
    success: 'bg-emerald-50 text-emerald-800',
  }
  return (
    <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
      {items.map((item) => (
        <div key={item.label} className={`rounded-lg border border-border px-3 py-2 ${toneClass[item.tone ?? 'neutral']}`}>
          <p className="text-[11px] font-medium text-muted">{item.label}</p>
          <p className="mt-1 text-[20px] font-semibold tracking-normal">{typeof item.value === 'number' ? formatCount(item.value) : item.value}</p>
        </div>
      ))}
    </div>
  )
}

function DashboardPanel({
  title,
  action,
  children,
}: {
  title: string
  action?: ReactNode
  children: ReactNode
}) {
  return (
    <section className="rounded-xl border border-border bg-panel shadow-soft">
      <div className="flex min-h-12 items-center justify-between gap-3 border-b border-border px-4 py-3">
        <h2 className="text-[13px] font-semibold text-ink">{title}</h2>
        {action}
      </div>
      <div className="p-4">{children}</div>
    </section>
  )
}

function DistributionBar({ item, total }: { item: DashboardCount; total: number }) {
  const pct = total > 0 ? Math.max(4, Math.round((item.count / total) * 100)) : 0
  return (
    <div className="grid gap-1">
      <div className="flex items-center justify-between gap-3 text-[12px]">
        <span className="truncate font-medium text-ink">{formatTicketType(item.key)}</span>
        <span className="text-muted">{formatCount(item.count)}</span>
      </div>
      <div className="h-2 overflow-hidden rounded-full bg-slate-100">
        <div className="h-full rounded-full bg-brand" style={{ width: `${pct}%` }} />
      </div>
    </div>
  )
}

function CountList({ items, empty = 'No data.' }: { items: DashboardCount[]; empty?: string }) {
  const total = items.reduce((sum, item) => sum + item.count, 0)
  if (items.length === 0) {
    return <p className="text-[12px] text-muted">{empty}</p>
  }
  return (
    <div className="grid gap-3">
      {items.map((item) => <DistributionBar key={item.key} item={item} total={total} />)}
    </div>
  )
}

function UserCountList({ items, empty = 'No data.' }: { items: DashboardUserCount[]; empty?: string }) {
  if (items.length === 0) {
    return <p className="text-[12px] text-muted">{empty}</p>
  }
  return (
    <div className="grid gap-2">
      {items.map((item, index) => (
        <div key={`${item.user_id ?? item.username ?? index}`} className="flex items-center justify-between gap-3 rounded-lg border border-border bg-panel-soft px-3 py-2 text-[12px]">
          <span className="truncate font-medium text-ink">{item.username || (item.user_id ? `User #${item.user_id}` : 'Unknown')}</span>
          <span className="text-muted">{formatCount(item.count)}</span>
        </div>
      ))}
    </div>
  )
}

function TicketTable({ tickets, empty }: { tickets: Ticket[]; empty: string }) {
  if (tickets.length === 0) {
    return <div className="rounded-lg border border-dashed border-border bg-panel-soft p-5 text-center text-[12px] text-muted">{empty}</div>
  }
  return (
    <div className="overflow-x-auto rounded-lg border border-border">
      <DataTable>
        <DataTableHead>
          <tr>
            <DataTableHeaderCell>Ticket</DataTableHeaderCell>
            <DataTableHeaderCell>Type</DataTableHeaderCell>
            <DataTableHeaderCell>Status</DataTableHeaderCell>
            <DataTableHeaderCell>Updated</DataTableHeaderCell>
          </tr>
        </DataTableHead>
        <DataTableBody>
          {tickets.map((ticket) => (
            <DataTableRow key={ticket.id}>
              <DataTableCell>
                <Link to={`/tickets/${ticket.ticket_no}`} className="grid gap-0.5 hover:text-accent">
                  <span className="font-semibold">{ticket.ticket_no}</span>
                  <span className="max-w-[260px] truncate text-muted">{ticket.title}</span>
                </Link>
              </DataTableCell>
              <DataTableCell>{formatTicketType(ticket.ticket_type)}</DataTableCell>
              <DataTableCell><StatusBadge status={ticket.status} /></DataTableCell>
              <DataTableCell className="whitespace-nowrap text-muted">{formatDateTime(ticket.updated_at)}</DataTableCell>
            </DataTableRow>
          ))}
        </DataTableBody>
      </DataTable>
    </div>
  )
}

function MetadataJobRow({ label, status, updatedAt }: { label: string; status?: string; updatedAt?: string | null }) {
  const normalizedStatus = status || 'idle'
  const statusClass = normalizedStatus === 'failed' ? 'border-rose-200 bg-rose-50 text-rose-700' : normalizedStatus === 'success' ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : 'border-border bg-white text-muted'
  return (
    <div className="flex items-center justify-between gap-3 rounded-lg border border-border bg-panel-soft px-3 py-2 text-[12px]">
      <div className="min-w-0">
        <p className="font-medium text-ink">{label}</p>
        <p className="truncate text-[11px] text-muted">{updatedAt ? formatDateTime(updatedAt) : 'No run recorded'}</p>
      </div>
      <span className={`inline-flex h-6 items-center rounded-full border px-2 text-[11px] font-semibold ${statusClass}`}>{normalizedStatus}</span>
    </div>
  )
}

function isRenewableQueryScope(scope: DashboardQueryAccessScope, renewableConnectionIDs: Set<number>) {
  return scope.effect === 'allow' && renewableConnectionIDs.has(scope.connection_id)
}

function AccessTable({ scopes, renewableConnectionIDs }: { scopes: DashboardQueryAccessScope[]; renewableConnectionIDs: Set<number> }) {
  if (scopes.length === 0) {
    return <div className="rounded-lg border border-dashed border-border bg-panel-soft p-5 text-center text-[12px] text-muted">No active query access scope.</div>
  }
  return (
    <div className="overflow-x-auto rounded-lg border border-border">
      <DataTable>
        <DataTableHead>
          <tr>
            <DataTableHeaderCell>Connection</DataTableHeaderCell>
            <DataTableHeaderCell>Scope</DataTableHeaderCell>
            <DataTableHeaderCell>Effect</DataTableHeaderCell>
            <DataTableHeaderCell>Expires</DataTableHeaderCell>
            <DataTableHeaderCell>Action</DataTableHeaderCell>
          </tr>
        </DataTableHead>
        <DataTableBody>
          {scopes.map((scope) => (
            <DataTableRow key={scope.id} className={scope.expiring_soon ? 'bg-amber-50/40' : undefined}>
              <DataTableCell className="font-medium">{scope.connection_name || `#${scope.connection_id}`}</DataTableCell>
              <DataTableCell>
                <span className="font-mono text-[12px]">{scope.database_pattern}.{scope.table_pattern}</span>
                {scope.source_ticket_no ? <span className="ml-2 text-muted">{scope.source_ticket_no}</span> : null}
              </DataTableCell>
              <DataTableCell>
                <span className={scope.effect === 'deny' ? 'text-danger' : 'text-success'}>{scope.effect}</span>
              </DataTableCell>
              <DataTableCell className="whitespace-nowrap">
                <span className={scope.expiring_soon ? 'font-semibold text-warning' : 'text-muted'}>{formatRemaining(scope)}</span>
              </DataTableCell>
              <DataTableCell>
                {isRenewableQueryScope(scope, renewableConnectionIDs) ? (
                  <Link to={scope.renew_ticket_path} className="inline-flex items-center gap-1 text-[12px] font-semibold text-ink hover:text-accent">
                    Renew
                    <ArrowRight className="h-3.5 w-3.5" />
                  </Link>
                ) : (
                  <span className="text-[12px] text-muted">{scope.effect === 'deny' ? 'Deny rule' : 'Admin managed'}</span>
                )}
              </DataTableCell>
            </DataTableRow>
          ))}
        </DataTableBody>
      </DataTable>
    </div>
  )
}

function TicketTypeDistribution({ summary }: { summary: DashboardTicketSummary }) {
  if (summary.by_type.length === 0) {
    return <p className="text-[12px] text-muted">No ticket data yet.</p>
  }
  return (
    <div className="grid gap-3">
      {summary.by_type.map((item) => <DistributionBar key={item.key} item={item} total={summary.total} />)}
    </div>
  )
}

export function DashboardPage() {
  const [data, setData] = useState<DashboardResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    let alive = true
    async function load() {
      setLoading(true)
      setError('')
      try {
        const next = await getDashboard()
        if (alive) {
          setData(next)
        }
      } catch (loadError) {
        if (alive) {
          setError(loadError instanceof ApiError ? loadError.message : 'Failed to load dashboard.')
        }
      } finally {
        if (alive) {
          setLoading(false)
        }
      }
    }
    void load()
    return () => {
      alive = false
    }
  }, [])

  const expiringAccess = useMemo(
    () => (data?.personal.query_access_scopes ?? []).filter((scope) => scope.expiring_soon),
    [data],
  )
  const pendingTickets = useMemo(
    () => (data?.personal.active_tickets ?? []).filter((ticket) => ACTIVE_TICKET_STATUSES.has(ticket.status)),
    [data],
  )
  const renewableConnectionIDs = useMemo(
    () => new Set((data?.personal.db_scopes ?? []).map((scope) => scope.id)),
    [data],
  )

  if (loading) {
    return <div className="p-3 sm:p-4"><LoadingBlock message="Loading dashboard..." className="min-h-[420px] rounded-xl border-border bg-panel" /></div>
  }

  if (!data) {
    return (
      <div className="p-3 sm:p-4">
        <InlineAlert>{error || 'Dashboard unavailable.'}</InlineAlert>
      </div>
    )
  }

  const personal = data.personal.ticket_summary
  const platform = data.platform

  return (
    <div className="grid gap-4 p-3 sm:p-4">
      {error ? <InlineAlert>{error}</InlineAlert> : null}

      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[20px] font-semibold tracking-normal text-ink">Dashboard</h1>
          <p className="mt-1 text-[12px] text-muted">Ticket workload, access boundary, and platform operations.</p>
        </div>
        <Link to="/tickets/new" className="inline-flex h-9 items-center gap-2 rounded-lg bg-brand px-3 text-[12px] font-semibold text-white transition hover:bg-slate-800">
          <FilePlus2 className="h-4 w-4" />
          New Ticket
        </Link>
      </div>

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        <KpiCard title="My Tickets" value={personal.total} helper="All tickets submitted by you" icon={TicketIcon} />
        <KpiCard title="Active" value={personal.active} helper="Waiting for review, execution, or running" icon={Clock3} tone="warning" />
        <KpiCard title="Completed" value={personal.completed} helper="Finished successfully" icon={CheckCircle2} tone="success" />
        <KpiCard title="Failed / Rejected" value={personal.failed} helper="Needs review or resubmit" icon={XCircle} tone="danger" />
      </div>

      <div className="grid gap-4 xl:grid-cols-[1.1fr_0.9fr]">
        <DashboardPanel title={pendingTickets.length > 0 ? 'My Pending Tickets' : 'My Recent Tickets'} action={<Link to="/tickets" className="text-[12px] font-semibold text-muted hover:text-ink">View all</Link>}>
          <TicketTable tickets={pendingTickets.length > 0 ? pendingTickets : data.personal.recent_tickets} empty="No ticket history yet." />
        </DashboardPanel>

        <DashboardPanel title="My Ticket Types">
          <TicketTypeDistribution summary={personal} />
        </DashboardPanel>
      </div>

      <div className="grid gap-4 xl:grid-cols-[0.9fr_1.1fr]">
        <DashboardPanel title="Submission DB Scope" action={<Link to="/account/access-scopes" className="text-[12px] font-semibold text-muted hover:text-ink">View all</Link>}>
          {data.personal.db_scopes.length === 0 ? (
            <div className="rounded-lg border border-dashed border-border bg-panel-soft p-5 text-center text-[12px] text-muted">No DB connection scope assigned.</div>
          ) : (
            <div className="grid gap-2 sm:grid-cols-2">
              {data.personal.db_scopes.slice(0, 8).map((scope) => (
                <div key={scope.id} className="flex items-center gap-3 rounded-lg border border-border bg-panel-soft px-3 py-2">
                  <Database className="h-4 w-4 text-muted" />
                  <div className="min-w-0">
                    <p className="truncate text-[12px] font-semibold text-ink">{scope.name}</p>
                    <p className="text-[11px] uppercase text-muted">{scope.db_type}</p>
                  </div>
                </div>
              ))}
            </div>
          )}
        </DashboardPanel>

        <DashboardPanel title="Expiring Query Access" action={<Link to="/account/access-scopes" className="text-[12px] font-semibold text-muted hover:text-ink">View all access</Link>}>
          <AccessTable scopes={expiringAccess.length > 0 ? expiringAccess : data.personal.query_access_scopes.slice(0, 5)} renewableConnectionIDs={renewableConnectionIDs} />
        </DashboardPanel>
      </div>

      {platform ? (
        <div className="grid gap-4">
          <div className="flex items-center gap-2 pt-2">
            <ShieldCheck className="h-4 w-4 text-muted" />
            <h2 className="text-[15px] font-semibold text-ink">Platform Operations</h2>
          </div>
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            <KpiCard title="Platform Tickets" value={platform.ticket_summary.total} helper="All visible workflow tickets" icon={TicketIcon} />
            <KpiCard title="Platform Active" value={platform.ticket_summary.active} helper="Queue and executing workload" icon={Clock3} tone="warning" />
            <KpiCard title="Platform Failed" value={platform.ticket_summary.failed} helper="Failed, stopped, or rejected" icon={AlertTriangle} tone="danger" />
            <KpiCard title="DB Health Issues" value={platform.db_connection_failures.length} helper="Connections with failed test status" icon={Database} tone={platform.db_connection_failures.length > 0 ? 'danger' : 'success'} />
          </div>

          <div className="grid gap-4 xl:grid-cols-[1.1fr_0.9fr]">
            <DashboardPanel title="Platform Queue">
              <MetricGrid
                items={[
                  { label: 'Pending Review', value: platform.queue.pending_review, tone: platform.queue.pending_review > 0 ? 'warning' : 'neutral' },
                  { label: 'Pending Execution', value: platform.queue.pending_execution, tone: platform.queue.pending_execution > 0 ? 'warning' : 'neutral' },
                  { label: 'Executing', value: platform.queue.executing, tone: platform.queue.executing > 0 ? 'warning' : 'neutral' },
                  { label: 'Needs Admin Attention', value: platform.queue.needs_admin_attention, tone: platform.queue.needs_admin_attention > 0 ? 'danger' : 'neutral' },
                  { label: 'Failed Today', value: platform.queue.failed_today, tone: platform.queue.failed_today > 0 ? 'danger' : 'neutral' },
                  { label: 'Failed 7d', value: platform.queue.failed_7d, tone: platform.queue.failed_7d > 0 ? 'danger' : 'neutral' },
                  { label: 'Long Pending >24h', value: platform.queue.long_pending, tone: platform.queue.long_pending > 0 ? 'warning' : 'neutral' },
                ]}
              />
            </DashboardPanel>
            <DashboardPanel title="SLA / Aging">
              <MetricGrid
                items={[
                  { label: 'Avg Review Age', value: formatMinutes(platform.aging.avg_review_age_minutes) },
                  { label: 'Max Review Age', value: formatMinutes(platform.aging.max_review_age_minutes) },
                  { label: 'Avg Execution Wait', value: formatMinutes(platform.aging.avg_execution_wait_age_minutes) },
                  { label: 'Max Execution Wait', value: formatMinutes(platform.aging.max_execution_wait_age_minutes) },
                  { label: 'Avg Execution Duration 7d', value: formatDurationMs(platform.aging.avg_execution_duration_ms) },
                ]}
              />
            </DashboardPanel>
          </div>

          <div className="grid gap-4 xl:grid-cols-[1.1fr_0.9fr]">
            <DashboardPanel title="Recent Platform Attention">
              <TicketTable tickets={platform.recent_attention} empty="No active platform attention items." />
            </DashboardPanel>
            <DashboardPanel title="Long Pending Tickets">
              <TicketTable tickets={platform.long_pending_tickets} empty="No tickets pending longer than 24 hours." />
            </DashboardPanel>
          </div>

          <div className="grid gap-4 xl:grid-cols-[1fr_1fr]">
            <DashboardPanel title="Execution Risk / Failure">
              <div className="grid gap-4">
                <MetricGrid
                  items={[
                    { label: 'Recent Failed 7d', value: platform.execution_risk.recent_failed, tone: platform.execution_risk.recent_failed > 0 ? 'danger' : 'neutral' },
                    { label: 'Manual Stop', value: platform.execution_risk.manually_stopped, tone: platform.execution_risk.manually_stopped > 0 ? 'warning' : 'neutral' },
                    { label: 'Service Shutdown', value: platform.execution_risk.service_shutdown, tone: platform.execution_risk.service_shutdown > 0 ? 'danger' : 'neutral' },
                    { label: 'Outcome Unknown', value: platform.execution_risk.outcome_unknown, tone: platform.execution_risk.outcome_unknown > 0 ? 'danger' : 'neutral' },
                    { label: 'Not Sent', value: platform.execution_risk.not_sent },
                    { label: 'DB Explicit Error', value: platform.execution_risk.db_explicit_error, tone: platform.execution_risk.db_explicit_error > 0 ? 'warning' : 'neutral' },
                  ]}
                />
                <CountList items={platform.execution_risk.by_outcome} empty="No failed execution outcomes in the last 7 days." />
              </div>
            </DashboardPanel>
            <DashboardPanel title="Recent Failed Tickets">
              <TicketTable tickets={platform.recent_failed_tickets} empty="No recent failed execution tickets." />
            </DashboardPanel>
          </div>

          <div className="grid gap-4 xl:grid-cols-[1fr_1fr]">
            <DashboardPanel title="Access Governance">
              <MetricGrid
                items={[
                  { label: 'Active Query Rules', value: platform.access_governance.active_rules },
                  { label: 'Expiring in 7d', value: platform.access_governance.expiring_soon, tone: platform.access_governance.expiring_soon > 0 ? 'warning' : 'neutral' },
                  { label: 'Long-lived >90d', value: platform.access_governance.long_lived, tone: platform.access_governance.long_lived > 0 ? 'warning' : 'neutral' },
                  { label: 'Never Expires', value: platform.access_governance.never_expires, tone: platform.access_governance.never_expires > 0 ? 'danger' : 'neutral' },
                  { label: 'Recently Revoked 7d', value: platform.access_governance.recently_revoked },
                  { label: 'Sensitive Access 7d', value: platform.access_governance.sensitive_requests_7d },
                  { label: 'SQL Export 7d', value: platform.access_governance.sql_export_requests_7d },
                ]}
              />
            </DashboardPanel>
            <DashboardPanel title="DB Connection Health">
              {platform.db_connection_failures.length === 0 ? (
                <div className="rounded-lg border border-dashed border-border bg-panel-soft p-5 text-center text-[12px] text-muted">No failed connection checks.</div>
              ) : (
                <div className="grid gap-2">
                  {platform.db_connection_failures.slice(0, 8).map((conn) => (
                    <Link key={conn.id} to="/db-connections" className="flex items-start gap-3 rounded-lg border border-rose-100 bg-rose-50/60 px-3 py-2 hover:bg-rose-50">
                      <AlertTriangle className="mt-0.5 h-4 w-4 text-danger" />
                      <span className="min-w-0">
                        <span className="block truncate text-[12px] font-semibold text-ink">{conn.name}</span>
                        <span className="block truncate text-[11px] text-muted">{conn.last_test_error || 'Connection test failed'}</span>
                      </span>
                    </Link>
                  ))}
                </div>
              )}
            </DashboardPanel>
          </div>

          <div className="grid gap-4 xl:grid-cols-[1fr_1fr]">
            <DashboardPanel title="DB Metadata Health">
              <div className="grid gap-4">
                <MetricGrid
                  items={[
                    { label: 'Metadata Enabled Connections', value: platform.db_metadata_health.enabled_metadata_connection_count },
                    { label: 'Snapshot Connections', value: platform.db_metadata_health.object_snapshot_connection_count },
                    { label: 'Stale Snapshot Connections', value: platform.db_metadata_health.stale_object_connection_count, tone: platform.db_metadata_health.stale_object_connection_count > 0 ? 'warning' : 'neutral' },
                    { label: 'Metadata Objects', value: platform.db_metadata_health.object_count },
                  ]}
                />
                <div className="grid gap-2 sm:grid-cols-2">
                  <MetadataJobRow label="Inventory Sync" status={platform.db_metadata_health.inventory_job?.status} updatedAt={platform.db_metadata_health.inventory_job?.updated_at} />
                  <MetadataJobRow label="Object Sync" status={platform.db_metadata_health.object_job?.status} updatedAt={platform.db_metadata_health.object_job?.updated_at} />
                </div>
                <CountList items={platform.db_metadata_health.db_type_counts} empty="No DB connections configured." />
              </div>
            </DashboardPanel>
            <DashboardPanel title="Notification Health">
              <div className="grid gap-4">
                <MetricGrid
                  items={[
                    { label: 'Lark Failed 7d', value: platform.notification_health.lark_failed_7d, tone: platform.notification_health.lark_failed_7d > 0 ? 'danger' : 'neutral' },
                    { label: 'Card Callback Failed 7d', value: platform.notification_health.interactive_callback_failed_7d, tone: platform.notification_health.interactive_callback_failed_7d > 0 ? 'danger' : 'neutral' },
                    { label: 'Retry / Failure 7d', value: platform.notification_health.retry_or_failure_7d, tone: platform.notification_health.retry_or_failure_7d > 0 ? 'warning' : 'neutral' },
                    { label: 'Missing Lark Recipient', value: platform.notification_health.missing_lark_recipient_7d, tone: platform.notification_health.missing_lark_recipient_7d > 0 ? 'warning' : 'neutral' },
                    { label: 'Recipient Conflict', value: platform.notification_health.recipient_conflict_7d, tone: platform.notification_health.recipient_conflict_7d > 0 ? 'warning' : 'neutral' },
                  ]}
                />
                <CountList items={platform.notification_health.by_type} empty="No notification delivery failures in the last 7 days." />
              </div>
            </DashboardPanel>
          </div>

          <div className="grid gap-4 xl:grid-cols-4">
            <DashboardPanel title="Top Submitters 30d">
              <UserCountList items={platform.top_usage.submitters} />
            </DashboardPanel>
            <DashboardPanel title="Top DB Connections 30d">
              <CountList items={platform.top_usage.db_connections_by_tickets} />
            </DashboardPanel>
            <DashboardPanel title="Top Failed DB 30d">
              <CountList items={platform.top_usage.failed_db_connections} />
            </DashboardPanel>
            <DashboardPanel title="SQL Export Users 30d">
              <UserCountList items={platform.top_usage.sql_exports_by_user} />
            </DashboardPanel>
          </div>
        </div>
      ) : null}

      <div className="flex items-center gap-2 rounded-xl border border-border bg-panel-soft px-4 py-3 text-[12px] text-muted">
        <KeyRound className="h-4 w-4" />
        Query access inherited from auth groups is included in active access scope.
      </div>
    </div>
  )
}
