import { useEffect, useMemo, useRef, useState } from 'react'
import { ArrowDown, ArrowUp, Check, Search, SlidersHorizontal } from 'lucide-react'
import { DBMetadataSectionTabs } from '@/modules/db-metadata/components/DBMetadataSectionTabs'
import { listDBObjectSnapshots } from '@/modules/db-metadata/api'
import { cn } from '@/lib/utils'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import type { DBObjectConnectionOption, DBObjectSnapshot } from '@/shared/types/dbMetadata'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { PageIntro } from '@/shared/ui/PageIntro'
import { Pagination } from '@/shared/ui/Pagination'

const PAGE_SIZE = 20

const OBJECT_COLUMNS = [
  { key: 'connection', label: 'Connection' },
  { key: 'engine', label: 'Engine' },
  { key: 'databaseSchema', label: 'Database / Schema' },
  { key: 'table', label: 'Table' },
  { key: 'rows', label: 'Rows' },
  { key: 'dataSize', label: 'Data Size' },
  { key: 'indexSize', label: 'Index Size' },
  { key: 'totalSize', label: 'Total Size' },
  { key: 'snapshotTime', label: 'Snapshot Time' },
] as const

type ObjectColumnKey = (typeof OBJECT_COLUMNS)[number]['key']
type SortKey = 'rows' | 'dataSize' | 'indexSize' | 'totalSize'
type SortDirection = 'asc' | 'desc'

const DEFAULT_VISIBLE_COLUMNS: ObjectColumnKey[] = OBJECT_COLUMNS.map((column) => column.key)

