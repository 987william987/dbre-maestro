import { useEffect, useMemo, useRef, useState } from 'react'
import { Check, Search, SlidersHorizontal } from 'lucide-react'
import { DBMetadataSectionTabs } from '@/modules/db-metadata/components/DBMetadataSectionTabs'
import { listDBObjectSnapshots } from '@/modules/db-metadata/api'
import { cn } from '@/lib/utils'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import type { DBObjectSnapshot } from '@/shared/types/dbMetadata'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { PageIntro } from '@/shared/ui/PageIntro'
import { Pagination } from '@/shared/ui/Pagination'

const PAGE_SIZE = 20

const OBJECT_COLUMNS = [
  { key: 'connection', label: 'Connection' },
  { key: 'engine', label: 'Engine' },
  { key: 'clusterNode', label: 'Cluster / Node' },
  { key: 'databaseSchema', label: 'Database / Schema' },
  { key: 'table', label: 'Table' },
  { key: 'dataSize', label: 'Data Size' },
  { key: 'indexSize', label: 'Index Size' },
  { key: 'snapshotTime', label: 'Snapshot Time' },
] as const

type ObjectColumnKey = (typeof OBJECT_COLUMNS)[number]['key']

const DEFAULT_VISIBLE_COLUMNS: ObjectColumnKey[] = OBJECT_COLUMNS.map((column) => column.key)

export function DBMetadataObjectsPage() {
  const [items, setItems] = useState<DBObjectSnapshot[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [offset, setOffset] = useState(0)
  const [engineFilter, setEngineFilter] = useState('all')
  const [connectionFilter, setConnectionFilter] = useState('all')
  const [keyword, setKeyword] = useState('')
  const [visibleColumns, setVisibleColumns] = useState<ObjectColumnKey[]>(DEFAULT_VISIBLE_COLUMNS)
  const [columnMenuOpen, setColumnMenuOpen] = useState(false)
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

  const connectionOptions = useMemo(() => {
    const names = Array.from(new Set(items.map((item) => item.connection_name).filter(Boolean))).sort((a, b) => a.localeCompare(b))
    return ['all', ...names]
  }, [items])

  const filteredItems = useMemo(() => {
    const loweredKeyword = keyword.trim().toLowerCase()
    return items.filter((item) => {
      if (engineFilter !== 'all' && item.engine !== engineFilter) {
        return false
      }
      if (connectionFilter !== 'all' && item.connection_name !== connectionFilter) {
        return false
      }
      if (loweredKeyword === '') {
        return true
      }

      return [
        item.connection_name,
        item.engine,
        item.cluster_name ?? '',
        item.node_name ?? '',
        item.database_name,
        item.schema_name,
        item.table_name,
      ].some((value) => value.toLowerCase().includes(loweredKeyword))
    })
  }, [connectionFilter, engineFilter, items, keyword])

  const pagedItems = filteredItems.slice(offset, offset + PAGE_SIZE)
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
              options={connectionOptions.map((option) => ({
                value: option,
                label: option === 'all' ? 'All Connections' : option,
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
                    {visibleColumns.includes('clusterNode') ? <th className="px-3 py-3">Cluster / Node</th> : null}
                    {visibleColumns.includes('databaseSchema') ? <th className="px-3 py-3">Database / Schema</th> : null}
                    {visibleColumns.includes('table') ? <th className="px-3 py-3">Table</th> : null}
                    {visibleColumns.includes('dataSize') ? <th className="px-3 py-3">Data Size</th> : null}
                    {visibleColumns.includes('indexSize') ? <th className="px-3 py-3">Index Size</th> : null}
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
                      {visibleColumns.includes('clusterNode') ? (
                        <td className="px-3 py-2.5 align-top text-[12px]">
                          {(item.cluster_name ?? '-') + ' / ' + (item.node_name ?? '-')}
                        </td>
                      ) : null}
                      {visibleColumns.includes('databaseSchema') ? (
                        <td className="px-3 py-2.5 align-top text-[12px]">
                          {item.database_name} / {item.schema_name}
                        </td>
                      ) : null}
                      {visibleColumns.includes('table') ? (
                        <td className="px-3 py-2.5 align-top font-mono text-[12px]">{item.table_name}</td>
                      ) : null}
                      {visibleColumns.includes('dataSize') ? (
                        <td className="px-3 py-2.5 align-top text-[12px]">{item.data_size_bytes.toLocaleString()}</td>
                      ) : null}
                      {visibleColumns.includes('indexSize') ? (
                        <td className="px-3 py-2.5 align-top text-[12px]">{item.index_size_bytes.toLocaleString()}</td>
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
              total={filteredItems.length}
              onChange={setOffset}
            />
          </div>
        )}
      </section>
    </div>
  )
}
