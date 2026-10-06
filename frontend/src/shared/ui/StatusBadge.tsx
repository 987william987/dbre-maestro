import { Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { TicketStatus } from '@/shared/types/ticket'

const STATUS_STYLES: Record<TicketStatus, string> = {
  pending_review: 'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-300',
  approved: 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300',
  rejected: 'border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-300',
  withdrawn: 'border-slate-200 bg-slate-100 text-slate-700 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-300',
  pending_execution: 'border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-800 dark:bg-blue-950/40 dark:text-blue-300',
  executing: 'border-slate-200 bg-slate-100 text-slate-700 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-300',
  completed: 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300',
  failed: 'border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-300',
  stopped: 'border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-300',
  interrupted: 'border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-300',
  needs_admin_attention: 'border-orange-200 bg-orange-50 text-orange-700 dark:border-orange-800 dark:bg-orange-950/40 dark:text-orange-300',
}

const STATUS_LABELS: Record<TicketStatus, string> = {
  pending_review: 'Pending Review',
  approved: 'Approved',
  rejected: 'Rejected',
  withdrawn: 'Withdrawn',
  pending_execution: 'Pending Execution',
  executing: 'Executing',
  completed: 'Completed',
  failed: 'Failed',
  stopped: 'Stopped',
  interrupted: 'Failed',
  needs_admin_attention: 'Needs Admin Attention',
}

export function StatusBadge({ status, className }: { status: TicketStatus; className?: string }) {
  const label = STATUS_LABELS[status]
  return (
    <span
      className={cn(
        'inline-flex rounded-full border px-2.5 py-1 text-[10px] font-semibold tracking-[0.04em] transition-all duration-300',
        STATUS_STYLES[status],
        className,
      )}
    >
      {status === 'executing' ? (
        <span className="inline-flex h-4 items-center gap-1">
          <Loader2 className="h-3 w-3 animate-spin" />
          <span>{label}</span>
          <span className="inline-flex w-4 items-end gap-0.5" aria-hidden="true">
            <span className="h-1 w-1 animate-bounce rounded-full bg-current [animation-duration:900ms]" />
            <span className="h-1 w-1 animate-bounce rounded-full bg-current [animation-delay:150ms] [animation-duration:900ms]" />
            <span className="h-1 w-1 animate-bounce rounded-full bg-current [animation-delay:300ms] [animation-duration:900ms]" />
          </span>
        </span>
      ) : label}
    </span>
  )
}
