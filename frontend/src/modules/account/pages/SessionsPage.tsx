import { useEffect, useState } from 'react'
import { RefreshCw, ShieldCheck, Trash2 } from 'lucide-react'
import { listAccountSessions, revokeAccountSession, revokeAllAccountSessions, type AccountSession } from '@/modules/account/api'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { PageIntro } from '@/shared/ui/PageIntro'
import { useToast } from '@/shared/ui/ToastContext'

export function SessionsPage() {
  const { pushToast } = useToast()
  const [sessions, setSessions] = useState<AccountSession[]>([])
  const [loading, setLoading] = useState(true)
  const [acting, setActing] = useState<number | 'all' | null>(null)
  const [error, setError] = useState('')

  async function loadSessions(options?: { background?: boolean }) {
    if (!options?.background) {
      setLoading(true)
    }
    setError('')
    try {
      const response = await listAccountSessions()
      setSessions(response.sessions)
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
      <PageIntro
        title="Account Sessions"
        description="Review active refresh sessions for your account and revoke sessions you no longer recognize."
      />

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      {loading ? (
        <LoadingBlock message="Loading sessions..." className="min-h-[320px] rounded-xl border-border bg-panel" />
      ) : (
        <section className="rounded-xl border border-border bg-panel shadow-soft">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border/80 px-4 py-3">
            <div>
              <p className="text-[14px] font-semibold text-ink">Refresh Sessions</p>
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
            <div className="overflow-x-auto">
              <table className="min-w-full border-collapse text-left text-[13px]">
                <thead className="bg-panel-soft text-[11px] font-semibold uppercase tracking-[0.08em] text-faint">
                  <tr>
                    <th className="whitespace-nowrap px-4 py-3">Session</th>
                    <th className="whitespace-nowrap px-4 py-3">IP</th>
                    <th className="whitespace-nowrap px-4 py-3">Created</th>
                    <th className="whitespace-nowrap px-4 py-3">Expires</th>
                    <th className="whitespace-nowrap px-4 py-3">Status</th>
                    <th className="whitespace-nowrap px-4 py-3 text-right">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border bg-white">
                  {sessions.map((session) => {
                    const revoked = session.revoked_at != null
                    return (
                      <tr key={session.id} className="align-top text-ink">
                        <td className="max-w-[460px] px-4 py-3">
                          <div className="flex items-start gap-2">
                            <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0 text-accent" />
                            <div className="min-w-0">
                              <p className="font-semibold">Session #{session.id}</p>
                              <p className="mt-1 truncate text-[12px] text-muted" title={session.user_agent ?? ''}>
                                {session.user_agent || 'Unknown user agent'}
                              </p>
                            </div>
                          </div>
                        </td>
                        <td className="whitespace-nowrap px-4 py-3 text-muted">{session.ip_address || '-'}</td>
                        <td className="whitespace-nowrap px-4 py-3 text-muted">{formatDateTime(session.created_at)}</td>
                        <td className="whitespace-nowrap px-4 py-3 text-muted">{formatDateTime(session.expires_at)}</td>
                        <td className="whitespace-nowrap px-4 py-3">
                          <span className={`inline-flex rounded-full border px-2.5 py-1 text-[10px] font-semibold tracking-[0.04em] ${revoked ? 'border-slate-200 bg-slate-100 text-slate-700' : 'border-emerald-200 bg-emerald-50 text-emerald-700'}`}>
                            {revoked ? 'Revoked' : 'Active'}
                          </span>
                        </td>
                        <td className="whitespace-nowrap px-4 py-3 text-right">
                          <button
                            type="button"
                            disabled={acting !== null || revoked}
                            onClick={() => void handleRevoke(session.id)}
                            className="inline-flex h-8 items-center gap-2 rounded-md border border-border bg-white px-2.5 text-[12px] font-semibold text-ink transition hover:bg-panel-soft disabled:cursor-not-allowed disabled:opacity-50"
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                            Revoke
                          </button>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </section>
      )}
    </div>
  )
}
