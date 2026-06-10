import { useEffect, useState } from 'react'
import { Search } from 'lucide-react'
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
    <div className="flex h-full flex-col gap-3 p-3 sm:p-4">
      <section className="rounded-[22px] border border-white/85 bg-[rgba(248,250,252,0.82)] shadow-soft">
        <div className="border-b border-border/80 px-4 py-3 sm:px-5">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
            <div className="max-w-3xl">
              <div className="flex flex-wrap items-center gap-2 text-[11px] font-semibold text-muted">
                <span className="rounded-full border border-border bg-white px-2.5 py-1 text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                  Audit
                </span>
                <span>/</span>
                <span>Event Timeline</span>
              </div>
              <h2 className="mt-3 text-[24px] font-bold tracking-[-0.03em] text-ink">稽核日誌</h2>
              <p className="mt-2 text-[13px] leading-6 text-muted">
                查看登入、工單、匯出與設定變更等操作紀錄。所有篩選欄位都會直接對應後端 query 參數。
              </p>
            </div>

            <div className="rounded-[14px] border border-border bg-white px-3 py-2.5 text-[12px] text-muted shadow-soft">
              <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">Total Events</p>
              <p className="mt-1 text-[20px] font-bold tracking-tight text-ink">{total}</p>
            </div>
          </div>
        </div>

        <form className="grid gap-3 px-4 py-3 sm:px-5 lg:grid-cols-3" onSubmit={handleSubmit}>
          <input
            value={filters.actionType}
            onChange={(event) => setFilters((current) => ({ ...current, actionType: event.target.value }))}
            className="h-10 rounded-[12px] border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
            placeholder="action_type"
          />
          <input
            value={filters.actorId}
            onChange={(event) => setFilters((current) => ({ ...current, actorId: event.target.value }))}
            className="h-10 rounded-[12px] border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
            placeholder="actor_id"
          />
          <input
            value={filters.resourceType}
            onChange={(event) => setFilters((current) => ({ ...current, resourceType: event.target.value }))}
            className="h-10 rounded-[12px] border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
            placeholder="resource_type"
          />
          <input
            value={filters.resourceId}
            onChange={(event) => setFilters((current) => ({ ...current, resourceId: event.target.value }))}
            className="h-10 rounded-[12px] border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
            placeholder="resource_id"
          />
          <input
            value={filters.from}
            onChange={(event) => setFilters((current) => ({ ...current, from: event.target.value }))}
            className="h-10 rounded-[12px] border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
            placeholder="from (RFC3339)"
          />
          <input
            value={filters.to}
            onChange={(event) => setFilters((current) => ({ ...current, to: event.target.value }))}
            className="h-10 rounded-[12px] border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
            placeholder="to (RFC3339)"
          />

          <div className="lg:col-span-3">
            <button
              type="submit"
              className="inline-flex h-10 items-center justify-center gap-2 rounded-[12px] bg-brand px-4 text-[13px] font-bold text-white transition hover:bg-slate-800"
            >
              <Search className="h-4 w-4" />
              套用篩選
            </button>
          </div>
        </form>
      </section>

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      <section className="overflow-hidden rounded-[22px] border border-white/85 bg-white/92 shadow-soft">
        {loading ? (
          <LoadingBlock message="載入稽核日誌中…" className="h-60 rounded-none border-0" />
        ) : logs.length === 0 ? (
          <div className="flex h-60 items-center justify-center text-sm text-muted">目前沒有符合條件的稽核紀錄。</div>
        ) : (
          <div className="overflow-x-auto">
            <table className="min-w-full border-collapse">
              <thead className="bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
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
                  <tr key={log.id} className="border-t border-border align-top text-sm text-ink hover:bg-slate-50/70">
                    <td className="px-3 py-3 text-[12px] text-muted">{formatDateTime(log.created_at, true)}</td>
                    <td className="px-3 py-3">
                      <p className="text-[13px] font-semibold">{log.actor_name || 'system'}</p>
                      <p className="mt-1 font-mono text-[12px] text-muted">{log.actor_id ?? '—'}</p>
                    </td>
                    <td className="px-3 py-3 font-mono text-[12px] text-ink">{log.action_type}</td>
                    <td className="px-3 py-3 text-[12px] text-muted">
                      {log.resource_type || '—'}
                      {log.resource_id ? ` / ${log.resource_id}` : ''}
                    </td>
                    <td className="px-3 py-3 font-mono text-[12px] text-muted">{log.ip_address || '—'}</td>
                    <td className="px-3 py-3">
                      <pre className="max-w-[340px] overflow-x-auto rounded-[10px] bg-panel-soft px-2 py-2 text-[12px] text-muted">
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

      <div className="flex items-center justify-between px-1">
        <p className="text-[12px] text-muted">
          共 {total} 筆，目前顯示 {offset + 1} - {Math.min(offset + PAGE_SIZE, total)}
        </p>
        <div className="flex gap-2">
          <button
            type="button"
            disabled={offset === 0}
            onClick={() => setOffset((current) => Math.max(0, current - PAGE_SIZE))}
            className="inline-flex h-9 items-center justify-center rounded-[10px] border border-border bg-panel px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:opacity-50"
          >
            上一頁
          </button>
          <button
            type="button"
            disabled={offset + PAGE_SIZE >= total}
            onClick={() => setOffset((current) => current + PAGE_SIZE)}
            className="inline-flex h-9 items-center justify-center rounded-[10px] border border-border bg-panel px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:opacity-50"
          >
            下一頁
          </button>
        </div>
      </div>
    </div>
  )
}
