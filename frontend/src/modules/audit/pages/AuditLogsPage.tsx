import { useEffect, useState } from 'react'
import { Loader2, Search } from 'lucide-react'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import type { AuditLog } from '@/shared/types/audit'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { listAuditLogs } from '@/modules/audit/api'

const PAGE_SIZE = 50

export function AuditLogsPage() {
  const [logs, setLogs] = useState<AuditLog[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [filters, setFilters] = useState({
    actionType: '',
    actorId: '',
    resourceType: '',
    resourceId: '',
    from: '',
    to: '',
  })
  const [offset, setOffset] = useState(0)

  async function loadLogs(nextOffset: number) {
    setLoading(true)
    setError('')
    try {
      const response = await listAuditLogs({
        ...filters,
        offset: nextOffset,
        limit: PAGE_SIZE,
      })
      setLogs(response.logs)
      setTotal(response.total)
    } catch (loadError) {
      setError(loadError instanceof ApiError ? loadError.message : '讀取稽核日誌失敗。')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void loadLogs(offset)
  }, [offset])

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setOffset(0)
    await loadLogs(0)
  }

  return (
    <div className="flex h-full flex-col gap-6 p-5 sm:p-6">
      <div className="border-b border-border pb-5">
        <p className="text-[11px] font-bold uppercase tracking-[0.2em] text-faint">Audit Logs</p>
        <h2 className="mt-2 font-display text-2xl font-black tracking-tight text-ink">稽核日誌</h2>
        <p className="mt-1 text-sm text-muted">查看登入、工單、匯出與設定變更等操作紀錄。篩選條件會直接對應後端 query 參數。</p>
      </div>

      <form className="grid gap-4 rounded-card border border-border bg-panel p-5 lg:grid-cols-3" onSubmit={handleSubmit}>
        <input
          value={filters.actionType}
          onChange={(event) => setFilters((current) => ({ ...current, actionType: event.target.value }))}
          className="h-10 rounded-control border border-border bg-panel-soft px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
          placeholder="action_type"
        />
        <input
          value={filters.actorId}
          onChange={(event) => setFilters((current) => ({ ...current, actorId: event.target.value }))}
          className="h-10 rounded-control border border-border bg-panel-soft px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
          placeholder="actor_id"
        />
        <input
          value={filters.resourceType}
          onChange={(event) => setFilters((current) => ({ ...current, resourceType: event.target.value }))}
          className="h-10 rounded-control border border-border bg-panel-soft px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
          placeholder="resource_type"
        />
        <input
          value={filters.resourceId}
          onChange={(event) => setFilters((current) => ({ ...current, resourceId: event.target.value }))}
          className="h-10 rounded-control border border-border bg-panel-soft px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
          placeholder="resource_id"
        />
        <input
          value={filters.from}
          onChange={(event) => setFilters((current) => ({ ...current, from: event.target.value }))}
          className="h-10 rounded-control border border-border bg-panel-soft px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
          placeholder="from (RFC3339)"
        />
        <input
          value={filters.to}
          onChange={(event) => setFilters((current) => ({ ...current, to: event.target.value }))}
          className="h-10 rounded-control border border-border bg-panel-soft px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
          placeholder="to (RFC3339)"
        />

        <div className="lg:col-span-3">
          <button
            type="submit"
            className="inline-flex h-10 items-center justify-center gap-2 rounded-control bg-brand px-4 text-sm font-bold text-white transition hover:bg-slate-800"
          >
            <Search className="h-4 w-4" />
            套用篩選
          </button>
        </div>
      </form>

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      <section className="overflow-hidden rounded-card border border-border bg-panel">
        {loading ? (
          <LoadingBlock message="載入稽核日誌中…" className="h-60 rounded-none border-0" />
        ) : logs.length === 0 ? (
          <div className="flex h-60 items-center justify-center text-sm text-muted">目前沒有符合條件的稽核紀錄。</div>
        ) : (
          <div className="overflow-x-auto">
            <table className="min-w-full border-collapse">
              <thead className="bg-panel-soft text-left text-[11px] font-bold uppercase tracking-[0.16em] text-faint">
                <tr>
                  <th className="px-3 py-3">Created</th>
                  <th className="px-3 py-3">Actor</th>
                  <th className="px-3 py-3">Action</th>
                  <th className="px-3 py-3">Resource</th>
                  <th className="px-3 py-3">IP</th>
                  <th className="px-3 py-3">Details</th>
                </tr>
              </thead>
              <tbody>
                {logs.map((log) => (
                  <tr key={log.id} className="border-t border-border align-top text-sm text-ink">
                    <td className="px-3 py-3 text-xs text-muted">{formatDateTime(log.created_at, true)}</td>
                    <td className="px-3 py-3">
                      <p className="font-semibold">{log.actor_name || 'system'}</p>
                      <p className="mt-1 font-mono text-xs text-muted">{log.actor_id ?? '—'}</p>
                    </td>
                    <td className="px-3 py-3 font-mono text-xs text-ink">{log.action_type}</td>
                    <td className="px-3 py-3 text-xs text-muted">
                      {log.resource_type || '—'}
                      {log.resource_id ? ` / ${log.resource_id}` : ''}
                    </td>
                    <td className="px-3 py-3 font-mono text-xs text-muted">{log.ip_address || '—'}</td>
                    <td className="px-3 py-3">
                      <pre className="max-w-[340px] overflow-x-auto rounded-control bg-panel-soft px-2 py-2 text-xs text-muted">
                        {log.details ? JSON.stringify(log.details, null, 2) : '—'}
                      </pre>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <div className="flex items-center justify-between">
        <p className="text-xs text-muted">
          共 {total} 筆，目前顯示 {offset + 1} - {Math.min(offset + PAGE_SIZE, total)}
        </p>
        <div className="flex gap-2">
          <button
            type="button"
            disabled={offset === 0}
            onClick={() => setOffset((current) => Math.max(0, current - PAGE_SIZE))}
            className="inline-flex h-9 items-center justify-center rounded-control border border-border bg-panel px-3 text-xs font-semibold text-ink transition hover:bg-page disabled:opacity-50"
          >
            上一頁
          </button>
          <button
            type="button"
            disabled={offset + PAGE_SIZE >= total}
            onClick={() => setOffset((current) => current + PAGE_SIZE)}
            className="inline-flex h-9 items-center justify-center rounded-control border border-border bg-panel px-3 text-xs font-semibold text-ink transition hover:bg-page disabled:opacity-50"
          >
            下一頁
          </button>
        </div>
      </div>
    </div>
  )
}
