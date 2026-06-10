import { useEffect, useState } from 'react'
import { ArrowRight, RefreshCcw } from 'lucide-react'
import { Link } from 'react-router-dom'
import { EmptyState } from '@/components/tickets/EmptyState'
import { ApiError } from '@/shared/api/client'
import { useAuth } from '@/shared/auth/AuthContext'
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
  const { user } = useAuth()
  const [tickets, setTickets] = useState<Ticket[]>([])
  const [status, setStatus] = useState<'' | TicketStatus>('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  async function loadTickets(nextStatus: '' | TicketStatus) {
    setLoading(true)
    setError('')

    try {
      const response = await listTickets(nextStatus || undefined)
      setTickets(Array.isArray(response.tickets) ? response.tickets : [])
    } catch (loadError) {
      setError(loadError instanceof ApiError ? loadError.message : '讀取工單列表失敗，請稍後重試。')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void loadTickets(status)
  }, [status])

  const canCreateTicket = user?.permissions.includes('tickets.apply') ?? false

  return (
    <div className="flex h-full flex-col gap-3 p-3 sm:p-4">
      <section className="rounded-[22px] border border-white/85 bg-[rgba(248,250,252,0.82)] shadow-soft">
        <div className="border-b border-border/80 px-4 py-3 sm:px-5">
          <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
            <div className="max-w-3xl">
              <div className="flex flex-wrap items-center gap-2 text-[11px] font-semibold text-muted">
                <span className="rounded-full border border-border bg-white px-2.5 py-1 text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                  Tickets
                </span>
                <span>/</span>
                <span>Operations Queue</span>
              </div>
              <h2 className="mt-3 text-[24px] font-bold tracking-[-0.03em] text-ink">工單工作台</h2>
              <p className="mt-2 max-w-[860px] text-[13px] leading-6 text-muted">
                以單一佇列檢視提交、審核、待執行與歷史紀錄。顯示範圍由目前角色與後端授權決定。
              </p>
            </div>

            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                onClick={() => void loadTickets(status)}
                className="inline-flex h-10 items-center gap-2 rounded-[12px] border border-border bg-white px-3.5 text-[13px] font-semibold text-ink transition-colors hover:bg-panel-soft"
              >
                <RefreshCcw className="h-4 w-4" />
                重新整理
              </button>
              {canCreateTicket ? (
                <Link
                  to="/tickets/new"
                  className="inline-flex h-10 items-center gap-2 rounded-[12px] bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition-colors hover:bg-slate-800"
                >
                  建立新工單
                  <ArrowRight className="h-4 w-4" />
                </Link>
              ) : null}
            </div>
          </div>
        </div>

        <div className="flex flex-col gap-3 px-4 py-3 sm:px-5 lg:flex-row lg:items-center lg:justify-between">
          <div className="flex flex-wrap items-center gap-2 text-[12px] text-muted">
            <span className="font-semibold text-ink">Current view</span>
            <span className="rounded-full border border-border bg-white px-2.5 py-1 font-semibold text-ink">
              {status ? STATUS_OPTIONS.find((option) => option.value === status)?.label : '全部工單'}
            </span>
            <span>{tickets.length} 筆</span>
          </div>

          <div className="flex flex-wrap items-center gap-2.5">
            <label className="flex items-center gap-2 rounded-[12px] border border-border bg-white px-3 py-2 text-[12px] text-muted shadow-soft">
              <span className="font-semibold text-ink">狀態</span>
              <select
                value={status}
                onChange={(event) => setStatus(event.target.value as '' | TicketStatus)}
                className="h-8 min-w-[128px] rounded-[10px] border border-border bg-panel-soft px-2.5 text-[12px] font-semibold text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
              >
                {STATUS_OPTIONS.map((option) => (
                  <option key={option.value || 'all'} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </label>
          </div>
        </div>
      </section>

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      <div className="overflow-hidden rounded-[22px] border border-white/85 bg-white/92 shadow-soft">
        {loading ? (
          <LoadingBlock message="載入工單列表中…" className="h-[320px] rounded-none border-0 bg-transparent" />
        ) : tickets.length === 0 ? (
          <div className="bg-panel">
            <EmptyState variant={status ? 'search' : 'history'} />
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="min-w-full border-collapse">
              <thead className="bg-editor-toolbar">
                <tr className="text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
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
                  <tr
                    key={ticket.id}
                    className="border-t border-border/90 text-sm text-ink transition-colors hover:bg-slate-50/70"
                  >
                    <td className="px-4 py-3.5 align-top">
                      <Link
                        to={`/tickets/${ticket.id}`}
                        className="inline-flex rounded-[8px] border border-transparent px-1.5 py-1 font-mono text-[12px] font-semibold text-accent transition hover:border-accent/15 hover:bg-accent-soft hover:text-blue-700"
                      >
                        {ticket.ticket_no}
                      </Link>
                    </td>
                    <td className="px-4 py-3.5 align-top">
                      <div>
                        <p className="text-[14px] font-semibold tracking-tight text-ink">{ticket.title}</p>
                        {ticket.description ? <p className="mt-1 max-w-[420px] text-[12px] leading-5 text-muted">{ticket.description}</p> : null}
                      </div>
                    </td>
                    <td className="px-4 py-3.5 align-top">
                      <span className="rounded-full border border-border bg-panel-soft px-2.5 py-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted">
                        {ticket.ticket_type}
                      </span>
                    </td>
                    <td className="px-4 py-3.5 align-top">
                      <StatusBadge status={ticket.status} />
                    </td>
                    <td className="px-4 py-3.5 align-top font-mono text-[12px] text-muted">{ticket.submitter_id}</td>
                    <td className="px-4 py-3.5 align-top text-[12px] text-muted">{formatDateTime(ticket.created_at)}</td>
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
