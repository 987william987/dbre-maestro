import { AlertCircle, AlertTriangle, CheckCircle2, Info } from 'lucide-react'
import { cn } from '@/lib/utils'

type InlineAlertProps = {
  tone?: 'error' | 'success' | 'info' | 'warning'
  children: React.ReactNode
  className?: string
}

const toneStyles = {
  error: {
    wrapper: 'border-danger/20 bg-red-50 text-danger dark:bg-red-950/40',
    icon: AlertCircle,
  },
  success: {
    wrapper: 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300',
    icon: CheckCircle2,
  },
  info: {
    wrapper: 'border-border bg-panel-soft text-muted',
    icon: Info,
  },
  warning: {
    wrapper: 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-300',
    icon: AlertTriangle,
  },
} as const

export function InlineAlert({ tone = 'error', children, className }: InlineAlertProps) {
  const Icon = toneStyles[tone].icon

  return (
    <div
      className={cn(
        'flex items-start gap-2 rounded-control border px-4 py-3 text-sm',
        toneStyles[tone].wrapper,
        className,
      )}
      role="alert"
    >
      <Icon className="mt-0.5 h-4 w-4 shrink-0" />
      <div>{children}</div>
    </div>
  )
}
