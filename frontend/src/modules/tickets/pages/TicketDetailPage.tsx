import { useEffect, useState } from 'react'
import { ArrowLeft, Download, Loader2, Play, Send, ShieldCheck, ShieldX } from 'lucide-react'
import { Link, useParams } from 'react-router-dom'
import { useAuth } from '@/shared/auth/AuthContext'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import type { Ticket, TicketDetail, TicketScope } from '@/shared/types/ticket'
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { StatusBadge } from '@/shared/ui/StatusBadge'
import { useToast } from '@/shared/ui/ToastContext'
import { approveTicket, executeTicket, getTicket, rejectTicket, requestExecution, revokeTicket } from '@/modules/tickets/api'

function InfoRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="grid gap-1 border-b border-border/90 py-3 last:border-b-0 sm:grid-cols-[132px_1fr] sm:gap-4">
      <dt className="text-[11px] font-semibold uppercase tracking-wide text-faint">{label}</dt>
      <dd className="text-[13px] text-ink">{value}</dd>
    </div>
  )
}

export function TicketDetailPage() {
  const { id } = useParams<{ id: string }>()
  const { user } = useAuth()
  const { pushToast } = useToast()
  const [detail, setDetail] = useState<TicketDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [comment, setComment] = useState('')
  const [reason, setReason] = useState('')
  const [acting, setActing] = useState<'approve' | 'reject' | 'request-execution' | 'execute' | 'revoke' | null>(null)
  const [confirmAction, setConfirmAction] = useState<'request-execution' | 'execute' | 'revoke' | null>(null)

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
        const nextDetail = await getTicket(id)
        if (active) {
          setDetail(nextDetail)
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

  const ticket = detail?.ticket ?? null
  const canReview = detail?.capabilities?.can_review ?? false
  const canOperateDBA = detail?.capabilities?.can_request_execution ?? false
  const canExecute = detail?.capabilities?.can_execute ?? false
  const canRevoke = detail?.capabilities?.can_revoke ?? false
  const exportDownloadURL = detail?.export_request?.download_url ?? null

  async function reloadTicket() {
    if (!id) {
      return
    }
    const nextDetail = await getTicket(id)
    setDetail(nextDetail)
  }

  async function runAction(
    type: 'approve' | 'reject' | 'request-execution' | 'execute' | 'revoke',
    action: () => Promise<Ticket | void>,
  ) {
    setActing(type)
    setError('')
    try {
      await action()
      await reloadTicket()
      setComment('')
      setReason('')
      pushToast('工單狀態已更新', 'success')
    } catch (actionError) {
      setError(actionError instanceof ApiError ? actionError.message : '操作失敗，請稍後重試。')
    } finally {
      setActing(null)
    }
  }

  return (
    <div className="flex h-full flex-col gap-3 p-3 sm:p-4">
      <section className="rounded-xl border border-border bg-panel-soft shadow-soft">
        <div className="border-b border-border/80 px-4 py-3 sm:px-5">
          <div className="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
            <div className="max-w-3xl">
              <div className="flex flex-wrap items-center gap-3">
                <h2 className="text-[24px] font-bold tracking-[-0.03em] text-ink">{ticket ? ticket.title : '工單詳情'}</h2>
                {ticket ? <StatusBadge status={ticket.status} /> : null}
              </div>
              <p className="mt-2 text-[13px] leading-6 text-muted">
                {ticket ? (
                  <>
                    <span className="font-mono font-semibold text-ink">{ticket.ticket_no}</span>
                    <span className="mx-2 text-faint">·</span>
                    建立於 {formatDateTime(ticket.created_at, true)}
                  </>
                ) : (
                  '檢視工單內容、狀態與依角色可執行的後續操作。'
                )}
              </p>
            </div>
            <Link
              to="/tickets"
              className="inline-flex h-10 shrink-0 items-center gap-2 rounded-lg border border-border bg-white px-4 text-[13px] font-semibold text-ink transition hover:bg-panel-soft"
            >
              <ArrowLeft className="h-4 w-4" />
              返回列表
            </Link>
          </div>
        </div>
      </section>

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      {loading ? (
        <LoadingBlock message="載入工單詳情中…" className="min-h-[420px] rounded-xl border-border bg-panel" />
      ) : !ticket || !detail ? (
        <div className="rounded-xl border border-border bg-panel p-6 text-sm text-muted shadow-soft">找不到這筆工單。</div>
      ) : (
        <div className="grid gap-3 xl:grid-cols-[1.15fr_0.85fr]">
          <section className="rounded-xl border border-border bg-panel shadow-soft">
            <div className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-3">
              <span className="font-mono text-sm font-semibold text-accent">{ticket.ticket_no}</span>
              <StatusBadge status={ticket.status} />
              <span className="rounded-full border border-border bg-panel-soft px-2.5 py-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted">
                {ticket.ticket_type}
              </span>
            </div>

            <dl className="px-4 py-2">
              <InfoRow label="Description" value={ticket.description || '—'} />
              <InfoRow label="DB Connection" value={ticket.db_connection_id ?? '未指定'} />
              <InfoRow label="Submitter" value={<span className="font-mono">{ticket.submitter_id}</span>} />
              <InfoRow label="Reviewer" value={ticket.reviewer_id ? <span className="font-mono">{ticket.reviewer_id}</span> : '—'} />
              <InfoRow label="Executor" value={ticket.executor_id ? <span className="font-mono">{ticket.executor_id}</span> : '—'} />
              <InfoRow label="Review Comment" value={ticket.review_comment || '—'} />
              <InfoRow label="Reject Reason" value={ticket.rejection_reason || '—'} />
              <InfoRow label="Approved Duration" value={ticket.approved_duration_minutes ? `${ticket.approved_duration_minutes} 分鐘` : '—'} />
              <InfoRow label="Approved Until" value={formatDateTime(ticket.approved_until, true)} />
              <InfoRow label="Revoked At" value={formatDateTime(ticket.revoked_at, true)} />
              <InfoRow label="Revoked By" value={ticket.revoked_by ? <span className="font-mono">{ticket.revoked_by}</span> : '—'} />
              <InfoRow label="Created At" value={formatDateTime(ticket.created_at, true)} />
              <InfoRow label="Updated At" value={formatDateTime(ticket.updated_at, true)} />
              <InfoRow label="Started At" value={formatDateTime(ticket.started_at, true)} />
              <InfoRow label="Completed At" value={formatDateTime(ticket.completed_at, true)} />
            </dl>

            {detail.scopes.length > 0 ? (
              <div className="border-t border-border px-4 py-4">
                <p className="text-[11px] font-semibold uppercase tracking-wide text-faint">Scopes</p>
                <div className="mt-3 space-y-2">
                  {detail.scopes.map((scope) => (
                    <ScopeRow key={scope.id} scope={scope} />
                  ))}
                </div>
              </div>
            ) : null}

            <div className="border-t border-border px-4 py-4">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-faint">SQL Content</p>
              <pre className="mt-2 overflow-x-auto rounded-xl border border-border bg-panel-soft p-4 font-mono text-[13px] leading-7 text-ink">
                <code>{ticket.sql_content}</code>
              </pre>
            </div>
          </section>

          <section className="flex flex-col gap-3">
            {canReview ? (
              <div className="rounded-xl border border-border bg-panel shadow-soft">
                <div className="border-b border-border/80 px-4 py-3">
                  <div className="flex items-center gap-2">
                    <ShieldCheck className="h-4 w-4 text-accent" />
                    <p className="text-[13px] font-semibold text-ink">審核操作</p>
                  </div>
                  <p className="mt-1 text-[12px] text-muted">當前角色可進行審核。後端仍會再次檢查狀態轉移是否合法。</p>
                </div>

                <div className="px-4 py-4">
                  <label className="flex flex-col gap-1.5">
                    <span className="text-[12px] font-semibold text-ink">審核意見（通過時選填）</span>
                    <textarea
                      value={comment}
                      onChange={(event) => setComment(event.target.value)}
                      className="min-h-24 rounded-lg border border-border bg-panel-soft px-3 py-2 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                      disabled={acting !== null}
                    />
                  </label>

                  {ticket.status === 'pending_review' ? (
                    <button
                      type="button"
                      disabled={acting !== null}
                      onClick={() => void runAction('approve', () => approveTicket(ticket.id, comment))}
                      className="mt-3 inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {acting === 'approve' ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldCheck className="h-4 w-4" />}
                      審核通過
                    </button>
                  ) : (
                    <p className="mt-3 rounded-lg border border-border bg-panel-soft px-3 py-2 text-[12px] text-muted">
                      目前狀態不是 `pending_review`，因此不能再做 approve / reject。
                    </p>
                  )}

                  <label className="mt-4 flex flex-col gap-1.5">
                    <span className="text-[12px] font-semibold text-ink">拒絕原因（必填）</span>
                    <textarea
                      value={reason}
                      onChange={(event) => setReason(event.target.value)}
                      className="min-h-24 rounded-lg border border-border bg-panel-soft px-3 py-2 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                      disabled={acting !== null}
                    />
                  </label>

                  {ticket.status === 'pending_review' ? (
                    <button
                      type="button"
                      disabled={acting !== null || reason.trim() === ''}
                      onClick={() => void runAction('reject', () => rejectTicket(ticket.id, reason.trim()))}
                      className="mt-3 inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg border border-danger/20 bg-red-50 px-4 text-[13px] font-bold text-danger transition hover:bg-red-100 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {acting === 'reject' ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldX className="h-4 w-4" />}
                      拒絕工單
                    </button>
                  ) : null}
                </div>
              </div>
            ) : null}

            {canOperateDBA || canExecute || canRevoke ? (
              <div className="rounded-xl border border-border bg-panel shadow-soft">
                <div className="border-b border-border/80 px-4 py-3">
                  <div className="flex items-center gap-2">
                    <Play className="h-4 w-4 text-accent" />
                    <p className="text-[13px] font-semibold text-ink">執行流程</p>
                  </div>
                  <p className="mt-1 text-[12px] text-muted">依工單類型提供送審後執行，或臨時敏感查詢的提前撤銷操作。</p>
                </div>

                <div className="flex flex-col gap-3 px-4 py-4">
                  <button
                    type="button"
                    disabled={acting !== null || !canOperateDBA || ticket.status !== 'approved'}
                    onClick={() => setConfirmAction('request-execution')}
                    className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg border border-border bg-panel-soft px-4 text-[13px] font-bold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {acting === 'request-execution' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}
                    Request Execution
                  </button>
                  <button
                    type="button"
                    disabled={acting !== null || !canExecute || ticket.status !== 'pending_execution'}
                    onClick={() => setConfirmAction('execute')}
                    className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {acting === 'execute' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}
                    Execute
                  </button>
                  {canRevoke ? (
                    <button
                      type="button"
                      disabled={acting !== null || ticket.status !== 'approved' || ticket.ticket_type !== 'sensitive_query_access'}
                      onClick={() => setConfirmAction('revoke')}
                      className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg border border-danger/20 bg-red-50 px-4 text-[13px] font-bold text-danger transition hover:bg-red-100 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {acting === 'revoke' ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldX className="h-4 w-4" />}
                      提前撤銷
                    </button>
                  ) : null}
                </div>

                <p className="mx-4 mb-4 rounded-lg border border-border bg-panel-soft px-3 py-2 text-[12px] text-muted">
                  目前狀態：`{ticket.status}`。
                  {ticket.ticket_type === 'sensitive_query_access' && canRevoke
                    ? ' 已核准的臨時敏感查詢可在此提前撤銷。'
                    : ticket.status === 'approved'
                    ? ' 可送入待執行佇列。'
                    : ticket.status === 'pending_execution'
                      ? ' 可觸發執行。'
                      : ' 此狀態下沒有 DBA 流程操作可執行。'}
                </p>
              </div>
            ) : null}

            {ticket.ticket_type === 'sql_export' && detail.capabilities.can_download_export && exportDownloadURL ? (
              <div className="rounded-xl border border-border bg-panel shadow-soft">
                <div className="border-b border-border/80 px-4 py-3">
                  <div className="flex items-center gap-2">
                    <Download className="h-4 w-4 text-accent" />
                    <p className="text-[13px] font-semibold text-ink">匯出下載</p>
                  </div>
                  <p className="mt-1 text-[12px] text-muted">工單審核通過後，從這裡下載 ready export。</p>
                </div>
                <div className="px-4 py-4">
                  <a
                    href={exportDownloadURL}
                    className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white transition hover:bg-slate-800"
                  >
                    <Download className="h-4 w-4" />
                    下載匯出檔
                  </a>
                  <p className="mt-3 text-[12px] text-muted">
                    有效期限：{detail.export_request?.expires_at ? formatDateTime(detail.export_request.expires_at, true) : '—'}
                  </p>
                </div>
              </div>
            ) : null}
          </section>
        </div>
      )}

      <ConfirmDialog
        open={confirmAction !== null}
        title={confirmAction === 'request-execution' ? '送入待執行佇列' : confirmAction === 'execute' ? '觸發執行流程' : '提前撤銷敏感查詢'}
        description={
          confirmAction === 'request-execution'
            ? '確認將這筆工單送入待執行佇列？後續 DBA 可從 pending_execution 狀態再觸發執行。'
            : confirmAction === 'execute'
              ? '確認觸發這筆工單進入執行流程？這會呼叫後端 execute API。'
              : '確認提前撤銷這筆 Sensitive Access 工單？撤銷後從下一次查詢起立即失效。'
        }
        confirmLabel={confirmAction === 'request-execution' ? '確認送出' : confirmAction === 'execute' ? '確認執行' : '確認撤銷'}
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
          if (confirmAction === 'revoke') {
            void runAction('revoke', () => revokeTicket(ticket.id)).finally(() => setConfirmAction(null))
          }
        }}
      />
    </div>
  )
}

function ScopeRow({ scope }: { scope: TicketScope }) {
  const parts = [scope.connection_id.toString()]
  if (scope.database_name) {
    parts.push(scope.database_name)
  }
  if (scope.schema_name) {
    parts.push(scope.schema_name)
  }
  if (scope.table_name) {
    parts.push(scope.table_name)
  }
  parts.push(scope.column_name)

  return (
    <div className="rounded-lg border border-border bg-panel-soft px-3 py-2 text-[12px] text-ink">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-mono">{parts.join(' / ')}</span>
        {scope.is_sensitive ? (
          <span className="rounded-full border border-rose-200 bg-rose-50 px-2 py-0.5 text-[10px] font-semibold text-rose-700">Sensitive</span>
        ) : null}
        <span className="rounded-full border border-border bg-white px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted">
          {scope.source_kind}
        </span>
      </div>
    </div>
  )
}
