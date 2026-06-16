import { AlertCircle, CheckCircle2, Info } from 'lucide-react'
import { cn } from '@/lib/utils'

type InlineAlertProps = {
  tone?: 'error' | 'success' | 'info'
  children: React.ReactNode
  className?: string
}

const toneStyles = {
  error: {
    wrapper: 'border-danger/20 bg-red-50 text-danger',
    icon: AlertCircle,
  },
  success: {
    wrapper: 'border-emerald-200 bg-emerald-50 text-emerald-700',
    icon: CheckCircle2,
  },
  info: {
    wrapper: 'border-border bg-panel-soft text-muted',
    icon: Info,
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
