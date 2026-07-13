import { useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { CalendarDays, Check, ChevronLeft, ChevronRight, SlidersHorizontal } from 'lucide-react'
import { Link } from 'react-router-dom'
import { EmptyState } from '@/components/tickets/EmptyState'
import { ApiError } from '@/shared/api/client'
import { cn } from '@/lib/utils'
import { formatDateTime } from '@/shared/lib/format'
import { useDebouncedValue } from '@/shared/lib/useDebouncedValue'
import { MAESTRO_REALTIME_EVENT } from '@/shared/realtime/events'
import type { Ticket, TicketStatus, TicketType } from '@/shared/types/ticket'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { Pagination } from '@/shared/ui/Pagination'
import { SearchInput } from '@/shared/ui/SearchInput'
import { StatusBadge } from '@/shared/ui/StatusBadge'
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
import { listTickets } from '@/modules/tickets/api'

const PAGE_SIZE = 20

const STATUS_OPTIONS: Array<{ value: '' | TicketStatus; label: string }> = [
  { value: '', label: 'All Status' },
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
  { value: 'needs_admin_attention', label: 'Needs Admin Attention' },
]

const TYPE_OPTIONS: Array<{ value: '' | TicketType; label: string }> = [
  { value: '', label: 'All Types' },
  { value: 'ddl', label: 'DDL' },
  { value: 'dml', label: 'DML' },
  { value: 'redis_command', label: 'Redis' },
  { value: 'query_access', label: 'Query Access' },
  { value: 'sql_export', label: 'SQL Export' },
  { value: 'sensitive_query_access', label: 'Sensitive Query Access' },
]

type TicketColumnKey = 'ticketNo' | 'title' | 'description' | 'type' | 'status' | 'submitter' | 'dbConnection' | 'database' | 'created'

const TICKET_COLUMNS: Array<{ key: TicketColumnKey; label: string }> = [
  { key: 'ticketNo', label: 'Ticket No.' },
  { key: 'title', label: 'Title' },
  { key: 'description', label: 'Description' },
  { key: 'type', label: 'Type' },
  { key: 'status', label: 'Status' },
  { key: 'submitter', label: 'Submitter' },
  { key: 'dbConnection', label: 'DB Connection' },
  { key: 'database', label: 'Database' },
  { key: 'created', label: 'Created' },
]

const DEFAULT_VISIBLE_COLUMNS: TicketColumnKey[] = ['ticketNo', 'title', 'type', 'status', 'submitter', 'dbConnection', 'database', 'created']

function formatTicketTypeLabel(ticketType: TicketType) {
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
      return 'Sensitive Query Access'
    default:
      return ticketType
  }
}

type TicketFilters = {
  ticketNo: string
  title: string
  submitter: string
  type: '' | TicketType
  status: '' | TicketStatus
  from: string
  to: string
}

const EMPTY_FILTERS: TicketFilters = {
  ticketNo: '',
  title: '',
  submitter: '',
  type: '',
  status: '',
  from: '',
  to: '',
}

