import { createContext, useContext, useMemo, useState, type ReactNode } from 'react'
import { CheckCircle2, Info, XCircle } from 'lucide-react'

type ToastTone = 'success' | 'error' | 'info'

type Toast = {
  id: number
  message: string
  tone: ToastTone
  placement: 'bottom-right' | 'center'
}

type ToastContextValue = {
  pushToast: (message: string, tone?: ToastTone, options?: { placement?: 'bottom-right' | 'center'; durationMs?: number }) => void
}

const ToastContext = createContext<ToastContextValue | null>(null)

const iconMap = {
  success: CheckCircle2,
  error: XCircle,
  info: Info,
} as const

const toneMap = {
  success: 'border-emerald-200 bg-emerald-50 text-emerald-700',
  error: 'border-danger/20 bg-red-50 text-danger',
  info: 'border-border bg-panel text-ink',
} as const

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])

  const value = useMemo<ToastContextValue>(() => ({
    pushToast(message, tone = 'info', options) {
      const nextToast = {
        id: Date.now() + Math.random(),
        message,
        tone,
        placement: options?.placement ?? 'bottom-right',
      }
      setToasts((current) => [...current, nextToast])
      window.setTimeout(() => {
        setToasts((current) => current.filter((toast) => toast.id !== nextToast.id))
      }, options?.durationMs ?? 2600)
    },
  }), [])

  const centerToasts = toasts.filter((toast) => toast.placement === 'center')
  const cornerToasts = toasts.filter((toast) => toast.placement === 'bottom-right')

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div className="pointer-events-none fixed inset-x-0 top-20 z-50 flex justify-center px-4">
        <div className="flex w-[min(480px,calc(100vw-2rem))] flex-col gap-2">
          {centerToasts.map((toast) => {
            const Icon = iconMap[toast.tone]
            return (
              <div
                key={toast.id}
                className={`flex items-start gap-2 rounded-card border px-4 py-3 text-sm shadow-soft ${toneMap[toast.tone]}`}
              >
                <Icon className="mt-0.5 h-4 w-4 shrink-0" />
                <p>{toast.message}</p>
              </div>
            )
          })}
        </div>
      </div>
      <div className="pointer-events-none fixed bottom-4 right-4 z-50 flex w-[min(360px,calc(100vw-2rem))] flex-col gap-2">
        {cornerToasts.map((toast) => {
          const Icon = iconMap[toast.tone]
          return (
            <div
              key={toast.id}
              className={`flex items-start gap-2 rounded-card border px-4 py-3 text-sm shadow-soft ${toneMap[toast.tone]}`}
            >
              <Icon className="mt-0.5 h-4 w-4 shrink-0" />
              <p>{toast.message}</p>
            </div>
          )
        })}
      </div>
    </ToastContext.Provider>
  )
}

export function useToast() {
  const context = useContext(ToastContext)
  if (!context) {
    throw new Error('useToast must be used within ToastProvider')
  }
  return context
}
