import { useEffect, useState } from 'react'
import { Plus } from 'lucide-react'
import { Link } from 'react-router-dom'
import { EmptyState } from '@/components/tickets/EmptyState'
import { ApiError } from '@/shared/api/client'
import { useAuth } from '@/shared/auth/AuthContext'
import { formatDateTime } from '@/shared/lib/format'
import type { Ticket, TicketStatus } from '@/shared/types/ticket'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { PageIntro } from '@/shared/ui/PageIntro'
import { Pagination } from '@/shared/ui/Pagination'
import { StatusBadge } from '@/shared/ui/StatusBadge'
import { listTickets } from '@/modules/tickets/api'

const PAGE_SIZE = 20

const STATUS_OPTIONS: Array<{ value: '' | TicketStatus; label: string }> = [
  { value: '', label: 'All' },
  { value: 'pending_review', label: 'Pending Review' },
  { value: 'approved', label: 'Approved' },
  { value: 'rejected', label: 'Rejected' },
  { value: 'pending_execution', label: 'Pending Execution' },
  { value: 'executing', label: 'Executing' },
  { value: 'completed', label: 'Completed' },
  { value: 'failed', label: 'Failed' },
  { value: 'stopped', label: 'Stopped' },
  { value: 'interrupted', label: 'Interrupted' },
]

export function TicketsPage() {
  const { user } = useAuth()
  const [tickets, setTickets] = useState<Ticket[]>([])
  const [status, setStatus] = useState<'' | TicketStatus>('')
  const [offset, setOffset] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  async function loadTickets(nextStatus: '' | TicketStatus, nextOffset: number) {
    setLoading(true)
    setError('')

    try {
      const response = await listTickets(nextStatus || undefined, PAGE_SIZE, nextOffset)
      setTickets(Array.isArray(response.tickets) ? response.tickets : [])
    } catch (loadError) {
      setError(loadError instanceof ApiError ? loadError.message : 'Failed to load tickets. Please try again.')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void loadTickets(status, offset)
  }, [status, offset])

  const canCreateTicket = user?.permissions.includes('tickets.apply') ?? false

  return (
    <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
      <PageIntro
        title="Ticket Workspace"
        description="View submitted, pending review, pending execution, and historical tickets in a single queue. Visible scope is determined by your current role and backend permissions."
        actions={
          canCreateTicket ? (
            <Link
              to="/tickets/new"
              className="inline-flex h-10 shrink-0 items-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition-colors hover:bg-slate-800"
            >
              <Plus className="h-4 w-4" />
              New Ticket
            </Link>
          ) : null
        }
      />

      <section className="rounded-xl border border-border bg-panel shadow-soft">
        <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 sm:px-5">
          <label className="flex items-center gap-2 text-[12px] text-muted">
            <span className="font-semibold text-ink">Status</span>
            <DropdownSelect
              ariaLabel="Status"
              value={status}
              onChange={(value) => {
                setStatus(value as '' | TicketStatus)
                setOffset(0)
              }}
              className="min-w-[160px]"
              size="sm"
              options={STATUS_OPTIONS.map((option) => ({
                value: option.value,
                label: option.label,
              }))}
            />
          </label>
          <p className="text-[12px] text-muted">{tickets.length} items</p>
        </div>
      </section>

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      <div className="overflow-hidden rounded-xl border border-border bg-panel shadow-soft">
        {loading ? (
          <LoadingBlock message="Loading tickets..." className="h-[320px] rounded-none border-0 bg-transparent" />
        ) : tickets.length === 0 ? (
          <div className="bg-panel">
            <EmptyState variant={status ? 'search' : 'history'} />
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="min-w-full border-collapse">
              <thead className="bg-editor-toolbar">
                <tr className="text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                  <th className="px-4 py-3">Ticket No.</th>
                  <th className="px-4 py-3">Title</th>
                  <th className="px-4 py-3">Type</th>
                  <th className="px-4 py-3">Status</th>
                  <th className="px-4 py-3">Submitter</th>
                  <th className="px-4 py-3">Created</th>
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
                        className="inline-flex rounded-md border border-transparent px-1.5 py-1 font-mono text-[12px] font-semibold text-accent transition hover:border-accent/15 hover:bg-accent-soft hover:text-blue-700"
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
                    <td className="px-4 py-3.5 align-top text-[12px] text-muted">{ticket.submitter_name || ticket.submitter_id}</td>
                    <td className="px-4 py-3.5 align-top text-[12px] text-muted">{formatDateTime(ticket.created_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <Pagination offset={offset} pageSize={PAGE_SIZE} count={tickets.length} onChange={setOffset} />
    </div>
  )
}
