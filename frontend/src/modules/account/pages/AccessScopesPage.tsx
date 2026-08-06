import { useEffect, useMemo, useState } from 'react'
import { Database, KeyRound, Search } from 'lucide-react'
import { Link } from 'react-router-dom'
import { getAccountAccessScopes } from '@/modules/account/api'
import type { DashboardDBScope, DashboardQueryAccessScope } from '@/modules/dashboard/api'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import { DataTable, DataTableBody, DataTableCell, DataTableHead, DataTableHeaderCell, DataTableRow, DataTableScroll } from '@/shared/ui/DataTable'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { Pagination } from '@/shared/ui/Pagination'

const PAGE_SIZE = 20

function formatRemaining(scope: DashboardQueryAccessScope) {
  if (!scope.expires_at) {
    return 'Never'
  }
  if (scope.remaining_days == null) {
    return '-'
  }
  return scope.remaining_days === 0 ? 'Today' : `${scope.remaining_days}d`
}

function matchesDBScope(scope: DashboardDBScope, keyword: string) {
  const value = `${scope.name} ${scope.db_type}`.toLowerCase()
  return value.includes(keyword)
}

function matchesQueryScope(scope: DashboardQueryAccessScope, keyword: string) {
  const value = `${scope.connection_name} ${scope.database_pattern} ${scope.table_pattern} ${scope.effect} ${scope.subject_type} ${scope.source_ticket_no ?? ''}`.toLowerCase()
  return value.includes(keyword)
}

