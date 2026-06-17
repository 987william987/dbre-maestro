import { useEffect, useState } from 'react'
import { ArrowLeft, Check, Download, Loader2, Play, Send, ShieldCheck, ShieldX, X } from 'lucide-react'
import { Link, useParams } from 'react-router-dom'
import { useAuth } from '@/shared/auth/AuthContext'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import type { Ticket, TicketDetail, TicketScope, TicketWorkflowParticipants } from '@/shared/types/ticket'
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { PageIntro } from '@/shared/ui/PageIntro'
import { StatusBadge } from '@/shared/ui/StatusBadge'
import { useToast } from '@/shared/ui/ToastContext'
import { approveTicket, downloadTicketExport, executeTicket, getTicket, rejectTicket, requestExecution, revokeTicket, withdrawTicket } from '@/modules/tickets/api'

function InfoRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="grid gap-1 border-b border-border/90 py-3 last:border-b-0 sm:grid-cols-[132px_1fr] sm:gap-4">
      <dt className="text-[11px] font-semibold uppercase tracking-wide text-faint">{label}</dt>
      <dd className="text-[13px] text-ink">{value}</dd>
    </div>
  )
}

function formatTicketActor(name: string | null | undefined, id: number | null | undefined) {
  if (name && name.trim()) {
    return name
  }
  if (id != null) {
    return String(id)
  }
  return '—'
}

type WorkflowStepTone = 'done' | 'current' | 'upcoming' | 'failed'

type WorkflowStep = {
  key: string
  title: string
  actor: string
  tone: WorkflowStepTone
  detail: string
}

function joinParticipantNames(names: string[], fallback: string) {
  const normalized = names.map((item) => item.trim()).filter(Boolean)
  if (normalized.length === 0) {
    return fallback
  }
  return normalized.join(', ')
}

function buildWorkflowSteps(ticket: Ticket, workflowParticipants: TicketWorkflowParticipants): WorkflowStep[] {
  const submitter = formatTicketActor(ticket.submitter_name, ticket.submitter_id)
  const reviewer = ticket.reviewer_id != null || ticket.reviewer_name
    ? formatTicketActor(ticket.reviewer_name, ticket.reviewer_id ?? null)
    : joinParticipantNames(workflowParticipants.reviewers, 'Pending reviewer assignment')
  const executor = ticket.executor_id != null || ticket.executor_name
    ? formatTicketActor(ticket.executor_name, ticket.executor_id ?? null)
    : joinParticipantNames(workflowParticipants.executors, 'Pending executor assignment')
  const usesExecutor = ticket.ticket_type === 'ddl' || ticket.ticket_type === 'dml'

  const reviewerTone: WorkflowStepTone =
    ticket.status === 'pending_review' ? 'current'
      : ticket.status === 'rejected' || ticket.status === 'withdrawn' ? 'failed'
      : 'done'
  const reviewerDetail =
    reviewerTone === 'current' ? 'Waiting for review'
      : ticket.status === 'withdrawn' ? 'Withdrawn by submitter'
      : reviewerTone === 'failed' ? 'Rejected at review stage'
      : 'Review completed'

  const executorTone: WorkflowStepTone = !usesExecutor
    ? 'upcoming'
    : ticket.status === 'rejected' || ticket.status === 'withdrawn' ? 'upcoming'
      : ticket.status === 'approved' || ticket.status === 'pending_execution' || ticket.status === 'executing' ? 'current'
        : ticket.status === 'completed' ? 'done'
          : ticket.status === 'failed' || ticket.status === 'stopped' || ticket.status === 'interrupted' ? 'failed'
            : 'upcoming'
  const executorDetail = !usesExecutor
    ? 'No execution stage for this ticket type'
    : executorTone === 'current' ? 'Waiting for DBA execution'
      : executorTone === 'done' ? 'Execution completed'
        : executorTone === 'failed' ? 'Execution ended with an exception'
          : 'Will enter execution after approval'

  const completionTone: WorkflowStepTone = usesExecutor
    ? ticket.status === 'completed' ? 'done'
      : ticket.status === 'failed' || ticket.status === 'stopped' || ticket.status === 'interrupted' || ticket.status === 'rejected' || ticket.status === 'withdrawn' ? 'failed'
        : 'upcoming'
    : ticket.status === 'approved' || ticket.status === 'completed' ? 'done'
      : ticket.status === 'rejected' || ticket.status === 'withdrawn' ? 'failed'
        : 'upcoming'
  const completionDetail = usesExecutor
    ? completionTone === 'done' ? 'Ticket closed successfully'
      : completionTone === 'failed' ? 'Ticket closed unsuccessfully'
        : 'Waiting for execution to finish'
    : completionTone === 'done' ? 'Ticket completed after approval'
      : completionTone === 'failed' ? 'Ticket closed unsuccessfully'
        : 'Waiting for approval to complete the request'

  const steps: WorkflowStep[] = [
    {
      key: 'submitter',
      title: 'Submitted',
      actor: submitter,
      tone: 'done',
      detail: 'Ticket has been created',
    },
    {
      key: 'reviewer',
      title: 'Review',
      actor: reviewer,
      tone: reviewerTone,
      detail: reviewerDetail,
    },
  ]

  if (usesExecutor) {
    steps.push({
      key: 'executor',
      title: 'Execution',
      actor: executor,
      tone: executorTone,
      detail: executorDetail,
    })
  }

  steps.push({
    key: 'complete',
    title: 'Complete',
    actor: usesExecutor ? 'System status update' : 'Approval outcome',
    tone: completionTone,
    detail: completionDetail,
  })

  return steps
}

