import { NavLink } from 'react-router-dom'
import { cn } from '@/lib/utils'

type PageTabItem = {
  key: string
  label: string
  to?: string
  active?: boolean
  onClick?: () => void
}

export function PageTabs({
  items,
  className,
}: {
  items: PageTabItem[]
  className?: string
}) {
  return (
    <div className={cn('border-b border-border px-1', className)}>
      <div className="flex flex-wrap items-center gap-5">
        {items.map((item) => {
          const content = (
            <span
              className={cn(
                'inline-flex items-center border-b-2 px-0.5 py-3 text-[13px] font-medium transition-colors',
                item.active
                  ? 'border-ink text-ink'
                  : 'border-transparent text-muted hover:text-ink',
              )}
            >
              {item.label}
            </span>
          )

          if (item.to) {
            return (
              <NavLink key={item.key} to={item.to} end>
                {({ isActive }) => (
                  <span
                    className={cn(
                      'inline-flex items-center border-b-2 px-0.5 py-3 text-[13px] font-medium transition-colors',
                      isActive
                        ? 'border-ink text-ink'
                        : 'border-transparent text-muted hover:text-ink',
                    )}
                  >
                    {item.label}
                  </span>
                )}
              </NavLink>
            )
          }

          return (
            <button key={item.key} type="button" onClick={item.onClick}>
              {content}
            </button>
          )
        })}
      </div>
    </div>
  )
}