export function AccessScopesPage() {
  const [dbScopes, setDBScopes] = useState<DashboardDBScope[]>([])
  const [queryScopes, setQueryScopes] = useState<DashboardQueryAccessScope[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [keyword, setKeyword] = useState('')
  const [dbOffset, setDBOffset] = useState(0)
  const [queryOffset, setQueryOffset] = useState(0)

  useEffect(() => {
    let alive = true
    async function load() {
      setLoading(true)
      setError('')
      try {
        const response = await getAccountAccessScopes()
        if (!alive) {
          return
        }
        setDBScopes(response.db_scopes)
        setQueryScopes(response.query_access_scopes)
      } catch (loadError) {
        if (alive) {
          setError(loadError instanceof ApiError ? loadError.message : 'Failed to load access scopes.')
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

  const normalizedKeyword = keyword.trim().toLowerCase()
  const filteredDBScopes = useMemo(
    () => normalizedKeyword ? dbScopes.filter((scope) => matchesDBScope(scope, normalizedKeyword)) : dbScopes,
    [dbScopes, normalizedKeyword],
  )
  const filteredQueryScopes = useMemo(
    () => normalizedKeyword ? queryScopes.filter((scope) => matchesQueryScope(scope, normalizedKeyword)) : queryScopes,
    [queryScopes, normalizedKeyword],
  )
  const pagedDBScopes = filteredDBScopes.slice(dbOffset, dbOffset + PAGE_SIZE)
  const pagedQueryScopes = filteredQueryScopes.slice(queryOffset, queryOffset + PAGE_SIZE)

  useEffect(() => {
    setDBOffset(0)
    setQueryOffset(0)
  }, [normalizedKeyword])

  return (
    <div className="flex min-h-full flex-col gap-4 p-3 sm:p-4">
      {error ? <InlineAlert>{error}</InlineAlert> : null}

      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[20px] font-semibold tracking-normal text-ink">Access Scopes</h1>
          <p className="mt-1 text-[12px] text-muted">Your effective DB submission scope and active query access scope.</p>
        </div>
        <div className="relative w-full sm:w-[320px]">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted" />
          <input
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            placeholder="Search scopes..."
            className="h-9 w-full rounded-md border border-border bg-panel pl-9 pr-3 text-[12px] outline-none transition focus:border-accent"
          />
        </div>
      </div>

      {loading ? (
        <LoadingBlock message="Loading access scopes..." className="min-h-[360px] rounded-xl border-border bg-panel" />
      ) : (
        <>
          <div className="grid gap-3 md:grid-cols-2">
            <section className="rounded-xl border border-border bg-panel p-4 shadow-soft">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <p className="text-[12px] font-semibold text-muted">Submission DB Scope</p>
                  <p className="mt-2 text-[28px] font-semibold tracking-normal text-ink">{filteredDBScopes.length}</p>
                </div>
                <span className="inline-flex h-10 w-10 items-center justify-center rounded-lg bg-slate-100 text-slate-700">
                  <Database className="h-5 w-5" />
                </span>
              </div>
            </section>
            <section className="rounded-xl border border-border bg-panel p-4 shadow-soft">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <p className="text-[12px] font-semibold text-muted">Active Query Access</p>
                  <p className="mt-2 text-[28px] font-semibold tracking-normal text-ink">{filteredQueryScopes.length}</p>
                </div>
                <span className="inline-flex h-10 w-10 items-center justify-center rounded-lg bg-emerald-50 text-emerald-700">
                  <KeyRound className="h-5 w-5" />
                </span>
              </div>
            </section>
          </div>

          <section className="rounded-xl border border-border bg-panel shadow-soft">
            <div className="border-b border-border px-4 py-3">
              <h2 className="text-[13px] font-semibold text-ink">Submission DB Scope</h2>
            </div>
            {filteredDBScopes.length === 0 ? (
              <div className="px-4 py-6 text-[13px] text-muted">No DB submission scope found.</div>
            ) : (
              <>
                <DataTableScroll>
                  <DataTable>
                    <DataTableHead>
                      <tr>
                        <DataTableHeaderCell>Connection</DataTableHeaderCell>
                        <DataTableHeaderCell>Type</DataTableHeaderCell>
                      </tr>
                    </DataTableHead>
                    <DataTableBody>
                      {pagedDBScopes.map((scope) => (
                        <DataTableRow key={scope.id}>
                          <DataTableCell className="font-medium">{scope.name}</DataTableCell>
                          <DataTableCell className="uppercase text-muted">{scope.db_type}</DataTableCell>
                        </DataTableRow>
                      ))}
                    </DataTableBody>
                  </DataTable>
                </DataTableScroll>
                <Pagination total={filteredDBScopes.length} pageSize={PAGE_SIZE} offset={dbOffset} count={pagedDBScopes.length} onChange={setDBOffset} />
              </>
            )}
          </section>

          <section className="rounded-xl border border-border bg-panel shadow-soft">
            <div className="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
              <h2 className="text-[13px] font-semibold text-ink">Query Access Scope</h2>
              <Link to="/tickets/new?ticket_type=query_access" className="text-[12px] font-semibold text-muted hover:text-ink">Request access</Link>
            </div>
            {filteredQueryScopes.length === 0 ? (
              <div className="px-4 py-6 text-[13px] text-muted">No active query access scope found.</div>
            ) : (
              <>
                <DataTableScroll>
                  <DataTable>
                    <DataTableHead>
                      <tr>
                        <DataTableHeaderCell>Connection</DataTableHeaderCell>
                        <DataTableHeaderCell>Scope</DataTableHeaderCell>
                        <DataTableHeaderCell>Effect</DataTableHeaderCell>
                        <DataTableHeaderCell>Source</DataTableHeaderCell>
                        <DataTableHeaderCell>Expires</DataTableHeaderCell>
                        <DataTableHeaderCell className="text-right">Action</DataTableHeaderCell>
                      </tr>
                    </DataTableHead>
                    <DataTableBody>
                      {pagedQueryScopes.map((scope) => (
                        <DataTableRow key={scope.id} className={scope.expiring_soon ? 'bg-amber-50/40' : undefined}>
                          <DataTableCell className="font-medium">{scope.connection_name || `#${scope.connection_id}`}</DataTableCell>
                          <DataTableCell className="font-mono text-[12px]">{scope.database_pattern}.{scope.table_pattern}</DataTableCell>
                          <DataTableCell>
                            <span className={scope.effect === 'deny' ? 'text-danger' : 'text-success'}>{scope.effect}</span>
                          </DataTableCell>
                          <DataTableCell>{scope.source_ticket_no || scope.granted_via || scope.subject_type}</DataTableCell>
                          <DataTableCell className="whitespace-nowrap">
                            <span className={scope.expiring_soon ? 'font-semibold text-warning' : 'text-muted'}>
                              {formatRemaining(scope)}
                            </span>
                            {scope.expires_at ? <span className="ml-2 text-muted">{formatDateTime(scope.expires_at)}</span> : null}
                          </DataTableCell>
                          <DataTableCell className="whitespace-nowrap text-right">
                            <Link to={scope.renew_ticket_path} className="inline-flex h-8 items-center rounded-md border border-border bg-white px-2.5 text-[12px] font-semibold text-ink transition hover:bg-panel-soft">
                              Renew
                            </Link>
                          </DataTableCell>
                        </DataTableRow>
                      ))}
                    </DataTableBody>
                  </DataTable>
                </DataTableScroll>
                <Pagination total={filteredQueryScopes.length} pageSize={PAGE_SIZE} offset={queryOffset} count={pagedQueryScopes.length} onChange={setQueryOffset} />
              </>
            )}
          </section>
        </>
      )}
    </div>
  )
}