function WorkflowStepIcon({ tone }: { tone: WorkflowStepTone }) {
  if (tone === 'done') {
    return (
      <span className="inline-flex h-9 w-9 items-center justify-center rounded-full bg-emerald-500 text-white">
        <Check className="h-5 w-5" />
      </span>
    )
  }
  if (tone === 'current') {
    return (
      <span className="inline-flex h-9 w-9 items-center justify-center rounded-full border-[6px] border-accent bg-white text-accent" />
    )
  }
  if (tone === 'failed') {
    return (
      <span className="inline-flex h-9 w-9 items-center justify-center rounded-full bg-rose-500 text-white">
        <X className="h-5 w-5" />
      </span>
    )
  }
  return (
    <span className="inline-flex h-9 w-9 items-center justify-center rounded-full bg-slate-300 text-white text-sm font-bold">
      •
    </span>
  )
}

function WorkflowTimeline({ ticket, workflowParticipants }: { ticket: Ticket; workflowParticipants: TicketWorkflowParticipants }) {
  const steps = buildWorkflowSteps(ticket, workflowParticipants)

  return (
    <section className="rounded-xl border border-border bg-panel shadow-soft">
      <div className="border-b border-border/80 px-4 py-3">
        <p className="text-[13px] font-semibold text-ink">Approval Flow</p>
      </div>

      <div className="overflow-x-auto px-4 py-4">
        <div className="grid min-w-[720px] gap-4" style={{ gridTemplateColumns: `repeat(${steps.length}, minmax(0, 1fr))` }}>
          {steps.map((step, index) => (
            <div key={step.key} className="relative pr-4 last:pr-0">
              {index < steps.length - 1 ? (
                <span className="absolute left-[44px] right-0 top-[18px] h-px bg-border" aria-hidden="true" />
              ) : null}
              <div className="relative">
                <WorkflowStepIcon tone={step.tone} />
                <div className="mt-3">
                  <p className="text-[13px] font-semibold text-ink">{step.title}</p>
                  <p className="mt-1 text-[12px] font-medium text-ink">{step.actor}</p>
                  <p className="mt-1 text-[11px] text-muted">{step.detail}</p>
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
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
  const [acting, setActing] = useState<'approve' | 'reject' | 'withdraw' | 'request-execution' | 'execute' | 'revoke' | null>(null)
  const [confirmAction, setConfirmAction] = useState<'withdraw' | 'request-execution' | 'execute' | 'revoke' | null>(null)
  const [downloadingExport, setDownloadingExport] = useState(false)

  useEffect(() => {
    let active = true

    async function loadTicket() {
      if (!id) {
        setError('Missing ticket number')
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
          setError(loadError instanceof ApiError ? loadError.message : 'Failed to load ticket. Please try again later.')
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
  const canReject = detail?.capabilities?.can_reject ?? false
  const canWithdraw = detail?.capabilities?.can_withdraw ?? false
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
    type: 'approve' | 'reject' | 'withdraw' | 'request-execution' | 'execute' | 'revoke',
    action: () => Promise<Ticket | void>,
  ) {
    setActing(type)
    setError('')
    try {
      await action()
      await reloadTicket()
      setComment('')
      setReason('')
      pushToast('Ticket updated', 'success')
    } catch (actionError) {
      setError(actionError instanceof ApiError ? actionError.message : 'Action failed. Please try again later.')
    } finally {
      setActing(null)
    }
  }

  async function handleDownloadExport() {
    if (!exportDownloadURL || downloadingExport) {
      return
    }

    setDownloadingExport(true)
    try {
      await downloadTicketExport(exportDownloadURL)
    } catch (downloadError) {
      pushToast(downloadError instanceof ApiError ? downloadError.message : 'Failed to download export.', 'error', {
        placement: 'center',
        durationMs: 3600,
      })
    } finally {
      setDownloadingExport(false)
    }
  }

  return (
    <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
      <PageIntro
        title={
          <span className="flex flex-wrap items-center gap-3">
            <span>{ticket ? ticket.title : 'Ticket Detail'}</span>
            {ticket ? <StatusBadge status={ticket.status} /> : null}
          </span>
        }
        description={
          ticket ? (
            <span className="font-mono font-semibold text-ink">{ticket.ticket_no}</span>
          ) : (
            'View ticket details, status, and available actions based on your role.'
          )
        }
        actions={
          <Link
            to="/tickets"
            className="inline-flex h-10 shrink-0 items-center gap-2 rounded-lg border border-border bg-white px-4 text-[13px] font-semibold text-ink transition hover:bg-panel-soft"
          >
            <ArrowLeft className="h-4 w-4" />
            Back to list
          </Link>
        }
      />

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      {loading ? (
        <LoadingBlock message="Loading ticket…" className="min-h-[420px] rounded-xl border-border bg-panel" />
      ) : !ticket || !detail ? (
        <div className="rounded-xl border border-border bg-panel p-6 text-sm text-muted shadow-soft">Ticket not found.</div>
      ) : (
        <div className="space-y-3">
          <WorkflowTimeline ticket={ticket} workflowParticipants={detail.workflow_participants} />
          <div className="grid gap-3 xl:grid-cols-[1.15fr_0.85fr]">
          <section className="rounded-xl border border-border bg-panel shadow-soft">
            <div className="border-b border-border px-4 py-3">
              <span className="font-mono text-sm font-semibold text-accent">{ticket.ticket_no}</span>
            </div>

            <dl className="px-4 py-2">
              <InfoRow label="Description" value={ticket.description || '—'} />
              <InfoRow label="Ticket Type" value={ticket.ticket_type.toUpperCase()} />
              <InfoRow label="DB Connection" value={ticket.db_connection_name || ticket.db_connection_id || 'Not specified'} />
              <InfoRow label="Database" value={ticket.database_name || '—'} />
              <InfoRow label="Submitter" value={formatTicketActor(ticket.submitter_name, ticket.submitter_id)} />
              <InfoRow label="Reviewer" value={formatTicketActor(ticket.reviewer_name, ticket.reviewer_id ?? null)} />
              <InfoRow label="Executor" value={formatTicketActor(ticket.executor_name, ticket.executor_id ?? null)} />
              <InfoRow label="Review Comment" value={ticket.review_comment || '—'} />
              <InfoRow label="Reject Reason" value={ticket.rejection_reason || '—'} />
              <InfoRow label="Approved Duration" value={ticket.approved_duration_minutes ? `${ticket.approved_duration_minutes} min` : '—'} />
              <InfoRow label="Approved Until" value={formatDateTime(ticket.approved_until, true)} />
              <InfoRow label="Revoked At" value={formatDateTime(ticket.revoked_at, true)} />
              <InfoRow label="Revoked By" value={formatTicketActor(ticket.revoked_by_name, ticket.revoked_by ?? null)} />
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

            {detail.review_results.length > 0 ? (
              <div className="border-t border-border px-4 py-4">
                <p className="text-[11px] font-semibold uppercase tracking-wide text-faint">Review Results</p>
                <div className="mt-3 overflow-x-auto rounded-xl border border-border">
                  <table className="min-w-full border-collapse">
                    <thead className="bg-panel-soft text-left text-[11px] font-semibold text-faint">
                      <tr>
                        <th className="px-4 py-3">ID</th>
                        <th className="px-4 py-3">SQL</th>
                        <th className="px-4 py-3">Scan / Impact Rows</th>
                        <th className="px-4 py-3">Review Status</th>
                        <th className="px-4 py-3">Review Message</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-border bg-white text-[13px] text-ink">
                      {detail.review_results.map((result) => (
                        <tr key={result.id || `${result.seq}-${result.sql_stmt}`}>
                          <td className="px-4 py-3 align-top">{result.seq}</td>
                          <td className="px-4 py-3 align-top font-mono text-[12px]">{result.sql_stmt}</td>
                          <td className="px-4 py-3 align-top">{result.scan_rows}</td>
                          <td className="px-4 py-3 align-top">{result.status}</td>
                          <td className="px-4 py-3 align-top text-muted">{result.message || '—'}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            ) : null}
          </section>

          <section className="flex flex-col gap-3">
            {canReview && ticket.status === 'pending_review' ? (
              <div className="rounded-xl border border-border bg-panel shadow-soft">
                <div className="border-b border-border/80 px-4 py-3">
                  <div className="flex items-center gap-2">
                    <ShieldCheck className="h-4 w-4 text-accent" />
                    <p className="text-[13px] font-semibold text-ink">Review</p>
                  </div>
                  <p className="mt-1 text-[12px] text-muted">Your role can review this ticket. The backend will re-validate the state transition.</p>
                </div>

                <div className="px-4 py-4">
                  <label className="flex flex-col gap-1.5">
                    <span className="text-[12px] font-semibold text-ink">Review comment (optional)</span>
                    <textarea
                      value={comment}
                      onChange={(event) => setComment(event.target.value)}
                      className="min-h-24 rounded-lg border border-border bg-panel-soft px-3 py-2 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                      disabled={acting !== null}
                    />
                  </label>

                  <button
                    type="button"
                    disabled={acting !== null}
                    onClick={() => void runAction('approve', () => approveTicket(ticket.id, comment))}
                    className="mt-3 inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {acting === 'approve' ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldCheck className="h-4 w-4" />}
                    Approve
                  </button>

                  <label className="mt-4 flex flex-col gap-1.5">
                    <span className="text-[12px] font-semibold text-ink">Rejection reason (required)</span>
                    <textarea
                      value={reason}
                      onChange={(event) => setReason(event.target.value)}
                      className="min-h-24 rounded-lg border border-border bg-panel-soft px-3 py-2 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                      disabled={acting !== null}
                    />
                  </label>

                  <button
                    type="button"
                    disabled={acting !== null || reason.trim() === ''}
                    onClick={() => void runAction('reject', () => rejectTicket(ticket.id, reason.trim()))}
                    className="mt-3 inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg border border-danger/20 bg-red-50 px-4 text-[13px] font-bold text-danger transition hover:bg-red-100 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {acting === 'reject' ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldX className="h-4 w-4" />}
                    Reject
                  </button>
                </div>
              </div>
            ) : null}

            {canWithdraw && ticket.status === 'pending_review' ? (
              <div className="rounded-xl border border-border bg-panel shadow-soft">
                <div className="border-b border-border/80 px-4 py-3">
                  <div className="flex items-center gap-2">
                    <Send className="h-4 w-4 text-accent" />
                    <p className="text-[13px] font-semibold text-ink">Submission</p>
                  </div>
                  <p className="mt-1 text-[12px] text-muted">Withdraw this ticket before review starts.</p>
                </div>

                <div className="px-4 py-4">
                  <button
                    type="button"
                    disabled={acting !== null}
                    onClick={() => setConfirmAction('withdraw')}
                    className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg border border-danger/20 bg-red-50 px-4 text-[13px] font-bold text-danger transition hover:bg-red-100 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {acting === 'withdraw' ? <Loader2 className="h-4 w-4 animate-spin" /> : <X className="h-4 w-4" />}
                    Withdraw Ticket
                  </button>
                </div>
              </div>
            ) : null}

            {canOperateDBA || canExecute || canReject || canRevoke ? (
              <div className="rounded-xl border border-border bg-panel shadow-soft">
                <div className="border-b border-border/80 px-4 py-3">
                  <div className="flex items-center gap-2">
                    <Play className="h-4 w-4 text-accent" />
                    <p className="text-[13px] font-semibold text-ink">Execution</p>
                  </div>
                  <p className="mt-1 text-[12px] text-muted">Request execution, reject at execution stage, execute the ticket, or revoke active sensitive access.</p>
                </div>

                <div className="flex flex-col gap-3 px-4 py-4">
                  {ticket.status === 'approved' ? (
                    <button
                      type="button"
                      disabled={acting !== null || !canOperateDBA}
                      onClick={() => setConfirmAction('request-execution')}
                      className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg border border-border bg-panel-soft px-4 text-[13px] font-bold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {acting === 'request-execution' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}
                      Request Execution
                    </button>
                  ) : null}
                  <button
                    type="button"
                    disabled={acting !== null || !canExecute || ticket.status !== 'pending_execution'}
                    onClick={() => setConfirmAction('execute')}
                    className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {acting === 'execute' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}
                    Execute
                  </button>
                  {canReject && (ticket.status === 'approved' || ticket.status === 'pending_execution') ? (
                    <>
                      <label className="flex flex-col gap-1.5">
                        <span className="text-[12px] font-semibold text-ink">Execution rejection reason (required)</span>
                        <textarea
                          value={reason}
                          onChange={(event) => setReason(event.target.value)}
                          className="min-h-24 rounded-lg border border-border bg-panel-soft px-3 py-2 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                          disabled={acting !== null}
                        />
                      </label>

                      <button
                        type="button"
                        disabled={acting !== null || reason.trim() === ''}
                        onClick={() => void runAction('reject', () => rejectTicket(ticket.id, reason.trim()))}
                        className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg border border-danger/20 bg-red-50 px-4 text-[13px] font-bold text-danger transition hover:bg-red-100 disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        {acting === 'reject' ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldX className="h-4 w-4" />}
                        Reject at Execution Stage
                      </button>
                    </>
                  ) : null}
                  {canRevoke ? (
                    <button
                      type="button"
                      disabled={acting !== null || ticket.status !== 'approved' || ticket.ticket_type !== 'sensitive_query_access'}
                      onClick={() => setConfirmAction('revoke')}
                      className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg border border-danger/20 bg-red-50 px-4 text-[13px] font-bold text-danger transition hover:bg-red-100 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {acting === 'revoke' ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldX className="h-4 w-4" />}
                      Revoke Access
                    </button>
                  ) : null}
                </div>

              </div>
            ) : null}

            {ticket.ticket_type === 'sql_export' && detail.capabilities.can_download_export && exportDownloadURL ? (
              <div className="rounded-xl border border-border bg-panel shadow-soft">
                <div className="border-b border-border/80 px-4 py-3">
                  <div className="flex items-center gap-2">
                    <Download className="h-4 w-4 text-accent" />
                    <p className="text-[13px] font-semibold text-ink">Export Download</p>
                  </div>
                  <p className="mt-1 text-[12px] text-muted">Download the export result once the ticket is approved.</p>
                </div>
                <div className="px-4 py-4">
                  <button
                    type="button"
                    onClick={() => void handleDownloadExport()}
                    disabled={downloadingExport}
                    className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white transition hover:bg-slate-800"
                  >
                    {downloadingExport ? <Loader2 className="h-4 w-4 animate-spin" /> : <Download className="h-4 w-4" />}
                    {downloadingExport ? 'Downloading…' : 'Download Export'}
                  </button>
                  <p className="mt-3 text-[12px] text-muted">
                    Expires: {detail.export_request?.expires_at ? formatDateTime(detail.export_request.expires_at, true) : '—'}
                  </p>
                </div>
              </div>
            ) : null}
          </section>
          </div>
        </div>
      )}

      <ConfirmDialog
        open={confirmAction !== null}
        title={
          confirmAction === 'withdraw'
            ? 'Withdraw Ticket'
            : confirmAction === 'request-execution'
              ? 'Request Execution'
              : confirmAction === 'execute'
                ? 'Execute Ticket'
                : 'Revoke Sensitive Access'
        }
        description={
          confirmAction === 'withdraw'
            ? 'Withdraw this ticket now? Reviewers will no longer process it.'
            : confirmAction === 'request-execution'
            ? 'Submit this ticket to the execution queue? A DBA can trigger execution from the pending_execution state.'
            : confirmAction === 'execute'
              ? 'Trigger execution for this ticket? This will call the backend execute API.'
              : 'Revoke this sensitive access ticket early? Access will be invalidated from the next query onwards.'
        }
        confirmLabel={
          confirmAction === 'withdraw'
            ? 'Withdraw'
            : confirmAction === 'request-execution'
              ? 'Confirm'
              : confirmAction === 'execute'
                ? 'Execute'
                : 'Revoke'
        }
        loading={confirmAction !== null && acting === confirmAction}
        onCancel={() => setConfirmAction(null)}
        onConfirm={() => {
          if (!ticket) return
          if (confirmAction === 'withdraw') {
            void runAction('withdraw', () => withdrawTicket(ticket.id)).finally(() => setConfirmAction(null))
          }
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
