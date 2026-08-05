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
import { getDashboard, type DashboardCount, type DashboardQueryAccessScope, type DashboardResponse, type DashboardTicketSummary } from '@/modules/dashboard/api'

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

function AccessTable({ scopes }: { scopes: DashboardQueryAccessScope[] }) {
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
                <Link to={scope.renew_ticket_path} className="inline-flex items-center gap-1 text-[12px] font-semibold text-ink hover:text-accent">
                  Renew
                  <ArrowRight className="h-3.5 w-3.5" />
                </Link>
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
        <DashboardPanel title="Submission DB Scope">
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

        <DashboardPanel title="Expiring Query Access" action={<Link to="/tickets/new?ticket_type=query_access" className="text-[12px] font-semibold text-muted hover:text-ink">Request access</Link>}>
          <AccessTable scopes={expiringAccess.length > 0 ? expiringAccess : data.personal.query_access_scopes.slice(0, 5)} />
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
            <DashboardPanel title="Recent Platform Attention">
              <TicketTable tickets={platform.recent_attention} empty="No active platform attention items." />
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
        </div>
      ) : null}

      <div className="flex items-center gap-2 rounded-xl border border-border bg-panel-soft px-4 py-3 text-[12px] text-muted">
        <KeyRound className="h-4 w-4" />
        Query access inherited from auth groups is included in active access scope.
      </div>
    </div>
  )
}