export function DBMetadataObjectsPage() {
  const [items, setItems] = useState<DBObjectSnapshot[]>([])
  const [scanConnectionOptions, setScanConnectionOptions] = useState<DBObjectConnectionOption[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [offset, setOffset] = useState(0)
  const [engineFilter, setEngineFilter] = useState('all')
  const [connectionFilter, setConnectionFilter] = useState('all')
  const [keyword, setKeyword] = useState('')
  const [visibleColumns, setVisibleColumns] = useState<ObjectColumnKey[]>(DEFAULT_VISIBLE_COLUMNS)
  const [columnMenuOpen, setColumnMenuOpen] = useState(false)
  const [sortState, setSortState] = useState<{ key: SortKey; direction: SortDirection }>({ key: 'rows', direction: 'desc' })
  const columnMenuRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    let active = true

    async function bootstrap() {
      setLoading(true)
      setError('')
      try {
        const response = await listDBObjectSnapshots()
        if (active) {
          setItems(response.items)
          setScanConnectionOptions(response.connection_options ?? [])
        }
      } catch (loadError) {
        if (active) {
          setError(loadError instanceof ApiError ? loadError.message : 'Failed to load database object snapshots.')
        }
      } finally {
        if (active) {
          setLoading(false)
        }
      }
    }

    void bootstrap()
    return () => {
      active = false
    }
  }, [])

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

  const engineOptions = useMemo(() => {
    const engines = Array.from(new Set(items.map((item) => item.engine).filter(Boolean))).sort((a, b) => a.localeCompare(b))
    return ['all', ...engines]
  }, [items])

  const connectionSelectOptions = useMemo(() => {
    const optionsByID = new Map<number, string>()
    for (const option of scanConnectionOptions) {
      optionsByID.set(option.id, option.name)
    }
    for (const item of items) {
      if (!optionsByID.has(item.db_connection_id)) {
        optionsByID.set(item.db_connection_id, item.connection_name)
      }
    }
    return [
      { value: 'all', label: 'All Connections' },
      ...Array.from(optionsByID.entries())
        .sort(([, left], [, right]) => left.localeCompare(right))
        .map(([id, name]) => ({ value: String(id), label: name })),
    ]
  }, [items, scanConnectionOptions])

  const filteredItems = useMemo(() => {
    const loweredKeyword = keyword.trim().toLowerCase()
    return items.filter((item) => {
      if (engineFilter !== 'all' && item.engine !== engineFilter) {
        return false
      }
      if (connectionFilter !== 'all' && String(item.db_connection_id) !== connectionFilter) {
        return false
      }
      if (loweredKeyword === '') {
        return true
      }

      return [
        item.connection_name,
        item.engine,
        item.database_name,
        item.schema_name,
        item.table_name,
      ].some((value) => value.toLowerCase().includes(loweredKeyword))
    })
  }, [connectionFilter, engineFilter, items, keyword])

  const sortedItems = useMemo(() => {
    return [...filteredItems].sort((left, right) => compareObjectSnapshots(left, right, sortState))
  }, [filteredItems, sortState])

  const pagedItems = sortedItems.slice(offset, offset + PAGE_SIZE)

  function toggleSort(key: SortKey) {
    setSortState((current) => ({
      key,
      direction: current.key === key && current.direction === 'desc' ? 'asc' : 'desc',
    }))
    setOffset(0)
  }

  function toggleColumn(columnKey: ObjectColumnKey) {
    setVisibleColumns((current) => {
      if (current.includes(columnKey)) {
        if (current.length === 1) {
          return current
        }
        return current.filter((item) => item !== columnKey)
      }
      return OBJECT_COLUMNS.map((column) => column.key).filter((key) => current.includes(key) || key === columnKey)
    })
  }

  return (
    <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
      <PageIntro
        title="Objects"
        description="This view only shows MySQL and PostgreSQL object snapshots. Data comes from scheduled scans rather than live database queries from the page."
      />

      <DBMetadataSectionTabs />

      <section>
        <div className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_180px_220px_auto]">
          <label className="block">
            <div className="flex h-11 items-center gap-2 rounded-2xl border border-border bg-white px-3 shadow-soft transition focus-within:border-slate-400">
              <Search className="h-4 w-4 text-faint" />
              <input
                value={keyword}
                onChange={(event) => {
                  setKeyword(event.target.value)
                  setOffset(0)
                }}
                placeholder="Search connection / database / schema / table"
                className="h-full w-full bg-transparent text-[13px] text-ink outline-none placeholder:text-muted"
              />
            </div>
          </label>

          <div>
            <DropdownSelect
              ariaLabel="Engine"
              value={engineFilter}
              onChange={(value) => {
                setEngineFilter(value)
                setOffset(0)
              }}
              options={engineOptions.map((option) => ({
                value: option,
                label: option === 'all' ? 'All Engines' : option,
              }))}
            />
          </div>

          <div>
            <DropdownSelect
              ariaLabel="Connection"
              value={connectionFilter}
              onChange={(value) => {
                setConnectionFilter(value)
                setOffset(0)
              }}
              options={connectionSelectOptions.map((option) => ({
                value: option.value,
                label: option.label,
              }))}
            />
          </div>

          <div ref={columnMenuRef} className="flex items-end justify-end">
            <div className="flex items-center gap-2">
              <button
                type="button"
                aria-haspopup="menu"
                aria-expanded={columnMenuOpen}
                aria-label="Visible Columns"
                onClick={() => setColumnMenuOpen((current) => !current)}
                className={cn(
                  'inline-flex h-11 w-11 items-center justify-center rounded-2xl border border-border bg-white text-ink shadow-soft transition',
                  columnMenuOpen ? 'border-slate-300' : 'hover:border-slate-300',
                )}
              >
                <SlidersHorizontal className="h-4 w-4" />
              </button>
            </div>

            {columnMenuOpen ? (
              <div className="relative">
                <div className="absolute right-0 top-2 z-20 w-[280px] max-w-[calc(100vw-2rem)] overflow-hidden rounded-2xl border border-border bg-white p-2 shadow-[0_22px_45px_rgba(15,23,42,0.14)]">
                  <div className="px-3 py-2">
                    <p className="text-[12px] font-bold uppercase tracking-[0.16em] text-faint">Table fields</p>
                    <p className="mt-1 text-[14px] font-semibold text-ink">Column Filter</p>
                  </div>
                  <div role="menu" aria-label="Visible columns menu" className="grid gap-1">
                    {OBJECT_COLUMNS.map((column) => {
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
              </div>
            ) : null}
          </div>
        </div>
      </section>

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      <section className="overflow-hidden rounded-xl border border-border bg-panel shadow-soft">
        {loading ? (
          <LoadingBlock message="Loading database objects..." className="min-h-[320px] rounded-none border-0 bg-transparent" />
        ) : items.length === 0 ? (
          <div className="m-4 flex min-h-[240px] items-center justify-center rounded-xl border border-dashed border-border bg-panel-soft text-sm text-muted">
            No object snapshots yet.
          </div>
        ) : filteredItems.length === 0 ? (
          <div className="m-4 flex min-h-[240px] items-center justify-center rounded-xl border border-dashed border-border bg-panel-soft text-sm text-muted">
            No object snapshots match the current filters.
          </div>
        ) : (
          <div className="grid gap-3 p-3">
            <div className="overflow-x-auto">
              <table className="min-w-full border-collapse">
                <thead className="bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                  <tr>
                    {visibleColumns.includes('connection') ? <th className="px-3 py-3">Connection</th> : null}
                    {visibleColumns.includes('engine') ? <th className="px-3 py-3">Engine</th> : null}
                    {visibleColumns.includes('databaseSchema') ? <th className="px-3 py-3">Database / Schema</th> : null}
                    {visibleColumns.includes('table') ? <th className="px-3 py-3">Table</th> : null}
                    {visibleColumns.includes('rows') ? <SortableHeader label="Rows" sortKey="rows" sortState={sortState} onSort={toggleSort} /> : null}
                    {visibleColumns.includes('dataSize') ? <SortableHeader label="Data Size" sortKey="dataSize" sortState={sortState} onSort={toggleSort} /> : null}
                    {visibleColumns.includes('indexSize') ? <SortableHeader label="Index Size" sortKey="indexSize" sortState={sortState} onSort={toggleSort} /> : null}
                    {visibleColumns.includes('totalSize') ? <SortableHeader label="Total Size" sortKey="totalSize" sortState={sortState} onSort={toggleSort} /> : null}
                    {visibleColumns.includes('snapshotTime') ? <th className="px-3 py-3">Snapshot Time</th> : null}
                  </tr>
                </thead>
                <tbody>
                  {pagedItems.map((item) => (
                    <tr key={item.id} className="border-t border-border text-sm text-ink hover:bg-slate-50/70">
                      {visibleColumns.includes('connection') ? (
                        <td className="px-3 py-2.5 align-top text-[12px] font-semibold">{item.connection_name}</td>
                      ) : null}
                      {visibleColumns.includes('engine') ? (
                        <td className="px-3 py-2.5 align-top text-[12px]">{item.engine}</td>
                      ) : null}
                      {visibleColumns.includes('databaseSchema') ? (
                        <td className="px-3 py-2.5 align-top text-[12px]">
                          {formatDatabaseSchema(item)}
                        </td>
                      ) : null}
                      {visibleColumns.includes('table') ? (
                        <td className="px-3 py-2.5 align-top font-mono text-[12px]">{item.table_name}</td>
                      ) : null}
                      {visibleColumns.includes('rows') ? (
                        <td className="px-3 py-2.5 align-top text-[12px] tabular-nums">{formatRows(item.row_count)}</td>
                      ) : null}
                      {visibleColumns.includes('dataSize') ? (
                        <td className="px-3 py-2.5 align-top text-[12px]">{formatGB(item.data_size_bytes)}</td>
                      ) : null}
                      {visibleColumns.includes('indexSize') ? (
                        <td className="px-3 py-2.5 align-top text-[12px]">{formatGB(item.index_size_bytes)}</td>
                      ) : null}
                      {visibleColumns.includes('totalSize') ? (
                        <td className="px-3 py-2.5 align-top text-[12px]">{formatGB(item.data_size_bytes + item.index_size_bytes)}</td>
                      ) : null}
                      {visibleColumns.includes('snapshotTime') ? (
                        <td className="px-3 py-2.5 align-top whitespace-nowrap text-[12px] text-muted">{formatDateTime(item.snapshot_at)}</td>
                      ) : null}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <Pagination
              offset={offset}
              pageSize={PAGE_SIZE}
              count={pagedItems.length}
              total={sortedItems.length}
              onChange={setOffset}
            />
          </div>
        )}
      </section>
    </div>
  )
}

function SortableHeader({
  label,
  sortKey,
  sortState,
  onSort,
}: {
  label: string
  sortKey: SortKey
  sortState: { key: SortKey; direction: SortDirection }
  onSort: (key: SortKey) => void
}) {
  const active = sortState.key === sortKey
  return (
    <th className="px-3 py-3">
      <button
        type="button"
        aria-label={active ? `${label} ${sortState.direction.toUpperCase()}` : label}
        onClick={() => onSort(sortKey)}
        className={cn('inline-flex items-center gap-1 text-left uppercase tracking-[0.16em] transition hover:text-ink', active ? 'text-ink' : 'text-faint')}
      >
        {label}
        {active ? sortState.direction === 'desc' ? <ArrowDown className="h-3 w-3" aria-hidden="true" /> : <ArrowUp className="h-3 w-3" aria-hidden="true" /> : null}
      </button>
    </th>
  )
}

function compareObjectSnapshots(left: DBObjectSnapshot, right: DBObjectSnapshot, sortState: { key: SortKey; direction: SortDirection }) {
  const leftValue = getSortValue(left, sortState.key)
  const rightValue = getSortValue(right, sortState.key)
  const direction = sortState.direction === 'desc' ? -1 : 1
  if (leftValue !== rightValue) {
    return leftValue < rightValue ? -direction : direction
  }
  const leftTotal = getTotalSize(left)
  const rightTotal = getTotalSize(right)
  if (leftTotal !== rightTotal) {
    return rightTotal - leftTotal
  }
  return left.table_name.localeCompare(right.table_name)
}

function getSortValue(item: DBObjectSnapshot, key: SortKey) {
  switch (key) {
    case 'rows':
      return item.row_count
    case 'dataSize':
      return item.data_size_bytes
    case 'indexSize':
      return item.index_size_bytes
    case 'totalSize':
      return getTotalSize(item)
  }
}

function getTotalSize(item: DBObjectSnapshot) {
  return item.data_size_bytes + item.index_size_bytes
}

function formatDatabaseSchema(item: DBObjectSnapshot) {
  if (item.engine === 'mysql' && item.database_name === item.schema_name) {
    return item.database_name
  }
  return `${item.database_name} / ${item.schema_name}`
}

function formatRows(rows: number) {
  if (!Number.isFinite(rows) || rows <= 0) {
    return '0'
  }
  return Math.round(rows).toLocaleString()
}

function formatGB(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return '0.00 GB'
  }
  const gb = bytes / 1024 / 1024 / 1024
  return `${gb.toLocaleString(undefined, {
    minimumFractionDigits: gb < 10 ? 2 : 1,
    maximumFractionDigits: gb < 10 ? 2 : 1,
  })} GB`
}
