import { useEffect, useMemo, useRef, useState } from 'react'
import { Check, SlidersHorizontal } from 'lucide-react'
import { DBMetadataSectionTabs } from '@/modules/db-metadata/components/DBMetadataSectionTabs'
import { listInventorySnapshots } from '@/modules/db-metadata/api'
import { cn } from '@/lib/utils'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import type { InventorySnapshot } from '@/shared/types/dbMetadata'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { Pagination } from '@/shared/ui/Pagination'
import { SearchInput } from '@/shared/ui/SearchInput'
import {
  DataTable,
  DataTableBody,
  DataTableCell,
  DataTableContent,
  DataTableHead,
  DataTableHeaderCell,
  DataTableRow,
  DataTableScroll,
  DataTableSurface,
} from '@/shared/ui/DataTable'

const PAGE_SIZE = 20

const INVENTORY_COLUMNS = [
  { key: 'identifier', label: 'Cluster' },
  { key: 'instance', label: 'Instance' },
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
      <DBMetadataSectionTabs />

      <section>
        <div className="grid gap-3 xl:grid-cols-[minmax(320px,1.2fr)_minmax(160px,0.55fr)_minmax(160px,0.55fr)_minmax(160px,0.55fr)_170px_170px_170px_auto]">
          <label className="block">
            <SearchInput
              aria-label="Inventory endpoint search"
              value={endpointKeyword}
              onChange={(event) => {
                setEndpointKeyword(event.target.value)
                setOffset(0)
              }}
              placeholder="Search cluster / instance / writer / reader / endpoint"
            />
          </label>

          <label className="block">
            <SearchInput
              aria-label="Version Search"
              value={versionKeyword}
              onChange={(event) => {
                setVersionKeyword(event.target.value)
                setOffset(0)
              }}
              placeholder="Version"
            />
          </label>

          <label className="block">
            <SearchInput
              aria-label="Size Search"
              value={sizeKeyword}
              onChange={(event) => {
                setSizeKeyword(event.target.value)
                setOffset(0)
              }}
              placeholder="Size"
            />
          </label>

          <label className="block">
            <SearchInput
              aria-label="Tags Search"
              value={tagsKeyword}
              onChange={(event) => {
                setTagsKeyword(event.target.value)
                setOffset(0)
              }}
              placeholder="Tags"
            />
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
                  'inline-flex h-9 w-9 items-center justify-center rounded-xl border border-border bg-white text-ink shadow-soft transition',
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

      <DataTableSurface>
        {loading ? (
          <LoadingBlock message="Loading inventory..." className="min-h-[320px] rounded-none border-0 bg-transparent" />
        ) : filteredItems.length === 0 ? (
          <div className="m-4 flex min-h-[240px] items-center justify-center rounded-xl border border-dashed border-border bg-panel-soft text-sm text-muted">
            No inventory snapshots match the current filters.
          </div>
        ) : (
          <DataTableContent>
            <DataTableScroll>
              <DataTable className="min-w-[1920px]">
                <DataTableHead>
                  <tr>
                    {visibleColumns.includes('identifier') ? <DataTableHeaderCell className="w-[240px]">Cluster</DataTableHeaderCell> : null}
                    {visibleColumns.includes('instance') ? <DataTableHeaderCell className="w-[240px]">Instance</DataTableHeaderCell> : null}
                    {visibleColumns.includes('engine') ? <DataTableHeaderCell className="w-[150px]">Engine</DataTableHeaderCell> : null}
                    {visibleColumns.includes('version') ? <DataTableHeaderCell className="w-[130px]">Version</DataTableHeaderCell> : null}
                    {visibleColumns.includes('regionAz') ? <DataTableHeaderCell className="w-[150px]">Region / AZ</DataTableHeaderCell> : null}
                    {visibleColumns.includes('role') ? <DataTableHeaderCell className="w-[90px]">Role</DataTableHeaderCell> : null}
                    {visibleColumns.includes('size') ? <DataTableHeaderCell className="w-[130px]">Size</DataTableHeaderCell> : null}
                    {visibleColumns.includes('clusterEndpoint') ? <DataTableHeaderCell className="w-[260px]">Cluster Writer</DataTableHeaderCell> : null}
                    {visibleColumns.includes('clusterReaderEndpoint') ? <DataTableHeaderCell className="w-[260px]">Cluster Reader</DataTableHeaderCell> : null}
                    {visibleColumns.includes('instanceEndpoint') ? <DataTableHeaderCell className="w-[300px]">Instance Endpoint</DataTableHeaderCell> : null}
                    {visibleColumns.includes('tags') ? <DataTableHeaderCell className="w-[240px]">Tags</DataTableHeaderCell> : null}
                    {visibleColumns.includes('mapping') ? <DataTableHeaderCell className="w-[120px]">Mapping</DataTableHeaderCell> : null}
                    {visibleColumns.includes('lastSynced') ? <DataTableHeaderCell className="w-[120px]">Last Synced</DataTableHeaderCell> : null}
                  </tr>
                </DataTableHead>
                <DataTableBody>
                  {pagedItems.map((item) => (
                    <DataTableRow key={item.id}>
                      {visibleColumns.includes('identifier') ? (
                        <DataTableCell className="w-[240px]">
                          <SingleLineValue value={inventoryClusterName(item)} />
                        </DataTableCell>
                      ) : null}
                      {visibleColumns.includes('instance') ? (
                        <DataTableCell className="w-[240px]">
                          <SingleLineValue value={inventoryInstanceName(item)} />
                        </DataTableCell>
                      ) : null}
                      {visibleColumns.includes('engine') ? <DataTableCell className="w-[150px] whitespace-nowrap">{item.engine}</DataTableCell> : null}
                      {visibleColumns.includes('version') ? <DataTableCell className="w-[130px]">{item.engine_version ?? '-'}</DataTableCell> : null}
                      {visibleColumns.includes('regionAz') ? (
                        <DataTableCell className="w-[150px] max-w-[150px]">
                          <ExpandableValue value={formatRegionAz(item.region, item.az)} />
                        </DataTableCell>
                      ) : null}
                      {visibleColumns.includes('role') ? <DataTableCell className="w-[90px]">{item.role ?? '-'}</DataTableCell> : null}
                      {visibleColumns.includes('size') ? <DataTableCell className="w-[130px]">{item.instance_class ?? '-'}</DataTableCell> : null}
                      {visibleColumns.includes('clusterEndpoint') ? (
                        <DataTableCell className="w-[260px] max-w-[260px]">
                          <ExpandableValue value={item.cluster_endpoint} />
                        </DataTableCell>
                      ) : null}
                      {visibleColumns.includes('clusterReaderEndpoint') ? (
                        <DataTableCell className="w-[260px] max-w-[260px]">
                          <ExpandableValue value={item.cluster_reader_endpoint} />
                        </DataTableCell>
                      ) : null}
                      {visibleColumns.includes('instanceEndpoint') ? (
                        <DataTableCell className="w-[300px] max-w-[300px]">
                          <ExpandableValue value={item.instance_endpoint} />
                        </DataTableCell>
                      ) : null}
                      {visibleColumns.includes('tags') ? (
                        <DataTableCell className="w-[240px] max-w-[240px]">
                          <InventoryTags tags={item.tags} />
                        </DataTableCell>
                      ) : null}
                      {visibleColumns.includes('mapping') ? (
                        <DataTableCell className="w-[120px]">
                          <p>{item.mapping_status}</p>
                          {item.mapping_status === 'ambiguous' ? <p className="mt-1">{item.mapping_connections?.join(', ') || '-'}</p> : null}
                        </DataTableCell>
                      ) : null}
                      {visibleColumns.includes('lastSynced') ? (
                        <DataTableCell className="w-[120px] whitespace-nowrap">{formatDateTime(item.snapshot_at)}</DataTableCell>
                      ) : null}
                    </DataTableRow>
                  ))}
                </DataTableBody>
              </DataTable>
            </DataTableScroll>
            <Pagination
              offset={offset}
              pageSize={PAGE_SIZE}
              count={pagedItems.length}
              total={filteredItems.length}
              onChange={setOffset}
            />
          </DataTableContent>
        )}
      </DataTableSurface>
    </div>
  )
}

function inventoryEndpointSearchValues(item: InventorySnapshot) {
  return [
    inventoryClusterName(item),
    inventoryInstanceName(item),
    item.cluster_endpoint ?? '',
    item.cluster_reader_endpoint ?? '',
    item.instance_endpoint ?? '',
  ]
}

function inventoryClusterName(item: InventorySnapshot) {
  return item.cluster_identifier || item.db_identifier
}

function inventoryInstanceName(item: InventorySnapshot) {
  return item.instance_identifier || item.db_identifier
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
    return <span className="text-[12px] text-ink">-</span>
  }
  const fullText = entries.map(([key, value]) => `${key}=${value || '-'}`).join(', ')
  return (
    <button
      type="button"
      aria-expanded={expanded}
      onClick={() => setExpanded((current) => !current)}
      className={cn(
        'block max-w-full bg-transparent p-0 text-left text-[12px] text-ink outline-none transition hover:text-ink focus-visible:rounded focus-visible:ring-2 focus-visible:ring-slate-300',
        expanded ? 'whitespace-normal break-words' : 'truncate whitespace-nowrap',
      )}
    >
      {fullText}
    </button>
  )
}
