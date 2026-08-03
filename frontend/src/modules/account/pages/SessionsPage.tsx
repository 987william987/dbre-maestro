import { useEffect, useMemo, useState } from 'react'
import { RefreshCw, ShieldCheck, Trash2 } from 'lucide-react'
import { listAccountSessions, revokeAccountSession, revokeAllAccountSessions, type AccountSession } from '@/modules/account/api'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { Pagination } from '@/shared/ui/Pagination'
import { useToast } from '@/shared/ui/ToastContext'
import {
  DataTable,
  DataTableBody,
  DataTableCell,
  DataTableHead,
  DataTableHeaderCell,
  DataTableRow,
  DataTableScroll,
} from '@/shared/ui/DataTable'

const SESSION_PAGE_SIZE = 20

export function SessionsPage() {
  const { pushToast } = useToast()
  const [sessions, setSessions] = useState<AccountSession[]>([])
  const [loading, setLoading] = useState(true)
  const [acting, setActing] = useState<number | 'all' | null>(null)
  const [error, setError] = useState('')
  const [sessionOffset, setSessionOffset] = useState(0)
  const sortedSessions = useMemo(() => [...sessions].sort(compareAccountSessions), [sessions])
  const pagedSessions = useMemo(() => sortedSessions.slice(sessionOffset, sessionOffset + SESSION_PAGE_SIZE), [sessionOffset, sortedSessions])

  async function loadSessions(options?: { background?: boolean }) {
    if (!options?.background) {
      setLoading(true)
    }
    setError('')
    try {
      const response = await listAccountSessions()
      setSessions(response.sessions)
      setSessionOffset((current) => Math.min(current, Math.max(0, Math.floor(Math.max(response.sessions.length - 1, 0) / SESSION_PAGE_SIZE) * SESSION_PAGE_SIZE)))
    } catch (loadError) {
      setError(loadError instanceof ApiError ? loadError.message : 'Failed to load sessions.')
    } finally {
      if (!options?.background) {
        setLoading(false)
      }
    }
  }

  useEffect(() => {
    void loadSessions()
  }, [])

  async function handleRevoke(sessionID: number) {
    setActing(sessionID)
    setError('')
    try {
      await revokeAccountSession(sessionID)
      await loadSessions({ background: true })
      pushToast('Session revoked.', 'success')
    } catch (revokeError) {
      setError(revokeError instanceof ApiError ? revokeError.message : 'Failed to revoke session.')
    } finally {
      setActing(null)
    }
  }

  async function handleRevokeAll() {
    setActing('all')
    setError('')
    try {
      await revokeAllAccountSessions()
      pushToast('All sessions revoked. Please sign in again.', 'success')
      window.location.assign('/login')
    } catch (revokeError) {
      setError(revokeError instanceof ApiError ? revokeError.message : 'Failed to revoke sessions.')
      setActing(null)
    }
  }

  return (
    <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
      {error ? <InlineAlert>{error}</InlineAlert> : null}

      {loading ? (
        <LoadingBlock message="Loading sessions..." className="min-h-[320px] rounded-xl border-border bg-panel" />
      ) : (
        <section className="rounded-xl border border-border bg-panel shadow-soft">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border/80 px-4 py-3">
            <div>
              <p className="text-[14px] font-semibold text-ink">Session Management</p>
              <p className="mt-1 text-[12px] leading-5 text-muted">A session represents a browser that can refresh your short-lived access token.</p>
            </div>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => void loadSessions({ background: true })}
                className="inline-flex h-9 items-center gap-2 rounded-md border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-panel-soft"
              >
                <RefreshCw className="h-4 w-4" />
                Refresh
              </button>
              <button
                type="button"
                disabled={acting !== null || sessions.length === 0}
                onClick={() => void handleRevokeAll()}
                className="inline-flex h-9 items-center gap-2 rounded-md border border-danger/20 bg-red-50 px-3 text-[12px] font-semibold text-danger transition hover:bg-red-100 disabled:cursor-not-allowed disabled:opacity-50"
              >
                <Trash2 className="h-4 w-4" />
                Revoke All
              </button>
            </div>
          </div>

          {sessions.length === 0 ? (
            <div className="px-4 py-6 text-[13px] text-muted">No sessions found.</div>
          ) : (
            <>
              <DataTableScroll>
                <DataTable>
                  <DataTableHead>
                    <tr>
                      <DataTableHeaderCell>Session</DataTableHeaderCell>
                      <DataTableHeaderCell>IP</DataTableHeaderCell>
                      <DataTableHeaderCell>Created</DataTableHeaderCell>
                      <DataTableHeaderCell>Expires</DataTableHeaderCell>
                      <DataTableHeaderCell>Status</DataTableHeaderCell>
                      <DataTableHeaderCell className="text-right">Actions</DataTableHeaderCell>
                    </tr>
                  </DataTableHead>
                  <DataTableBody>
                    {pagedSessions.map((session) => {
                      const revoked = session.revoked_at != null
                      return (
                        <DataTableRow key={session.id}>
                          <DataTableCell className="max-w-[460px]">
                            <div className="flex items-start gap-2">
                              <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0 text-accent" />
                              <div className="min-w-0">
                                <div className="flex flex-wrap items-center gap-2">
                                  <p>Session #{session.id}</p>
                                  {session.is_current ? (
                                    <span className="inline-flex rounded-full border border-accent/20 bg-accent/10 px-2 py-0.5 text-[10px] font-semibold text-accent">
                                      Current
                                    </span>
                                  ) : null}
                                </div>
                                <p className="mt-1 truncate text-[12px] text-muted" title={session.user_agent ?? ''}>
                                  {session.user_agent || 'Unknown user agent'}
                                </p>
                              </div>
                            </div>
                          </DataTableCell>
                          <DataTableCell className="whitespace-nowrap">{session.ip_address || '-'}</DataTableCell>
                          <DataTableCell className="whitespace-nowrap">{formatDateTime(session.created_at)}</DataTableCell>
                          <DataTableCell className="whitespace-nowrap">{formatDateTime(session.expires_at)}</DataTableCell>
                          <DataTableCell className="whitespace-nowrap">
                            <span className={`inline-flex rounded-full border px-2.5 py-1 text-[10px] font-semibold tracking-[0.04em] ${revoked ? 'border-slate-200 bg-slate-100 text-slate-700' : 'border-emerald-200 bg-emerald-50 text-emerald-700'}`}>
                              {revoked ? 'Revoked' : 'Active'}
                            </span>
                          </DataTableCell>
                          <DataTableCell className="whitespace-nowrap text-right">
                            <button
                              type="button"
                              disabled={acting !== null || revoked || session.is_current}
                              onClick={() => void handleRevoke(session.id)}
                              className="inline-flex h-8 items-center gap-2 rounded-md border border-border bg-white px-2.5 text-[12px] font-semibold text-ink transition hover:bg-panel-soft disabled:cursor-not-allowed disabled:opacity-50"
                            >
                              <Trash2 className="h-3.5 w-3.5" />
                              Revoke
                            </button>
                          </DataTableCell>
                        </DataTableRow>
                      )
                    })}
                  </DataTableBody>
                </DataTable>
              </DataTableScroll>
              <Pagination
                total={sessions.length}
                pageSize={SESSION_PAGE_SIZE}
                offset={sessionOffset}
                count={pagedSessions.length}
                onChange={setSessionOffset}
              />
            </>
          )}
        </section>
      )}
    </div>
  )
}

function compareAccountSessions(left: AccountSession, right: AccountSession) {
  if (left.is_current !== right.is_current) {
    return left.is_current ? -1 : 1
  }
  const leftActive = isActiveSession(left)
  const rightActive = isActiveSession(right)
  if (leftActive !== rightActive) {
    return leftActive ? -1 : 1
  }
  return new Date(right.created_at).getTime() - new Date(left.created_at).getTime()
}

function isActiveSession(session: AccountSession) {
  return session.revoked_at == null && new Date(session.expires_at).getTime() > Date.now()
}
