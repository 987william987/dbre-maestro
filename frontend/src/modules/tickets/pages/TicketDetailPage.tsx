import { useEffect, useState } from 'react'
import { ArrowLeft, Loader2, Play, Send, ShieldCheck, ShieldX } from 'lucide-react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useAuth } from '@/shared/auth/AuthContext'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import type { Ticket } from '@/shared/types/ticket'
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { StatusBadge } from '@/shared/ui/StatusBadge'
import { useToast } from '@/shared/ui/ToastContext'
import { createExportRequest } from '@/modules/exports/api'
import { approveTicket, executeTicket, getTicket, rejectTicket, requestExecution } from '@/modules/tickets/api'

function InfoRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="grid gap-1 border-b border-border py-3 last:border-b-0 sm:grid-cols-[132px_1fr] sm:gap-4">
      <dt className="text-xs font-semibold uppercase tracking-wide text-faint">{label}</dt>
      <dd className="text-sm text-ink">{value}</dd>
    </div>
  )
}

export function TicketDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { user } = useAuth()
  const { pushToast } = useToast()
  const [ticket, setTicket] = useState<Ticket | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [comment, setComment] = useState('')
  const [reason, setReason] = useState('')
  const [acting, setActing] = useState<'approve' | 'reject' | 'request-execution' | 'execute' | null>(null)
  const [exportState, setExportState] = useState<{ url: string; expiresAt: string } | null>(null)
  const [exporting, setExporting] = useState(false)
  const [confirmAction, setConfirmAction] = useState<'request-execution' | 'execute' | null>(null)

  useEffect(() => {
    let active = true

    async function loadTicket() {
      if (!id) {
        setError('缺少工單編號')
        setLoading(false)
        return
      }

      setLoading(true)
      setError('')

      try {
        const nextTicket = await getTicket(id)
        if (active) {
          setTicket(nextTicket)
        }
      } catch (loadError) {
        if (active) {
          setError(loadError instanceof ApiError ? loadError.message : '讀取工單失敗，請稍後重試。')
        }
      } finally {
        if (active) {
          setLoading(false)
        }
      }
    }

    void loadTicket()

    return () => {
      active = false
    }
  }, [id])

  if (!user) {
    return null
  }

  const canReview = user.authGroups.some((group) => group === 'reviewer' || group === 'dba' || group === 'admin')
  const canOperateDBA = user.authGroups.some((group) => group === 'dba' || group === 'admin')

  async function runAction(
    type: 'approve' | 'reject' | 'request-execution' | 'execute',
    action: () => Promise<Ticket>,
  ) {
    setActing(type)
    setError('')
    try {
      const updated = await action()
      setTicket(updated)
      setComment('')
      setReason('')
      pushToast('工單狀態已更新', 'success')
    } catch (actionError) {
      setError(actionError instanceof ApiError ? actionError.message : '操作失敗，請稍後重試。')
    } finally {
      setActing(null)
    }
  }

  async function handleExport() {
    if (!ticket?.db_connection_id) {
      return
    }

    setExporting(true)
    setError('')
    try {
      const result = await createExportRequest({
        sql_content: ticket.sql_content,
        db_connection_id: ticket.db_connection_id,
      })
      setExportState({
        url: result.download_url,
        expiresAt: result.expires_at,
      })
      pushToast('已建立匯出請求', 'success')
    } catch (exportError) {
      setError(exportError instanceof ApiError ? exportError.message : '建立匯出請求失敗。')
    } finally {
      setExporting(false)
    }
  }

  return (
    <div className="flex h-full flex-col gap-6 p-5 sm:p-6">
      <div className="flex items-center justify-between gap-3 border-b border-border pb-5">
        <div>
          <p className="text-[11px] font-bold uppercase tracking-[0.2em] text-faint">Ticket Detail</p>
          <h2 className="mt-2 font-display text-2xl font-black tracking-tight text-ink">
            {ticket ? ticket.title : '工單詳情'}
          </h2>
          <p className="mt-1 text-sm text-muted">檢視工單內容、目前狀態與依角色可執行的後續操作。</p>
        </div>
        <div className="flex gap-2">
          <button
            type="button"
            onClick={() => navigate(0)}
            className="inline-flex items-center gap-2 rounded-control border border-border bg-panel px-3 py-2 text-sm font-semibold text-ink transition hover:bg-page"
          >
            重新整理
          </button>
          <Link
            to="/tickets"
            className="inline-flex items-center gap-2 rounded-control border border-border bg-panel px-3 py-2 text-sm font-semibold text-ink transition hover:bg-page"
          >
            <ArrowLeft className="h-4 w-4" />
            返回列表
          </Link>
        </div>
      </div>

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      {loading ? (
        <LoadingBlock message="載入工單詳情中…" className="min-h-[420px]" />
      ) : !ticket ? (
        <div className="rounded-card border border-border bg-panel p-6 text-sm text-muted">找不到這筆工單。</div>
      ) : (
        <div className="grid gap-6 xl:grid-cols-[1.15fr_0.85fr]">
          <section className="rounded-card border border-border bg-panel p-5">
            <div className="flex flex-wrap items-center gap-3 border-b border-border pb-4">
              <span className="font-mono text-sm font-semibold text-accent">{ticket.ticket_no}</span>
              <StatusBadge status={ticket.status} />
              <span className="rounded-pill border border-border bg-panel-soft px-2 py-1 text-[11px] font-semibold uppercase tracking-wide text-muted">
                {ticket.ticket_type}
              </span>
            </div>

            <dl className="mt-2">
              <InfoRow label="Description" value={ticket.description || '—'} />
              <InfoRow label="DB Connection" value={ticket.db_connection_id ?? '未指定'} />
              <InfoRow label="Submitter" value={<span className="font-mono">{ticket.submitter_id}</span>} />
              <InfoRow label="Reviewer" value={ticket.reviewer_id ? <span className="font-mono">{ticket.reviewer_id}</span> : '—'} />
              <InfoRow label="Executor" value={ticket.executor_id ? <span className="font-mono">{ticket.executor_id}</span> : '—'} />
              <InfoRow label="Review Comment" value={ticket.review_comment || '—'} />
              <InfoRow label="Reject Reason" value={ticket.rejection_reason || '—'} />
              <InfoRow label="Created At" value={formatDateTime(ticket.created_at, true)} />
              <InfoRow label="Updated At" value={formatDateTime(ticket.updated_at, true)} />
              <InfoRow label="Started At" value={formatDateTime(ticket.started_at, true)} />
              <InfoRow label="Completed At" value={formatDateTime(ticket.completed_at, true)} />
            </dl>

            <div className="mt-6">
              <p className="text-xs font-semibold uppercase tracking-wide text-faint">SQL Content</p>
              <pre className="mt-2 overflow-x-auto rounded-card border border-border bg-[#f9fbfd] p-4 font-mono text-sm text-[#1f2937]">
                <code>{ticket.sql_content}</code>
              </pre>
            </div>
          </section>

          <section className="flex flex-col gap-4">
            {canReview ? (
              <div className="rounded-card border border-border bg-panel p-5">
                <div className="flex items-center gap-2">
                  <ShieldCheck className="h-4 w-4 text-accent" />
                  <p className="text-sm font-semibold text-ink">審核操作</p>
                </div>
                <p className="mt-1 text-xs text-muted">當前角色可進行審核。後端仍會再次檢查狀態轉移是否合法。</p>

                <label className="mt-4 flex flex-col gap-1.5">
                  <span className="text-xs font-semibold text-ink">審核意見（通過時選填）</span>
                  <textarea
                    value={comment}
                    onChange={(event) => setComment(event.target.value)}
                    className="min-h-24 rounded-card border border-border bg-panel-soft px-3 py-2 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                    disabled={acting !== null}
                  />
                </label>

                {ticket.status === 'pending_review' ? (
                  <button
                    type="button"
                    disabled={acting !== null}
                    onClick={() => void runAction('approve', () => approveTicket(ticket.id, comment))}
                    className="mt-3 inline-flex h-10 w-full items-center justify-center gap-2 rounded-control bg-brand px-4 text-sm font-bold text-white transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {acting === 'approve' ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldCheck className="h-4 w-4" />}
                    審核通過
                  </button>
                ) : (
                  <p className="mt-3 rounded-control border border-border bg-panel-soft px-3 py-2 text-xs text-muted">
                    目前狀態不是 `pending_review`，因此不能再做 approve / reject。
                  </p>
                )}

                <label className="mt-4 flex flex-col gap-1.5">
                  <span className="text-xs font-semibold text-ink">拒絕原因（必填）</span>
                  <textarea
                    value={reason}
                    onChange={(event) => setReason(event.target.value)}
                    className="min-h-24 rounded-card border border-border bg-panel-soft px-3 py-2 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                    disabled={acting !== null}
                  />
                </label>

                {ticket.status === 'pending_review' ? (
                  <button
                    type="button"
                    disabled={acting !== null || reason.trim() === ''}
                    onClick={() => void runAction('reject', () => rejectTicket(ticket.id, reason.trim()))}
                    className="mt-3 inline-flex h-10 w-full items-center justify-center gap-2 rounded-control border border-danger/20 bg-red-50 px-4 text-sm font-bold text-danger transition hover:bg-red-100 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {acting === 'reject' ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldX className="h-4 w-4" />}
                    拒絕工單
                  </button>
                ) : null}
              </div>
            ) : null}

            {canOperateDBA ? (
              <div className="rounded-card border border-border bg-panel p-5">
                <div className="flex items-center gap-2">
                  <Play className="h-4 w-4 text-accent" />
                  <p className="text-sm font-semibold text-ink">執行流程</p>
                </div>
                <p className="mt-1 text-xs text-muted">
                  `Request Execution` 會把狀態切到 `pending_execution`；`Execute` 目前是觸發執行狀態流轉，不代表已有逐句執行 viewer。
                </p>

                <div className="mt-4 flex flex-col gap-3">
                  <button
                    type="button"
                    disabled={acting !== null || ticket.status !== 'approved'}
                    onClick={() => setConfirmAction('request-execution')}
                    className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-control border border-border bg-panel-soft px-4 text-sm font-bold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {acting === 'request-execution' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}
                    Request Execution
                  </button>
                  <button
                    type="button"
                    disabled={acting !== null || ticket.status !== 'pending_execution'}
                    onClick={() => setConfirmAction('execute')}
                    className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-control bg-brand px-4 text-sm font-bold text-white transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {acting === 'execute' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}
                    Execute
                  </button>
                </div>

                <p className="mt-3 rounded-control border border-border bg-panel-soft px-3 py-2 text-xs text-muted">
                  目前狀態：`{ticket.status}`。
                  {ticket.status === 'approved'
                    ? ' 可送入待執行佇列。'
                    : ticket.status === 'pending_execution'
                      ? ' 可觸發執行。'
                      : ' 此狀態下沒有 DBA 流程操作可執行。'}
                </p>
              </div>
            ) : null}

            <div className="rounded-card border border-border bg-panel p-5">
              <div className="flex items-center gap-2">
                <Send className="h-4 w-4 text-accent" />
                <p className="text-sm font-semibold text-ink">Export</p>
              </div>
              <p className="mt-1 text-xs text-muted">
                會使用這筆工單的 SQL 與 DB 連線建立匯出請求。後端目前會直接回傳下載連結，不保存前端歷史列表。
              </p>

              <button
                type="button"
                disabled={exporting || !ticket.db_connection_id}
                onClick={() => void handleExport()}
                className="mt-4 inline-flex h-10 w-full items-center justify-center gap-2 rounded-control border border-border bg-panel-soft px-4 text-sm font-bold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
              >
                {exporting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}
                建立匯出請求
              </button>

              {!ticket.db_connection_id ? (
                <p className="mt-2 text-xs text-muted">這筆工單沒有指定 `db_connection_id`，因此目前無法直接匯出。</p>
              ) : null}

              {exportState ? (
                <div className="mt-4 rounded-card border border-border bg-panel-soft p-4">
                  <p className="text-xs font-semibold uppercase tracking-wide text-faint">Download URL</p>
                  <a
                    href={exportState.url}
                    className="mt-2 block break-all text-sm font-semibold text-accent hover:text-blue-700"
                  >
                    {exportState.url}
                  </a>
                  <p className="mt-2 text-xs text-muted">Expires at: {formatDateTime(exportState.expiresAt, true)}</p>
                </div>
              ) : null}
            </div>
          </section>
        </div>
      )}

      <ConfirmDialog
        open={confirmAction !== null}
        title={confirmAction === 'request-execution' ? '送入待執行佇列' : '觸發執行流程'}
        description={
          confirmAction === 'request-execution'
            ? '確認將這筆工單送入待執行佇列？後續 DBA 可從 pending_execution 狀態再觸發執行。'
            : '確認觸發這筆工單進入執行流程？這會呼叫後端 execute API。'
        }
        confirmLabel={confirmAction === 'request-execution' ? '確認送出' : '確認執行'}
        loading={confirmAction !== null && acting === confirmAction}
        onCancel={() => setConfirmAction(null)}
        onConfirm={() => {
          if (!ticket) return
          if (confirmAction === 'request-execution') {
            void runAction('request-execution', () => requestExecution(ticket.id)).finally(() => setConfirmAction(null))
          }
          if (confirmAction === 'execute') {
            void runAction('execute', () => executeTicket(ticket.id)).finally(() => setConfirmAction(null))
          }
        }}
      />
    </div>
  )
}