export function TicketsPage() {
  const [tickets, setTickets] = useState<Ticket[]>([])
  const [total, setTotal] = useState(0)
  const [filters, setFilters] = useState<TicketFilters>(EMPTY_FILTERS)
  const [offset, setOffset] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [visibleColumns, setVisibleColumns] = useState<TicketColumnKey[]>(DEFAULT_VISIBLE_COLUMNS)
  const [columnMenuOpen, setColumnMenuOpen] = useState(false)
  const columnMenuRef = useRef<HTMLDivElement | null>(null)
  const query = useMemo(() => ({ filters, offset }), [filters, offset])
  const debouncedQuery = useDebouncedValue(query, 300)

  async function loadTickets(nextFilters: TicketFilters, nextOffset: number) {
    setLoading(true)
    setError('')

    try {
      const response = await listTickets({
        status: nextFilters.status || undefined,
        type: nextFilters.type || undefined,
        ticketNo: nextFilters.ticketNo.trim() || undefined,
        title: nextFilters.title.trim() || undefined,
        submitter: nextFilters.submitter.trim() || undefined,
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
    void loadTickets(debouncedQuery.filters, debouncedQuery.offset)
  }, [debouncedQuery])

  useEffect(() => {
    function handlePointerDown(event: MouseEvent) {
      const target = event.target as Node
      if (!columnMenuRef.current?.contains(target)) {
        setColumnMenuOpen(false)
      }
    }

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        setColumnMenuOpen(false)
      }
    }

    document.addEventListener('mousedown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('mousedown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [])

  useEffect(() => {
    const handleRealtime = (event: Event) => {
      const realtimeEvent = event as CustomEvent<{ event?: string }>
      if (realtimeEvent.detail?.event !== 'ticket.updated') {
        return
      }
      void loadTickets(filters, offset)
    }

    window.addEventListener(MAESTRO_REALTIME_EVENT, handleRealtime)
    return () => {
      window.removeEventListener(MAESTRO_REALTIME_EVENT, handleRealtime)
    }
  }, [filters, offset])

  const hasActiveFilters = useMemo(
    () => Boolean(filters.ticketNo.trim() || filters.title.trim() || filters.submitter.trim() || filters.type || filters.status || filters.from || filters.to),
    [filters],
  )

  function updateFilters(patch: Partial<TicketFilters>) {
    setFilters((current) => ({ ...current, ...patch }))
    setOffset(0)
  }

  function toggleColumn(key: TicketColumnKey) {
    setVisibleColumns((current) => {
      if (current.includes(key)) {
        return current.length === 1 ? current : current.filter((column) => column !== key)
      }
      return [...current, key]
    })
  }

  return (
    <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
      <section>
        <div className="py-1">
          <div className="flex w-full flex-nowrap items-end gap-2.5">
            <FilterHint
              hint="Search by ticket number."
              className="w-[170px] shrink-0"
            >
              <SearchInput
                value={filters.ticketNo}
                onChange={(event) => updateFilters({ ticketNo: event.target.value })}
                placeholder="Ticket no."
              />
            </FilterHint>
            <FilterHint
              hint="Search by ticket title."
              className="w-[170px] shrink-0"
            >
              <SearchInput
                value={filters.title}
                onChange={(event) => updateFilters({ title: event.target.value })}
                placeholder="Title"
              />
            </FilterHint>
            <FilterHint
              hint="Search by submitter username."
              className="w-[160px] shrink-0"
            >
              <SearchInput
                value={filters.submitter}
                onChange={(event) => updateFilters({ submitter: event.target.value })}
                placeholder="Submitter"
              />
            </FilterHint>
            <FilterHint
              hint="Filter by ticket type."
              className="w-[160px] shrink-0"
            >
              <SelectField
                ariaLabel="Type"
                value={filters.type}
                onChange={(value) => updateFilters({ type: value as '' | TicketType })}
                options={TYPE_OPTIONS}
              />
            </FilterHint>
            <FilterHint
              hint="Filter by ticket status."
              className="w-[160px] shrink-0"
            >
              <SelectField
                ariaLabel="Status"
                value={filters.status}
                onChange={(value) => updateFilters({ status: value as '' | TicketStatus })}
                options={STATUS_OPTIONS}
              />
            </FilterHint>
            <FilterHint
              hint="Show tickets created after this date and time."
              className="w-[180px] shrink-0"
            >
              <DateTimeField
                value={filters.from}
                onChange={(value) => updateFilters({ from: value })}
                placeholder="Start date and time"
                presets={FROM_DATE_PRESETS}
              />
            </FilterHint>
            <FilterHint
              hint="Show tickets created before this date and time."
              className="w-[180px] shrink-0"
            >
              <DateTimeField
                value={filters.to}
                onChange={(value) => updateFilters({ to: value })}
                placeholder="End date and time"
                presets={TO_DATE_PRESETS}
              />
            </FilterHint>
            <div ref={columnMenuRef} className="relative flex shrink-0 items-end">
              <button
                type="button"
                aria-haspopup="menu"
                aria-expanded={columnMenuOpen}
                aria-label="Visible Columns"
                title="Visible Columns"
                onClick={() => setColumnMenuOpen((current) => !current)}
                className={cn(
                  'inline-flex h-9 w-9 items-center justify-center rounded-lg border border-border bg-panel text-ink shadow-soft transition',
                  columnMenuOpen ? 'border-slate-300' : 'hover:border-slate-300 hover:bg-panel-soft',
                )}
              >
                <SlidersHorizontal className="h-4 w-4" />
              </button>
              {columnMenuOpen ? (
                <div className="absolute right-0 top-[calc(100%+8px)] z-30 w-[260px] max-w-[calc(100vw-2rem)] overflow-hidden rounded-2xl border border-border bg-white p-2 shadow-[0_22px_45px_rgba(15,23,42,0.14)]">
                  <div className="px-3 py-2">
                    <p className="text-[12px] font-bold uppercase tracking-[0.16em] text-faint">Table fields</p>
                    <p className="mt-1 text-[14px] font-semibold text-ink">Column Filter</p>
                  </div>
                  <div role="menu" aria-label="Visible columns menu" className="grid gap-1">
                    {TICKET_COLUMNS.map((column) => {
                      const selected = visibleColumns.includes(column.key)
                      return (
                        <button
                          key={column.key}
                          type="button"
                          role="menuitemcheckbox"
                          aria-checked={selected}
                          onClick={() => toggleColumn(column.key)}
                          className={cn(
                            'flex items-center gap-3 rounded-xl px-4 py-3 text-left text-[13px] transition',
                            selected ? 'bg-panel-soft text-ink' : 'text-ink hover:bg-panel-soft/70',
                          )}
                        >
                          <span className="flex h-4 w-4 shrink-0 items-center justify-center">
                            {selected ? <Check className="h-4 w-4" /> : null}
                          </span>
                          <span>{column.label}</span>
                        </button>
                      )
                    })}
                  </div>
                </div>
              ) : null}
            </div>
          </div>
        </div>
      </section>

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      <DataTableSurface>
        {loading ? (
          <LoadingBlock message="Loading tickets..." className="h-[320px] rounded-none border-0 bg-transparent" />
        ) : tickets.length === 0 ? (
          <div className="bg-panel">
            <EmptyState variant={hasActiveFilters ? 'search' : 'history'} />
          </div>
        ) : (
          <DataTableScroll>
            <DataTable className="min-w-[1360px] table-fixed">
              <colgroup>
                {visibleColumns.includes('ticketNo') ? <col className="w-[230px]" /> : null}
                {visibleColumns.includes('title') ? <col className="w-[260px]" /> : null}
                {visibleColumns.includes('description') ? <col className="w-[280px]" /> : null}
                {visibleColumns.includes('type') ? <col className="w-[110px]" /> : null}
                {visibleColumns.includes('status') ? <col className="w-[150px]" /> : null}
                {visibleColumns.includes('submitter') ? <col className="w-[110px]" /> : null}
                {visibleColumns.includes('dbConnection') ? <col className="w-[260px]" /> : null}
                {visibleColumns.includes('database') ? <col className="w-[210px]" /> : null}
                {visibleColumns.includes('created') ? <col className="w-[150px]" /> : null}
              </colgroup>
              <DataTableHead>
                <tr>
                  {visibleColumns.includes('ticketNo') ? <DataTableHeaderCell>Ticket No.</DataTableHeaderCell> : null}
                  {visibleColumns.includes('title') ? <DataTableHeaderCell>Title</DataTableHeaderCell> : null}
                  {visibleColumns.includes('description') ? <DataTableHeaderCell>Description</DataTableHeaderCell> : null}
                  {visibleColumns.includes('type') ? <DataTableHeaderCell>Type</DataTableHeaderCell> : null}
                  {visibleColumns.includes('status') ? <DataTableHeaderCell>Status</DataTableHeaderCell> : null}
                  {visibleColumns.includes('submitter') ? <DataTableHeaderCell>Submitter</DataTableHeaderCell> : null}
                  {visibleColumns.includes('dbConnection') ? <DataTableHeaderCell>DB Connection</DataTableHeaderCell> : null}
                  {visibleColumns.includes('database') ? <DataTableHeaderCell>Database</DataTableHeaderCell> : null}
                  {visibleColumns.includes('created') ? <DataTableHeaderCell>Created</DataTableHeaderCell> : null}
                </tr>
              </DataTableHead>
              <DataTableBody>
                {tickets.map((ticket) => (
                  <DataTableRow key={ticket.id}>
                    {visibleColumns.includes('ticketNo') ? (
                      <DataTableCell>
                        <Link
                          to={`/tickets/${ticket.ticket_no}`}
                          title={ticket.ticket_no}
                          className="inline-flex max-w-full items-center rounded-md border border-transparent px-1.5 py-0.5 text-[12px] font-normal leading-5 text-ink transition hover:border-accent/15 hover:bg-accent-soft hover:text-accent"
                        >
                          <span className="block max-w-[210px] truncate">{ticket.ticket_no}</span>
                        </Link>
                      </DataTableCell>
                    ) : null}
                    {visibleColumns.includes('title') ? (
                      <DataTableCell>
                        <p className="truncate text-[12px] font-normal leading-5 text-ink" title={ticket.title}>{ticket.title}</p>
                      </DataTableCell>
                    ) : null}
                    {visibleColumns.includes('description') ? (
                      <DataTableCell>
                        <p className="truncate text-[12px] leading-5 text-ink" title={ticket.description || undefined}>{ticket.description || '-'}</p>
                      </DataTableCell>
                    ) : null}
                    {visibleColumns.includes('type') ? (
                      <DataTableCell className="whitespace-nowrap">
                        <span className="inline-flex items-center text-[10px] font-semibold uppercase leading-none tracking-[0.12em] text-muted">
                          {formatTicketTypeLabel(ticket.ticket_type)}
                        </span>
                      </DataTableCell>
                    ) : null}
                    {visibleColumns.includes('status') ? (
                      <DataTableCell className="whitespace-nowrap">
                        <StatusBadge status={ticket.status} className="h-6 items-center justify-center px-3 py-0 text-[10px] leading-none" />
                      </DataTableCell>
                    ) : null}
                    {visibleColumns.includes('submitter') ? <DataTableCell><p className="truncate" title={String(ticket.submitter_name || ticket.submitter_id)}>{ticket.submitter_name || ticket.submitter_id}</p></DataTableCell> : null}
                    {visibleColumns.includes('dbConnection') ? (
                      <DataTableCell>
                        <p className="truncate" title={ticket.db_connection_name || undefined}>{ticket.db_connection_name || '-'}</p>
                      </DataTableCell>
                    ) : null}
                    {visibleColumns.includes('database') ? (
                      <DataTableCell>
                        <p className="truncate" title={ticket.database_name || undefined}>{ticket.database_name || '-'}</p>
                      </DataTableCell>
                    ) : null}
                    {visibleColumns.includes('created') ? <DataTableCell className="whitespace-nowrap">{formatDateTime(ticket.created_at)}</DataTableCell> : null}
                  </DataTableRow>
                ))}
              </DataTableBody>
            </DataTable>
          </DataTableScroll>
        )}
      </DataTableSurface>

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
      <div className="pointer-events-none absolute left-0 top-[calc(100%+8px)] z-20 hidden w-full rounded-md border border-border bg-white px-3 py-2 text-[11px] font-medium text-muted shadow-soft group-hover:block">
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
