import { useEffect, useMemo, useRef, useState } from 'react'
import { Check, Search, SlidersHorizontal } from 'lucide-react'
import { DBMetadataSectionTabs } from '@/modules/db-metadata/components/DBMetadataSectionTabs'
import { listInventorySnapshots } from '@/modules/db-metadata/api'
import { cn } from '@/lib/utils'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import type { InventorySnapshot } from '@/shared/types/dbMetadata'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { PageIntro } from '@/shared/ui/PageIntro'
import { Pagination } from '@/shared/ui/Pagination'

const PAGE_SIZE = 10

const INVENTORY_COLUMNS = [
  { key: 'identifier', label: 'Identifier' },
  { key: 'engine', label: 'Engine' },
  { key: 'version', label: 'Version' },
  { key: 'regionAz', label: 'Region / AZ' },
  { key: 'role', label: 'Role' },
  { key: 'size', label: 'Size' },
  { key: 'mapping', label: 'Mapping' },
  { key: 'lastSynced', label: 'Last Synced' },
] as const

type InventoryColumnKey = (typeof INVENTORY_COLUMNS)[number]['key']

const DEFAULT_VISIBLE_COLUMNS: InventoryColumnKey[] = INVENTORY_COLUMNS.map((column) => column.key)

export function DBMetadataInventoryPage() {
  const [items, setItems] = useState<InventorySnapshot[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [offset, setOffset] = useState(0)
  const [engineFilter, setEngineFilter] = useState('all')
  const [roleFilter, setRoleFilter] = useState('all')
  const [identifierKeyword, setIdentifierKeyword] = useState('')
  const [visibleColumns, setVisibleColumns] = useState<InventoryColumnKey[]>(DEFAULT_VISIBLE_COLUMNS)
  const [columnMenuOpen, setColumnMenuOpen] = useState(false)
  const columnMenuRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    let active = true

    async function bootstrap() {
      setLoading(true)
      setError('')
      try {
        const response = await listInventorySnapshots()
        if (active) {
          setItems(response.items)
        }
      } catch (loadError) {
        if (active) {
          setError(loadError instanceof ApiError ? loadError.message : 'Failed to load inventory snapshots.')
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
    return ['all', ...new Set(items.map((item) => item.engine).filter(Boolean))] as string[]
  }, [items])

  const roleOptions = useMemo(() => {
    return ['all', ...new Set(items.map((item) => item.role ?? '').filter(Boolean))] as string[]
  }, [items])

  const filteredItems = useMemo(() => {
    const keyword = identifierKeyword.trim().toLowerCase()
    return items.filter((item) => {
      if (engineFilter !== 'all' && item.engine !== engineFilter) {
        return false
      }
      if (roleFilter !== 'all' && (item.role ?? '') !== roleFilter) {
        return false
      }
      if (keyword !== '' && !item.db_identifier.toLowerCase().includes(keyword)) {
        return false
      }
      return true
    })
  }, [engineFilter, identifierKeyword, items, roleFilter])

  const pagedItems = filteredItems.slice(offset, offset + PAGE_SIZE)

  function toggleColumn(columnKey: InventoryColumnKey) {
    setVisibleColumns((current) => {
      if (current.includes(columnKey)) {
        if (current.length === 1) {
          return current
        }
        return current.filter((item) => item !== columnKey)
      }
      return INVENTORY_COLUMNS.map((column) => column.key).filter((key) => current.includes(key) || key === columnKey)
    })
  }

  return (
    <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
      <PageIntro
        title="Inventory"
        description="This view shows AWS inventory snapshots rather than real-time status. `Mapping` is determined only by exact matches between discovered endpoints and `DB Connection host` values."
      />

      <DBMetadataSectionTabs />

      <section>
        <div className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_180px_180px_auto]">
          <label className="block">
            <div className="flex h-11 items-center gap-2 rounded-2xl border border-border bg-white px-3 shadow-soft transition focus-within:border-slate-400">
              <Search className="h-4 w-4 text-faint" />
              <input
                aria-label="Identifier Search"
                value={identifierKeyword}
                onChange={(event) => {
                  setIdentifierKeyword(event.target.value)
                  setOffset(0)
                }}
                placeholder="Search DB identifier"
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
              ariaLabel="Role"
              value={roleFilter}
              onChange={(value) => {
                setRoleFilter(value)
                setOffset(0)
              }}
              options={roleOptions.map((option) => ({
                value: option,
                label: option === 'all' ? 'All Roles' : option,
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
                    {INVENTORY_COLUMNS.map((column) => {
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
          <LoadingBlock message="Loading inventory..." className="min-h-[320px] rounded-none border-0 bg-transparent" />
        ) : filteredItems.length === 0 ? (
          <div className="m-4 flex min-h-[240px] items-center justify-center rounded-xl border border-dashed border-border bg-panel-soft text-sm text-muted">
            No inventory snapshots match the current filters.
          </div>
        ) : (
          <div className="grid gap-3 p-3">
            <div className="overflow-x-auto">
            <table className="min-w-full border-collapse">
              <thead className="bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                <tr>
                  {visibleColumns.includes('identifier') ? <th className="px-3 py-3">Identifier</th> : null}
                  {visibleColumns.includes('engine') ? <th className="px-3 py-3">Engine</th> : null}
                  {visibleColumns.includes('version') ? <th className="px-3 py-3">Version</th> : null}
                  {visibleColumns.includes('regionAz') ? <th className="px-3 py-3">Region / AZ</th> : null}
                  {visibleColumns.includes('role') ? <th className="px-3 py-3">Role</th> : null}
                  {visibleColumns.includes('size') ? <th className="px-3 py-3">Size</th> : null}
                  {visibleColumns.includes('mapping') ? <th className="px-3 py-3">Mapping</th> : null}
                  {visibleColumns.includes('lastSynced') ? <th className="px-3 py-3">Last Synced</th> : null}
                </tr>
              </thead>
              <tbody>
                {pagedItems.map((item) => (
                  <tr key={item.id} className="border-t border-border text-sm text-ink hover:bg-slate-50/70">
                    {visibleColumns.includes('identifier') ? (
                      <td className="px-3 py-2.5 align-top">
                        <p className="font-semibold">{item.db_identifier}</p>
                        <p className="mt-1 break-all font-mono text-[11px] text-muted">{item.instance_endpoint ?? item.cluster_endpoint ?? '-'}</p>
                      </td>
                    ) : null}
                    {visibleColumns.includes('engine') ? <td className="px-3 py-2.5 align-top text-[12px]">{item.engine}</td> : null}
                    {visibleColumns.includes('version') ? <td className="px-3 py-2.5 align-top text-[12px]">{item.engine_version ?? '-'}</td> : null}
                    {visibleColumns.includes('regionAz') ? (
                      <td className="px-3 py-2.5 align-top text-[12px]">
                        {item.region}
                        {item.az ? ` / ${item.az}` : ''}
                      </td>
                    ) : null}
                    {visibleColumns.includes('role') ? <td className="px-3 py-2.5 align-top text-[12px]">{item.role ?? '-'}</td> : null}
                    {visibleColumns.includes('size') ? <td className="px-3 py-2.5 align-top text-[12px]">{item.instance_class ?? '-'}</td> : null}
                    {visibleColumns.includes('mapping') ? (
                      <td className="px-3 py-2.5 align-top text-[12px]">
                        <p className="font-semibold">{item.mapping_status}</p>
                        <p className="mt-1 text-muted">{item.mapping_connections?.join(', ') || '-'}</p>
                      </td>
                    ) : null}
                    {visibleColumns.includes('lastSynced') ? (
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
