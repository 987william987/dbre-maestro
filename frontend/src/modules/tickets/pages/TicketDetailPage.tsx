import { startTransition, useEffect, useRef, useState } from 'react'
import { ArrowLeft, Check, ChevronDown, Download, Loader2, Play, Send, ShieldCheck, ShieldX, X } from 'lucide-react'
import { Link, useParams } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { useAuth } from '@/shared/auth/AuthContext'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import { MAESTRO_REALTIME_EVENT } from '@/shared/realtime/events'
import type { Ticket, TicketDetail, TicketScope, TicketWorkflowParticipants } from '@/shared/types/ticket'
import type { AuditLog } from '@/shared/types/audit'
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { PageIntro } from '@/shared/ui/PageIntro'
import { StatusBadge } from '@/shared/ui/StatusBadge'
import { useToast } from '@/shared/ui/ToastContext'
import { approveTicket, downloadTicketExport, executeTicket, getTicket, rejectTicket, requestExecution, revokeTicket, withdrawTicket } from '@/modules/tickets/api'

function DetailTable({
  headers,
  rows,
}: {
  headers: string[]
  rows: Array<Array<React.ReactNode>>
}) {
  return (
    <div className="mt-3 overflow-x-auto rounded-xl border border-border">
      <table className="min-w-full border-collapse">
        <thead className="bg-panel-soft text-left text-[11px] font-semibold text-faint">
          <tr>
            {headers.map((header) => (
              <th key={header} className="px-4 py-3">
                {header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-border bg-white text-[13px] text-ink">
          {rows.map((row, rowIndex) => (
            <tr key={rowIndex}>
              {row.map((cell, cellIndex) => (
                <td key={`${rowIndex}-${cellIndex}`} className="px-4 py-3 align-top">
                  {cell}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
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

function formatExecutionDuration(startedAt?: string | null, completedAt?: string | null) {
  if (!startedAt || !completedAt) {
    return '—'
  }
  const start = new Date(startedAt).getTime()
  const end = new Date(completedAt).getTime()
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) {
    return '—'
  }
  return `${((end - start) / 1000).toFixed(3)}s`
}

function formatExecutionStage(status: string) {
  switch (status) {
    case 'pending':
      return 'Pending'
    case 'running':
      return 'Running'
    case 'completed':
      return 'Execute Successfully'
    case 'failed':
      return 'Execute Failed'
    default:
      return status
  }
}

function formatTicketTypeLabel(ticketType: string) {
  switch (ticketType) {
    case 'ddl':
      return 'DDL'
    case 'dml':
      return 'DML'
    case 'redis_command':
      return 'Redis'
    case 'sql_export':
      return 'SQL Export'
    case 'sensitive_query_access':
      return 'Sensitive Access'
    default:
      return ticketType
  }
}

function formatActivityAction(actionType: string) {
  switch (actionType) {
    case 'ticket_submit':
      return 'Submitted'
    case 'ticket_approve':
      return 'Approved'
    case 'ticket_reject':
      return 'Rejected'
    case 'ticket_withdraw':
      return 'Withdrawn'
    case 'ticket_request_execution':
      return 'Queued for Execution'
    case 'ticket_execute_start':
      return 'Execution Started'
    case 'ticket_execute_complete':
      return 'Execution Completed'
    case 'ticket_execute_failed':
      return 'Execution Failed'
    case 'ticket_revoke':
      return 'Access Revoked'
    case 'ticket_schedule':
      return 'Execution Scheduled'
    default:
      return actionType
    }
}

function parseAuditDetails(details: unknown) {
  if (!details || typeof details !== 'object' || Array.isArray(details)) {
    return null
  }
  return details as Record<string, unknown>
}

function formatActivityDetail(log: AuditLog) {
  const details = parseAuditDetails(log.details)
  switch (log.action_type) {
    case 'ticket_submit':
      return 'Ticket submitted and waiting for review.'
    case 'ticket_approve':
      return typeof details?.comment === 'string' && details.comment.trim()
        ? `Review comment: ${details.comment.trim()}`
        : 'Ticket approved.'
    case 'ticket_reject':
      return typeof details?.reason === 'string' && details.reason.trim()
        ? `Reject reason: ${details.reason.trim()}`
        : 'Ticket rejected.'
    case 'ticket_withdraw':
      return 'Ticket withdrawn by submitter.'
    case 'ticket_request_execution':
      return 'Ticket moved to the pending execution queue.'
    case 'ticket_execute_start':
      return 'Ticket execution started.'
    case 'ticket_execute_complete':
      return 'Execution result: completed successfully.'
    case 'ticket_execute_failed':
      return 'Execution result: failed.'
    case 'ticket_revoke':
      return 'Sensitive access was revoked early.'
    case 'ticket_schedule':
      return 'Ticket was scheduled for execution.'
    default:
      return details ? JSON.stringify(details) : '—'
  }
}

type StatementResultRow = {
  seq: number
  sql: string
  scanRows: number | null
  reviewStatus: string | null
  reviewMessage: string | null
  rowsAffected: number | null
  executionStatus: string | null
  currentStage: string | null
  duration: string | null
  errorMessage: string | null
}

function buildStatementResults(detail: TicketDetail) {
  const rows = new Map<number, StatementResultRow>()

  detail.review_results.forEach((result) => {
    if (result.phase && result.phase !== 'validation') {
      return
    }
    const existing = rows.get(result.seq)
    const nextMessage = [existing?.reviewMessage, result.message]
      .filter((item): item is string => Boolean(item && item.trim()))
      .join(' | ')
    rows.set(result.seq, {
      seq: result.seq,
      sql: result.sql_stmt,
      scanRows: Math.max(existing?.scanRows ?? 0, result.scan_rows),
      reviewStatus: existing?.reviewStatus === 'error' || result.status === 'error' ? 'error' : result.status,
      reviewMessage: nextMessage || null,
      rowsAffected: existing?.rowsAffected ?? null,
      executionStatus: existing?.executionStatus ?? null,
      currentStage: existing?.currentStage ?? null,
      duration: existing?.duration ?? null,
      errorMessage: existing?.errorMessage ?? null,
    })
  })

  detail.executions.forEach((execution) => {
    const existing = rows.get(execution.seq)
    rows.set(execution.seq, {
      seq: execution.seq,
      sql: existing?.sql || execution.sql_stmt,
      scanRows: existing?.scanRows ?? null,
      reviewStatus: existing?.reviewStatus ?? null,
      reviewMessage: existing?.reviewMessage ?? null,
      rowsAffected: execution.rows_affected ?? 0,
      executionStatus: execution.status,
      currentStage: formatExecutionStage(execution.status),
      duration: typeof execution.duration_ms === 'number'
        ? `${(execution.duration_ms / 1000).toFixed(3)}s`
        : formatExecutionDuration(execution.started_at, execution.completed_at),
      errorMessage: execution.error_msg ?? null,
    })
  })

  return Array.from(rows.values()).sort((a, b) => a.seq - b.seq)
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
  const usesExecutor = ticket.ticket_type === 'ddl' || ticket.ticket_type === 'dml' || ticket.ticket_type === 'redis_command'

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

function WorkflowTimeline({
  ticket,
  workflowParticipants,
  highlight,
  refreshing,
}: {
  ticket: Ticket
  workflowParticipants: TicketWorkflowParticipants
  highlight?: boolean
  refreshing?: boolean
}) {
  const steps = buildWorkflowSteps(ticket, workflowParticipants)

  return (
    <section
      className={cn(
        'rounded-xl border border-border bg-panel shadow-soft transition-all duration-300',
        highlight ? 'border-accent/30 shadow-[0_0_0_3px_rgba(59,130,246,0.10)]' : '',
      )}
    >
      <div className="border-b border-border/80 px-4 py-3">
        <div className="flex items-center justify-between gap-3">
          <p className="text-[13px] font-semibold text-ink">Approval Flow</p>
          {refreshing ? (
            <span className="inline-flex items-center gap-2 text-[11px] font-medium text-muted">
              <span className="h-2 w-2 animate-pulse rounded-full bg-accent" />
              Syncing status...
            </span>
          ) : null}
        </div>
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
  const [otherDetailsOpen, setOtherDetailsOpen] = useState(false)
  const [isRefreshing, setIsRefreshing] = useState(false)
  const [statusTransitioning, setStatusTransitioning] = useState(false)
  const previousStatusRef = useRef<string | null>(null)

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
          startTransition(() => {
            setDetail(nextDetail)
          })
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

  useEffect(() => {
    if (!id) {
      return
    }

    const handleRealtime = (event: Event) => {
      const realtimeEvent = event as CustomEvent<{ event?: string; data?: { ticket_id?: number } | null }>
      if (realtimeEvent.detail?.event !== 'ticket.updated') {
        return
      }
      if (String(realtimeEvent.detail?.data?.ticket_id ?? '') !== id) {
        return
      }
      void reloadTicket({ background: true })
    }

    window.addEventListener(MAESTRO_REALTIME_EVENT, handleRealtime)
    return () => {
      window.removeEventListener(MAESTRO_REALTIME_EVENT, handleRealtime)
    }
  }, [id])

  useEffect(() => {
    const nextStatus = detail?.ticket.status ?? null
    const previousStatus = previousStatusRef.current
    previousStatusRef.current = nextStatus

    if (!nextStatus || !previousStatus || nextStatus === previousStatus) {
      return
    }

    setStatusTransitioning(true)
    const timer = window.setTimeout(() => {
      setStatusTransitioning(false)
    }, 900)
    return () => {
      window.clearTimeout(timer)
    }
  }, [detail?.ticket.status])

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
  const statementResults = detail ? buildStatementResults(detail) : []
  const hasActionPanel = canReview || canWithdraw || canOperateDBA || canExecute || canReject || canRevoke || (ticket?.ticket_type === 'sql_export' && detail?.capabilities.can_download_export && exportDownloadURL)
  const shouldShowActionPanel = hasActionPanel && !['completed', 'failed', 'rejected', 'withdrawn', 'stopped', 'interrupted'].includes(ticket?.status ?? '')
  const showExecutionActions = (ticket?.status === 'approved' && canOperateDBA) ||
    (ticket?.status === 'pending_execution' && (canExecute || canReject)) ||
    (ticket?.ticket_type === 'sensitive_query_access' && ticket?.status === 'approved' && canRevoke)

  async function reloadTicket(options?: { background?: boolean }) {
    if (!id) {
      return
    }
    const background = options?.background === true
    if (background) {
      setIsRefreshing(true)
    }
    try {
      const nextDetail = await getTicket(id)
      startTransition(() => {
        setDetail(nextDetail)
      })
    } finally {
      if (background) {
        setIsRefreshing(false)
      }
    }
  }

  async function runAction(
    type: 'approve' | 'reject' | 'withdraw' | 'request-execution' | 'execute' | 'revoke',
    action: () => Promise<Ticket | void>,
  ) {
    setActing(type)
    setError('')
    try {
      await action()
      await reloadTicket({ background: true })
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
            {ticket ? (
              <StatusBadge
                status={ticket.status}
                className={cn(
                  statusTransitioning ? 'scale-[1.03] ring-4 ring-accent/15' : '',
                  isRefreshing ? 'opacity-80' : '',
                )}
              />
            ) : null}
            {isRefreshing ? (
              <span className="inline-flex items-center gap-2 text-[11px] font-medium text-muted">
                <span className="h-2 w-2 animate-pulse rounded-full bg-accent" />
                Updating...
              </span>
            ) : null}
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
        <div className={cn('space-y-3 transition-opacity duration-300', isRefreshing ? 'opacity-95' : 'opacity-100')}>
          <WorkflowTimeline
            ticket={ticket}
            workflowParticipants={detail.workflow_participants}
            highlight={statusTransitioning}
            refreshing={isRefreshing}
          />
          <section className="rounded-xl border border-border bg-panel shadow-soft">
            <div className="border-b border-border px-4 py-3">
              <span className="font-mono text-sm font-semibold text-accent">{ticket.ticket_no}</span>
            </div>

            <div className="px-4 py-4">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-faint">Overview</p>
              <DetailTable
                headers={['Ticket Type', 'DB Connection', 'Database', 'Submitter', 'Reviewer', 'Executor', 'Description', 'Current Status']}
                rows={[[
                  formatTicketTypeLabel(ticket.ticket_type),
                  ticket.db_connection_name || ticket.db_connection_id || 'Not specified',
                  ticket.database_name || '—',
                  formatTicketActor(ticket.submitter_name, ticket.submitter_id),
                  formatTicketActor(ticket.reviewer_name, ticket.reviewer_id ?? null),
                  formatTicketActor(ticket.executor_name, ticket.executor_id ?? null),
                  ticket.description || '—',
                  <StatusBadge status={ticket.status} />,
                ]]}
              />
            </div>

            {statementResults.length === 0 ? (
              <div className="px-4 pb-4">
                <p className="text-[11px] font-semibold uppercase tracking-wide text-faint">SQL Content</p>
                <pre className="mt-2 overflow-x-auto rounded-xl border border-border bg-panel-soft p-4 font-mono text-[13px] leading-7 text-ink">
                  <code>{ticket.sql_content}</code>
                </pre>
              </div>
            ) : (
              <div className="px-4 pb-4">
                <p className="text-[11px] font-semibold uppercase tracking-wide text-faint">Statement Results</p>
                <div className="mt-3 overflow-x-auto rounded-xl border border-border">
                  <table className="min-w-full border-collapse">
                    <thead className="bg-panel-soft text-left text-[11px] font-semibold text-faint">
                      <tr>
                        <th className="px-4 py-3">ID</th>
                        <th className="px-4 py-3">SQL</th>
                        <th className="px-4 py-3">Scan / Impact Rows</th>
                        <th className="px-4 py-3">Review Status</th>
                        <th className="px-4 py-3">Review Message</th>
                        <th className="px-4 py-3">Rows Affected</th>
                        <th className="px-4 py-3">Execution Status</th>
                        <th className="px-4 py-3">Current Stage</th>
                        <th className="px-4 py-3">Duration</th>
                        <th className="px-4 py-3">Error Message</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-border bg-white text-[13px] text-ink">
                      {statementResults.map((row) => (
                        <tr key={`${row.seq}-${row.sql}`}>
                          <td className="px-4 py-3 align-top">{row.seq}</td>
                          <td className="px-4 py-3 align-top font-mono text-[12px]">{row.sql}</td>
                          <td className="px-4 py-3 align-top">{row.scanRows ?? '—'}</td>
                          <td className="px-4 py-3 align-top">{row.reviewStatus ?? '—'}</td>
                          <td className="px-4 py-3 align-top text-muted">{row.reviewMessage || '—'}</td>
                          <td className="px-4 py-3 align-top">{row.rowsAffected ?? '—'}</td>
                          <td className="px-4 py-3 align-top">{row.executionStatus ?? '—'}</td>
                          <td className="px-4 py-3 align-top">{row.currentStage ?? '—'}</td>
                          <td className="px-4 py-3 align-top">{row.duration ?? '—'}</td>
                          <td className="px-4 py-3 align-top text-muted">{row.errorMessage || '—'}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            )}

            {shouldShowActionPanel ? (
              <div className="px-4 pb-4">
                <p className="text-[11px] font-semibold uppercase tracking-wide text-faint">Actions</p>
                <div className="mt-3">
                  {canReview && ticket.status === 'pending_review' ? (
                    <div className="p-0">
                      <div className="flex items-center gap-2">
                        <ShieldCheck className="h-4 w-4 text-accent" />
                        <p className="text-[13px] font-semibold text-ink">Review</p>
                      </div>
                      <p className="mt-1 text-[12px] text-muted">Your role can review this ticket. The backend will re-validate the state transition.</p>

                      <div className="mt-3 grid gap-3 xl:grid-cols-2">
                        <label className="flex flex-col gap-1.5">
                          <span className="text-[12px] font-semibold text-ink">Review comment (optional)</span>
                          <textarea
                            value={comment}
                            onChange={(event) => setComment(event.target.value)}
                            className="min-h-24 rounded-lg border border-border bg-white px-3 py-2 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                            disabled={acting !== null}
                          />
                          <button
                            type="button"
                            disabled={acting !== null}
                            onClick={() => void runAction('approve', () => approveTicket(ticket.id, comment))}
                            className="mt-3 inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
                          >
                            {acting === 'approve' ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldCheck className="h-4 w-4" />}
                            Approve
                          </button>
                        </label>

                        <label className="flex flex-col gap-1.5">
                          <span className="text-[12px] font-semibold text-ink">Rejection reason (required)</span>
                          <textarea
                            value={reason}
                            onChange={(event) => setReason(event.target.value)}
                            className="min-h-24 rounded-lg border border-border bg-white px-3 py-2 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                            disabled={acting !== null}
                          />
                          <button
                            type="button"
                            disabled={acting !== null || reason.trim() === ''}
                            onClick={() => void runAction('reject', () => rejectTicket(ticket.id, reason.trim()))}
                            className="mt-3 inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg border border-danger/20 bg-red-50 px-4 text-[13px] font-bold text-danger transition hover:bg-red-100 disabled:cursor-not-allowed disabled:opacity-50"
                          >
                            {acting === 'reject' ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldX className="h-4 w-4" />}
                            Reject
                          </button>
                        </label>
                      </div>
                    </div>
                  ) : null}

                  {canWithdraw && ticket.status === 'pending_review' ? (
                    <div className="p-0">
                      <div className="flex items-center gap-2">
                        <Send className="h-4 w-4 text-accent" />
                        <p className="text-[13px] font-semibold text-ink">Submission</p>
                      </div>
                      <p className="mt-1 text-[12px] text-muted">Withdraw this ticket before review starts.</p>

                      <button
                        type="button"
                        disabled={acting !== null}
                        onClick={() => setConfirmAction('withdraw')}
                        className="mt-3 inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg border border-danger/20 bg-red-50 px-4 text-[13px] font-bold text-danger transition hover:bg-red-100 disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        {acting === 'withdraw' ? <Loader2 className="h-4 w-4 animate-spin" /> : <X className="h-4 w-4" />}
                        Withdraw Ticket
                      </button>
                    </div>
                  ) : null}

                  {showExecutionActions ? (
                    <div className="p-0">
                      <div className="flex flex-col gap-2">
                        {ticket.status === 'approved' ? (
                          <button
                            type="button"
                            disabled={acting !== null || !canOperateDBA}
                            onClick={() => setConfirmAction('request-execution')}
                            className="inline-flex h-9 w-auto items-center justify-center gap-2 self-start rounded-md border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                          >
                            {acting === 'request-execution' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}
                            Request Execution
                          </button>
                        ) : null}
                        {canReject && (ticket.status === 'approved' || ticket.status === 'pending_execution') ? (
                          <>
                            <label className="flex flex-col gap-1.5">
                              <textarea
                                value={reason}
                                onChange={(event) => setReason(event.target.value)}
                                className="min-h-[96px] rounded-lg border border-border bg-white px-3 py-2 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                                placeholder="Execution rejection reason (required)"
                                disabled={acting !== null}
                              />
                            </label>
                          </>
                        ) : null}
                        {ticket.status === 'pending_execution' ? (
                          <div className="flex flex-wrap items-center gap-2">
                            <button
                              type="button"
                              disabled={acting !== null || !canExecute}
                              onClick={() => setConfirmAction('execute')}
                              className="inline-flex h-9 w-auto items-center justify-center gap-2 rounded-md bg-brand px-3 text-[12px] font-semibold text-white transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
                            >
                              {acting === 'execute' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}
                              Execute
                            </button>
                            {canReject ? (
                              <button
                                type="button"
                                disabled={acting !== null || reason.trim() === ''}
                                onClick={() => void runAction('reject', () => rejectTicket(ticket.id, reason.trim()))}
                                className="inline-flex h-9 w-auto items-center justify-center gap-2 rounded-md border border-danger/20 bg-red-50 px-3 text-[12px] font-semibold text-danger transition hover:bg-red-100 disabled:cursor-not-allowed disabled:opacity-50"
                              >
                                {acting === 'reject' ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldX className="h-4 w-4" />}
                                Reject at Execution Stage
                              </button>
                            ) : null}
                          </div>
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
                    <div className="p-0">
                      <div className="flex items-center gap-2">
                        <Download className="h-4 w-4 text-accent" />
                        <p className="text-[13px] font-semibold text-ink">Export Download</p>
                      </div>
                      <p className="mt-1 text-[12px] text-muted">Download the export result once the ticket is approved.</p>
                      <button
                        type="button"
                        onClick={() => void handleDownloadExport()}
                        disabled={downloadingExport}
                        className="mt-3 inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white transition hover:bg-slate-800"
                      >
                        {downloadingExport ? <Loader2 className="h-4 w-4 animate-spin" /> : <Download className="h-4 w-4" />}
                        {downloadingExport ? 'Downloading…' : 'Download Export'}
                      </button>
                      <p className="mt-3 text-[12px] text-muted">
                        Expires: {detail.export_request?.expires_at ? formatDateTime(detail.export_request.expires_at, true) : '—'}
                      </p>
                    </div>
                  ) : null}
                </div>
              </div>
            ) : null}

            <div className="px-4 pb-4">
              <button
                type="button"
                onClick={() => setOtherDetailsOpen((current) => !current)}
                className="inline-flex items-center gap-1.5 text-left"
                aria-expanded={otherDetailsOpen}
              >
                <span className="text-[11px] font-semibold uppercase tracking-wide text-faint">Other Details</span>
                <ChevronDown className={`h-4 w-4 text-muted transition-transform ${otherDetailsOpen ? 'rotate-180' : ''}`} />
              </button>
              {otherDetailsOpen ? (
                <DetailTable
                  headers={['Action', 'Actor', 'Timestamp', 'Detail']}
                  rows={detail.activity_logs.map((log) => [
                    formatActivityAction(log.action_type),
                    log.actor_name?.trim() ? log.actor_name : log.actor_id ? String(log.actor_id) : 'System',
                    formatDateTime(log.created_at, true),
                    formatActivityDetail(log),
                  ])}
                />
              ) : null}
            </div>

            {detail.scopes.length > 0 ? (
              <div className="px-4 pb-4">
                <p className="text-[11px] font-semibold uppercase tracking-wide text-faint">Scopes</p>
                <div className="mt-3 space-y-2">
                  {detail.scopes.map((scope) => (
                    <ScopeRow key={scope.id} scope={scope} />
                  ))}
                </div>
              </div>
            ) : null}
          </section>
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
