import { useEffect, useMemo, useRef, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { CalendarDays, ChevronLeft, ChevronRight, Plus, Search } from 'lucide-react'
import { Link } from 'react-router-dom'
import { EmptyState } from '@/components/tickets/EmptyState'
import { ApiError } from '@/shared/api/client'
import { useAuth } from '@/shared/auth/AuthContext'
import { formatDateTime } from '@/shared/lib/format'
import { MAESTRO_REALTIME_EVENT } from '@/shared/realtime/events'
import type { Ticket, TicketStatus, TicketType } from '@/shared/types/ticket'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { PageIntro } from '@/shared/ui/PageIntro'
import { Pagination } from '@/shared/ui/Pagination'
import { StatusBadge } from '@/shared/ui/StatusBadge'
import { listTickets } from '@/modules/tickets/api'

const PAGE_SIZE = 20

const STATUS_OPTIONS: Array<{ value: '' | TicketStatus; label: string }> = [
  { value: '', label: 'All' },
  { value: 'pending_review', label: 'Pending Review' },
  { value: 'approved', label: 'Approved' },
  { value: 'rejected', label: 'Rejected' },
  { value: 'withdrawn', label: 'Withdrawn' },
  { value: 'pending_execution', label: 'Pending Execution' },
  { value: 'executing', label: 'Executing' },
  { value: 'completed', label: 'Completed' },
  { value: 'failed', label: 'Failed' },
  { value: 'stopped', label: 'Stopped' },
  { value: 'interrupted', label: 'Interrupted' },
]

const TYPE_OPTIONS: Array<{ value: '' | TicketType; label: string }> = [
  { value: '', label: 'All Types' },
  { value: 'ddl', label: 'DDL' },
  { value: 'dml', label: 'DML' },
  { value: 'redis_command', label: 'Redis' },
  { value: 'sql_export', label: 'SQL Export' },
  { value: 'sensitive_query_access', label: 'Sensitive Query Access' },
]

function formatTicketTypeLabel(ticketType: TicketType) {
  switch (ticketType) {
    case 'ddl':
      return 'DDL'
    case 'dml':
      return 'DML'
    case 'redis_command':
      return 'Redis'
    case 'sql_export':
      return 'SQL Export'
    case 'sensitive_query_access':
      return 'Sensitive Query Access'
    default:
      return ticketType
  }
}

type TicketFilters = {
  keyword: string
  type: '' | TicketType
  status: '' | TicketStatus
  from: string
  to: string
}

const EMPTY_FILTERS: TicketFilters = {
  keyword: '',
  type: '',
  status: '',
  from: '',
  to: '',
}

export function TicketsPage() {
  const { user } = useAuth()
  const [tickets, setTickets] = useState<Ticket[]>([])
  const [total, setTotal] = useState(0)
  const [filters, setFilters] = useState<TicketFilters>(EMPTY_FILTERS)
  const [appliedFilters, setAppliedFilters] = useState<TicketFilters>(EMPTY_FILTERS)
  const [offset, setOffset] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  async function loadTickets(nextFilters: TicketFilters, nextOffset: number) {
    setLoading(true)
    setError('')

    try {
      const response = await listTickets({
        status: nextFilters.status || undefined,
        type: nextFilters.type || undefined,
        keyword: nextFilters.keyword.trim() || undefined,
        from: toRFC3339(nextFilters.from) || undefined,
        to: toRFC3339(nextFilters.to) || undefined,
        limit: PAGE_SIZE,
        offset: nextOffset,
      })
      setTickets(Array.isArray(response.tickets) ? response.tickets : [])
      setTotal(response.total)
    } catch (loadError) {
      setError(loadError instanceof ApiError ? loadError.message : 'Failed to load tickets. Please try again.')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void loadTickets(appliedFilters, offset)
  }, [appliedFilters, offset])

  useEffect(() => {
    const handleRealtime = (event: Event) => {
      const realtimeEvent = event as CustomEvent<{ event?: string }>
      if (realtimeEvent.detail?.event !== 'ticket.updated') {
        return
      }
      void loadTickets(appliedFilters, offset)
    }

    window.addEventListener(MAESTRO_REALTIME_EVENT, handleRealtime)
    return () => {
      window.removeEventListener(MAESTRO_REALTIME_EVENT, handleRealtime)
    }
  }, [appliedFilters, offset])

  const canCreateTicket = user?.permissions.includes('tickets.apply') ?? false
  const hasActiveFilters = useMemo(
    () => Boolean(appliedFilters.keyword.trim() || appliedFilters.type || appliedFilters.status || appliedFilters.from || appliedFilters.to),
    [appliedFilters],
  )

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setAppliedFilters(filters)
    setOffset(0)
  }

  return (
    <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
      <PageIntro
        title="Ticket Workspace"
        description="View submitted, pending review, pending execution, and historical tickets in a single queue. Visible scope is determined by your current role and backend permissions."
        actions={
          canCreateTicket ? (
            <Link
              to="/tickets/new"
              className="inline-flex h-10 shrink-0 items-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition-colors hover:bg-slate-800"
            >
              <Plus className="h-4 w-4" />
              New Ticket
            </Link>
          ) : null
        }
      />

      <section>
        <form className="py-1" onSubmit={handleSubmit}>
          <div className="flex flex-wrap items-end gap-2.5">
            <FilterHint
              hint="Search by ticket number, title, or submitter."
              className="min-w-[220px] flex-[1.3] xl:max-w-[320px]"
            >
              <input
                value={filters.keyword}
                onChange={(event) => setFilters((current) => ({ ...current, keyword: event.target.value }))}
                className="h-10 w-full rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                placeholder="Ticket no. / title / submitter"
              />
            </FilterHint>
            <FilterHint
              hint="Filter by ticket type."
              className="min-w-[180px] flex-1 xl:max-w-[220px]"
            >
              <SelectField
                ariaLabel="Type"
                value={filters.type}
                onChange={(value) => setFilters((current) => ({ ...current, type: value as '' | TicketType }))}
                options={TYPE_OPTIONS}
              />
            </FilterHint>
            <FilterHint
              hint="Filter by ticket status."
              className="min-w-[180px] flex-1 xl:max-w-[220px]"
            >
              <SelectField
                ariaLabel="Status"
                value={filters.status}
                onChange={(value) => setFilters((current) => ({ ...current, status: value as '' | TicketStatus }))}
                options={STATUS_OPTIONS}
              />
            </FilterHint>
            <FilterHint
              hint="Show tickets created after this date and time."
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
              hint="Show tickets created before this date and time."
              className="min-w-[210px] flex-1 xl:max-w-[250px]"
            >
              <DateTimeField
                value={filters.to}
                onChange={(value) => setFilters((current) => ({ ...current, to: value }))}
                placeholder="End date and time"
                presets={TO_DATE_PRESETS}
              />
            </FilterHint>
            <button
              type="submit"
              className="inline-flex h-10 shrink-0 items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white transition hover:bg-slate-800"
            >
              <Search className="h-4 w-4" />
              Apply
            </button>
          </div>
        </form>
      </section>

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      <div className="overflow-hidden rounded-xl border border-border bg-panel shadow-soft">
        {loading ? (
          <LoadingBlock message="Loading tickets..." className="h-[320px] rounded-none border-0 bg-transparent" />
        ) : tickets.length === 0 ? (
          <div className="bg-panel">
            <EmptyState variant={hasActiveFilters ? 'search' : 'history'} />
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="min-w-full border-collapse">
              <thead className="bg-editor-toolbar">
                <tr className="text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                  <th className="px-4 py-3">Ticket No.</th>
                  <th className="px-4 py-3">Title</th>
                  <th className="px-4 py-3">Type</th>
                  <th className="px-4 py-3">Status</th>
                  <th className="px-4 py-3">Submitter</th>
                  <th className="px-4 py-3">Created</th>
                </tr>
              </thead>
              <tbody>
                {tickets.map((ticket) => (
                  <tr
                    key={ticket.id}
                    className="border-t border-border/90 text-sm text-ink transition-colors hover:bg-slate-50/70"
                  >
                    <td className="px-4 py-3.5 align-top">
                      <Link
                        to={`/tickets/${ticket.id}`}
                        className="inline-flex rounded-md border border-transparent px-1.5 py-1 font-mono text-[12px] font-semibold text-accent transition hover:border-accent/15 hover:bg-accent-soft hover:text-blue-700"
                      >
                        {ticket.ticket_no}
                      </Link>
                    </td>
                    <td className="px-4 py-3.5 align-top">
                      <div>
                        <p className="text-[14px] font-semibold tracking-tight text-ink">{ticket.title}</p>
                        {ticket.description ? <p className="mt-1 max-w-[420px] text-[12px] leading-5 text-muted">{ticket.description}</p> : null}
                      </div>
                    </td>
                    <td className="px-4 py-3.5 align-top">
                      <span className="rounded-full border border-border bg-panel-soft px-2.5 py-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted">
                        {formatTicketTypeLabel(ticket.ticket_type)}
                      </span>
                    </td>
                    <td className="px-4 py-3.5 align-top">
                      <StatusBadge status={ticket.status} />
                    </td>
                    <td className="px-4 py-3.5 align-top text-[12px] text-muted">{ticket.submitter_name || ticket.submitter_id}</td>
                    <td className="px-4 py-3.5 align-top text-[12px] text-muted">{formatDateTime(ticket.created_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <Pagination offset={offset} pageSize={PAGE_SIZE} count={tickets.length} total={total} onChange={setOffset} />
    </div>
  )
}

function SelectField({
  ariaLabel,
  value,
  onChange,
  options,
}: {
  ariaLabel: string
  value: string
  onChange: (value: string) => void
  options: ReadonlyArray<{ value: string; label: string }>
}) {
  return (
    <DropdownSelect
      ariaLabel={ariaLabel}
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
  children: ReactNode
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

function toRFC3339(value: string) {
  if (!value) {
    return ''
  }
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '' : date.toISOString()
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
        className="inline-flex h-10 w-full items-center justify-between gap-2 rounded-lg border border-border bg-panel-soft px-3 text-left text-[13px] text-ink outline-none transition hover:bg-white focus:border-accent focus:ring-2 focus:ring-accent/20"
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
              className="h-10 flex-1 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
            />
            {value ? (
              <button
                type="button"
                onClick={clearValue}
                className="inline-flex h-10 items-center justify-center rounded-lg border border-border bg-panel-soft px-3 text-[12px] font-semibold text-muted transition hover:bg-page hover:text-ink"
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

function startOfDay(date: Date) {
  const value = new Date(date)
  value.setHours(0, 0, 0, 0)
  return value
}

function endOfDay(date: Date) {
  const value = new Date(date)
  value.setHours(23, 59, 0, 0)
  return value
}

function startOfMonth(date: Date) {
  return new Date(date.getFullYear(), date.getMonth(), 1, 0, 0, 0, 0)
}

function endOfMonth(date: Date) {
  return new Date(date.getFullYear(), date.getMonth() + 1, 0, 23, 59, 0, 0)
}

function addDays(date: Date, amount: number) {
  const value = new Date(date)
  value.setDate(value.getDate() + amount)
  return value
}

function addMonths(date: Date, amount: number) {
  return new Date(date.getFullYear(), date.getMonth() + amount, 1, date.getHours(), date.getMinutes(), 0, 0)
}

function isSameDay(left: Date, right: Date) {
  return left.getFullYear() === right.getFullYear()
    && left.getMonth() === right.getMonth()
    && left.getDate() === right.getDate()
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
  const hours = String(date.getHours()).padStart(2, '0')
  const minutes = String(date.getMinutes()).padStart(2, '0')
  return `${year}-${month}-${day}T${hours}:${minutes}`
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
  return formatDateTime(date.toISOString())
}
