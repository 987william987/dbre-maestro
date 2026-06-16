import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

export function PageIntro({
  eyebrow,
  title,
  description,
  actions,
  className,
}: {
  eyebrow?: ReactNode
  title: ReactNode
  description?: ReactNode
  actions?: ReactNode
  className?: string
}) {
  return (
    <section className={cn('px-1 pb-1 pt-2', className)}>
      <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div className="min-w-0 max-w-4xl">
          {eyebrow ? <div className="mb-2 flex flex-wrap items-center gap-2 text-[11px] font-medium text-muted">{eyebrow}</div> : null}
          <h2 className="text-[28px] font-semibold tracking-[-0.04em] text-ink sm:text-[32px]">{title}</h2>
          {description ? (
            <p className="mt-2 max-w-[920px] text-[13px] leading-6 text-muted">
              {description}
            </p>
          ) : null}
        </div>
        {actions ? <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div> : null}
      </div>
    </section>
  )
}
