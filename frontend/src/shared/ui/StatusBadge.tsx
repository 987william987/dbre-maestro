import { cn } from '@/lib/utils'
import type { TicketStatus } from '@/shared/types/ticket'

const STATUS_STYLES: Record<TicketStatus, string> = {
  pending_review: 'border-amber-200 bg-amber-50 text-amber-700',
  approved: 'border-emerald-200 bg-emerald-50 text-emerald-700',
  rejected: 'border-rose-200 bg-rose-50 text-rose-700',
  pending_execution: 'border-blue-200 bg-blue-50 text-blue-700',
  executing: 'border-slate-200 bg-slate-100 text-slate-700',
  completed: 'border-emerald-200 bg-emerald-50 text-emerald-700',
  failed: 'border-rose-200 bg-rose-50 text-rose-700',
  stopped: 'border-zinc-200 bg-zinc-100 text-zinc-700',
  interrupted: 'border-orange-200 bg-orange-50 text-orange-700',
}

const STATUS_LABELS: Record<TicketStatus, string> = {
  pending_review: '待審核',
  approved: '已通過',
  rejected: '已拒絕',
  pending_execution: '待執行',
  executing: '執行中',
  completed: '已完成',
  failed: '失敗',
  stopped: '已停止',
  interrupted: '已中斷',
}

export function StatusBadge({ status }: { status: TicketStatus }) {
  return (
    <span
      className={cn(
        'inline-flex rounded-full border px-2.5 py-1 text-[10px] font-semibold tracking-[0.04em]',
        STATUS_STYLES[status],
      )}
    >
      {STATUS_LABELS[status]}
    </span>
  )
}
