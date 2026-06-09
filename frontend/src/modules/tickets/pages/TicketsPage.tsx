import { useEffect, useState } from 'react'
import { ArrowRight, RefreshCcw } from 'lucide-react'
import { Link } from 'react-router-dom'
import { EmptyState } from '@/components/tickets/EmptyState'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import type { Ticket, TicketStatus } from '@/shared/types/ticket'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { StatusBadge } from '@/shared/ui/StatusBadge'
import { listTickets } from '@/modules/tickets/api'

const STATUS_OPTIONS: Array<{ value: '' | TicketStatus; label: string }> = [
  { value: '', label: '全部狀態' },
  { value: 'pending_review', label: '待審核' },
  { value: 'approved', label: '已通過' },
  { value: 'rejected', label: '已拒絕' },
  { value: 'pending_execution', label: '待執行' },
  { value: 'executing', label: '執行中' },
  { value: 'completed', label: '已完成' },
  { value: 'failed', label: '失敗' },
  { value: 'stopped', label: '已停止' },
  { value: 'interrupted', label: '已中斷' },
]

export function TicketsPage() {
  const [tickets, setTickets] = useState<Ticket[]>([])
  const [status, setStatus] = useState<'' | TicketStatus>('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  async function loadTickets(nextStatus: '' | TicketStatus) {
    setLoading(true)
    setError('')

    try {
      const response = await listTickets(nextStatus || undefined)
      setTickets(response.tickets)
    } catch (loadError) {
      setError(loadError instanceof ApiError ? loadError.message : '讀取工單列表失敗，請稍後重試。')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void loadTickets(status)
  }, [status])

  return (
    <div className="flex h-full flex-col gap-6 p-5 sm:p-6">
      <div className="flex flex-col gap-2 border-b border-border pb-5">
        <p className="text-[11px] font-bold uppercase tracking-[0.2em] text-faint">Tickets</p>
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <h2 className="font-display text-2xl font-black tracking-tight text-ink">工單工作台</h2>
            <p className="mt-1 text-sm text-muted">
              這裡顯示目前使用者可見的工單。Developer 只會看到自己的工單；Reviewer / DBA / Admin 的可見範圍由後端授權決定。
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              onClick={() => void loadTickets(status)}
              className="inline-flex items-center gap-2 rounded-control border border-border bg-panel px-3 py-2 text-sm font-semibold text-ink transition hover:bg-page"
            >
              <RefreshCcw className="h-4 w-4" />
              重新整理
            </button>
            <Link
              to="/tickets/new"
              className="inline-flex items-center gap-2 rounded-control bg-brand px-4 py-2 text-sm font-bold text-white transition hover:bg-slate-800"
            >
              建立新工單
              <ArrowRight className="h-4 w-4" />
            </Link>
          </div>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <label className="flex items-center gap-2 text-sm text-muted">
          <span className="font-semibold text-ink">狀態篩選</span>
          <select
            value={status}
            onChange={(event) => setStatus(event.target.value as '' | TicketStatus)}
            className="h-10 rounded-control border border-border bg-panel px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
          >
            {STATUS_OPTIONS.map((option) => (
              <option key={option.value || 'all'} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </label>
      </div>

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      <div className="min-h-[360px] overflow-hidden rounded-card border border-border">
        {loading ? (
          <LoadingBlock message="載入工單列表中…" className="h-[360px] rounded-none border-0" />
        ) : tickets.length === 0 ? (
          <div className="bg-panel">
            <EmptyState variant={status ? 'search' : 'history'} />
          </div>
        ) : (
          <div className="overflow-x-auto bg-panel">
            <table className="min-w-full border-collapse">
              <thead className="bg-panel-soft">
                <tr className="text-left text-[11px] font-bold uppercase tracking-[0.16em] text-faint">
                  <th className="px-4 py-3">工單編號</th>
                  <th className="px-4 py-3">標題</th>
                  <th className="px-4 py-3">類型</th>
                  <th className="px-4 py-3">狀態</th>
                  <th className="px-4 py-3">提交者</th>
                  <th className="px-4 py-3">建立時間</th>
                </tr>
              </thead>
              <tbody>
                {tickets.map((ticket) => (
                  <tr key={ticket.id} className="border-t border-border text-sm text-ink">
                    <td className="px-4 py-3 align-top">
                      <Link to={`/tickets/${ticket.id}`} className="font-mono font-semibold text-accent hover:text-blue-700">
                        {ticket.ticket_no}
                      </Link>
                    </td>
                    <td className="px-4 py-3 align-top">
                      <div>
                        <p className="font-semibold text-ink">{ticket.title}</p>
                        {ticket.description ? <p className="mt-1 line-clamp-2 text-xs text-muted">{ticket.description}</p> : null}
                      </div>
                    </td>
                    <td className="px-4 py-3 align-top">
                      <span className="rounded-pill border border-border bg-panel-soft px-2 py-1 text-[11px] font-semibold uppercase tracking-wide text-muted">
                        {ticket.ticket_type}
                      </span>
                    </td>
                    <td className="px-4 py-3 align-top">
                      <StatusBadge status={ticket.status} />
                    </td>
                    <td className="px-4 py-3 align-top font-mono text-xs text-muted">{ticket.submitter_id}</td>
                    <td className="px-4 py-3 align-top text-xs text-muted">{formatDateTime(ticket.created_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}
