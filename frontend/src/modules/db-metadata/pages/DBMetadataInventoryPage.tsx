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
  { key: 'clusterEndpoint', label: 'Cluster Writer' },
  { key: 'clusterReaderEndpoint', label: 'Cluster Reader' },
  { key: 'instanceEndpoint', label: 'Instance Endpoint' },
  { key: 'tags', label: 'Tags' },
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
  const [endpointKeyword, setEndpointKeyword] = useState('')
  const [versionKeyword, setVersionKeyword] = useState('')
  const [sizeKeyword, setSizeKeyword] = useState('')
  const [tagsKeyword, setTagsKeyword] = useState('')
  const [mappingFilter, setMappingFilter] = useState('all')
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

  const mappingOptions = useMemo(() => {
    return ['all', ...new Set(items.map((item) => item.mapping_status ?? '').filter(Boolean))] as string[]
  }, [items])

  const filteredItems = useMemo(() => {
    const endpointNeedle = endpointKeyword.trim().toLowerCase()
    const versionNeedle = versionKeyword.trim().toLowerCase()
    const sizeNeedle = sizeKeyword.trim().toLowerCase()
    const tagsNeedle = tagsKeyword.trim().toLowerCase()
    return items.filter((item) => {
      if (engineFilter !== 'all' && item.engine !== engineFilter) {
        return false
      }
      if (roleFilter !== 'all' && (item.role ?? '') !== roleFilter) {
        return false
      }
      if (mappingFilter !== 'all' && item.mapping_status !== mappingFilter) {
        return false
      }
      if (endpointNeedle !== '' && !inventoryEndpointSearchValues(item).some((value) => value.toLowerCase().includes(endpointNeedle))) {
        return false
      }
      if (versionNeedle !== '' && !(item.engine_version ?? '').toLowerCase().includes(versionNeedle)) {
        return false
      }
      if (sizeNeedle !== '' && !(item.instance_class ?? '').toLowerCase().includes(sizeNeedle)) {
        return false
      }
      if (tagsNeedle !== '' && !inventoryTagSearchValues(item).some((value) => value.toLowerCase().includes(tagsNeedle))) {
        return false
      }
      return true
    })
  }, [endpointKeyword, engineFilter, items, mappingFilter, roleFilter, sizeKeyword, tagsKeyword, versionKeyword])

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
        <div className="grid gap-3 xl:grid-cols-[minmax(320px,1.2fr)_minmax(160px,0.55fr)_minmax(160px,0.55fr)_minmax(160px,0.55fr)_170px_170px_170px_auto]">
          <label className="block">
            <div className="flex h-11 items-center gap-2 rounded-2xl border border-border bg-white px-3 shadow-soft transition focus-within:border-slate-400">
              <Search className="h-4 w-4 text-faint" />
              <input
                aria-label="Inventory endpoint search"
                value={endpointKeyword}
                onChange={(event) => {
                  setEndpointKeyword(event.target.value)
                  setOffset(0)
                }}
                placeholder="Search identifier / writer / reader / instance endpoint"
                className="h-full w-full bg-transparent text-[13px] text-ink outline-none placeholder:text-muted"
              />
            </div>
          </label>

          <label className="block">
            <div className="flex h-11 items-center gap-2 rounded-2xl border border-border bg-white px-3 shadow-soft transition focus-within:border-slate-400">
              <Search className="h-4 w-4 text-faint" />
              <input
                aria-label="Version Search"
                value={versionKeyword}
                onChange={(event) => {
                  setVersionKeyword(event.target.value)
                  setOffset(0)
                }}
                placeholder="Version"
                className="h-full w-full bg-transparent text-[13px] text-ink outline-none placeholder:text-muted"
              />
            </div>
          </label>

          <label className="block">
            <div className="flex h-11 items-center gap-2 rounded-2xl border border-border bg-white px-3 shadow-soft transition focus-within:border-slate-400">
              <Search className="h-4 w-4 text-faint" />
              <input
                aria-label="Size Search"
                value={sizeKeyword}
                onChange={(event) => {
                  setSizeKeyword(event.target.value)
                  setOffset(0)
                }}
                placeholder="Size"
                className="h-full w-full bg-transparent text-[13px] text-ink outline-none placeholder:text-muted"
              />
            </div>
          </label>

          <label className="block">
            <div className="flex h-11 items-center gap-2 rounded-2xl border border-border bg-white px-3 shadow-soft transition focus-within:border-slate-400">
              <Search className="h-4 w-4 text-faint" />
              <input
                aria-label="Tags Search"
                value={tagsKeyword}
                onChange={(event) => {
                  setTagsKeyword(event.target.value)
                  setOffset(0)
                }}
                placeholder="Tags"
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

          <div>
            <DropdownSelect
              ariaLabel="Mapping"
              value={mappingFilter}
              onChange={(value) => {
                setMappingFilter(value)
                setOffset(0)
              }}
              options={mappingOptions.map((option) => ({
                value: option,
                label: option === 'all' ? 'All Mapping' : option,
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
            <table className="min-w-[1780px] border-collapse">
              <thead className="bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                <tr>
                  {visibleColumns.includes('identifier') ? <th className="w-[240px] px-3 py-3">Identifier</th> : null}
                  {visibleColumns.includes('engine') ? <th className="w-[150px] px-3 py-3">Engine</th> : null}
                  {visibleColumns.includes('version') ? <th className="w-[130px] px-3 py-3">Version</th> : null}
                  {visibleColumns.includes('regionAz') ? <th className="w-[150px] px-3 py-3">Region / AZ</th> : null}
                  {visibleColumns.includes('role') ? <th className="w-[90px] px-3 py-3">Role</th> : null}
                  {visibleColumns.includes('size') ? <th className="w-[130px] px-3 py-3">Size</th> : null}
                  {visibleColumns.includes('clusterEndpoint') ? <th className="w-[260px] px-3 py-3">Cluster Writer</th> : null}
                  {visibleColumns.includes('clusterReaderEndpoint') ? <th className="w-[260px] px-3 py-3">Cluster Reader</th> : null}
                  {visibleColumns.includes('instanceEndpoint') ? <th className="w-[300px] px-3 py-3">Instance Endpoint</th> : null}
                  {visibleColumns.includes('tags') ? <th className="w-[240px] px-3 py-3">Tags</th> : null}
                  {visibleColumns.includes('mapping') ? <th className="w-[120px] px-3 py-3">Mapping</th> : null}
                  {visibleColumns.includes('lastSynced') ? <th className="w-[120px] px-3 py-3">Last Synced</th> : null}
                </tr>
              </thead>
              <tbody>
                {pagedItems.map((item) => (
                  <tr key={item.id} className="border-t border-border text-sm text-ink hover:bg-slate-50/70">
                    {visibleColumns.includes('identifier') ? (
                      <td className="w-[240px] px-3 py-2 align-middle">
                        <SingleLineValue value={item.db_identifier} className="font-semibold text-ink" />
                      </td>
                    ) : null}
                    {visibleColumns.includes('engine') ? <td className="w-[150px] px-3 py-2 align-middle whitespace-nowrap text-[12px]">{item.engine}</td> : null}
                    {visibleColumns.includes('version') ? <td className="w-[130px] px-3 py-2 align-middle text-[12px]">{item.engine_version ?? '-'}</td> : null}
                    {visibleColumns.includes('regionAz') ? (
                      <td className="w-[150px] max-w-[150px] px-3 py-2 align-middle text-[12px]">
                        <ExpandableValue value={formatRegionAz(item.region, item.az)} />
                      </td>
                    ) : null}
                    {visibleColumns.includes('role') ? <td className="w-[90px] px-3 py-2 align-middle text-[12px]">{item.role ?? '-'}</td> : null}
                    {visibleColumns.includes('size') ? <td className="w-[130px] px-3 py-2 align-middle text-[12px]">{item.instance_class ?? '-'}</td> : null}
                    {visibleColumns.includes('clusterEndpoint') ? (
                      <td className="w-[260px] max-w-[260px] px-3 py-2 align-middle">
                        <ExpandableValue value={item.cluster_endpoint} className="font-mono text-[11px] text-muted" />
                      </td>
                    ) : null}
                    {visibleColumns.includes('clusterReaderEndpoint') ? (
                      <td className="w-[260px] max-w-[260px] px-3 py-2 align-middle">
                        <ExpandableValue value={item.cluster_reader_endpoint} className="font-mono text-[11px] text-muted" />
                      </td>
                    ) : null}
                    {visibleColumns.includes('instanceEndpoint') ? (
                      <td className="w-[300px] max-w-[300px] px-3 py-2 align-middle">
                        <ExpandableValue value={item.instance_endpoint} className="font-mono text-[11px] text-muted" />
                      </td>
                    ) : null}
                    {visibleColumns.includes('tags') ? (
                      <td className="w-[240px] max-w-[240px] px-3 py-2 align-middle">
                        <InventoryTags tags={item.tags} />
                      </td>
                    ) : null}
                    {visibleColumns.includes('mapping') ? (
                      <td className="w-[120px] px-3 py-2 align-middle text-[12px]">
                        <p className="font-semibold">{item.mapping_status}</p>
                        {item.mapping_status === 'ambiguous' ? <p className="mt-1 text-muted">{item.mapping_connections?.join(', ') || '-'}</p> : null}
                      </td>
                    ) : null}
                    {visibleColumns.includes('lastSynced') ? (
                      <td className="w-[120px] px-3 py-2 align-middle whitespace-nowrap text-[12px] text-muted">{formatDateTime(item.snapshot_at)}</td>
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

function inventoryEndpointSearchValues(item: InventorySnapshot) {
  return [
    item.db_identifier,
    item.cluster_endpoint ?? '',
    item.cluster_reader_endpoint ?? '',
    item.instance_endpoint ?? '',
  ]
}

function inventoryTagSearchValues(item: InventorySnapshot) {
  return Object.entries(item.tags ?? {}).map(([key, value]) => `${key}:${value}`)
}

function formatRegionAz(region: string, az?: string | null) {
  if (!az) {
    return region
  }
  const displayAz = az.startsWith(region) ? az.slice(region.length) : az
  return `${region} / ${displayAz || az}`
}

function SingleLineValue({ value, className }: { value?: string | null; className?: string }) {
  const displayValue = value || '-'
  return (
    <span title={displayValue} className={cn('block max-w-full truncate whitespace-nowrap text-[12px]', className)}>
      {displayValue}
    </span>
  )
}

function ExpandableValue({ value, className }: { value?: string | null; className?: string }) {
  const [expanded, setExpanded] = useState(false)
  const displayValue = value || '-'
  if (!value) {
    return <span className={cn('block max-w-full truncate whitespace-nowrap text-[12px]', className)}>-</span>
  }
  return (
    <button
      type="button"
      aria-expanded={expanded}
      onClick={() => setExpanded((current) => !current)}
      className={cn(
        'block max-w-full bg-transparent p-0 text-left text-[12px] outline-none transition hover:text-ink focus-visible:rounded focus-visible:ring-2 focus-visible:ring-slate-300',
        expanded ? 'whitespace-normal break-all' : 'truncate whitespace-nowrap',
        className,
      )}
    >
      {displayValue}
    </button>
  )
}

function InventoryTags({ tags }: { tags?: Record<string, string> }) {
  const [expanded, setExpanded] = useState(false)
  const entries = Object.entries(tags ?? {}).sort(([left], [right]) => left.localeCompare(right))
  if (entries.length === 0) {
    return <span className="text-[12px] text-muted">-</span>
  }
  const fullText = entries.map(([key, value]) => `${key}=${value || '-'}`).join(', ')
  return (
    <button
      type="button"
      aria-expanded={expanded}
      onClick={() => setExpanded((current) => !current)}
      className={cn(
        'block max-w-full bg-transparent p-0 text-left text-[12px] text-muted outline-none transition hover:text-ink focus-visible:rounded focus-visible:ring-2 focus-visible:ring-slate-300',
        expanded ? 'whitespace-normal break-words' : 'truncate whitespace-nowrap',
      )}
    >
      {fullText}
    </button>
  )
}
