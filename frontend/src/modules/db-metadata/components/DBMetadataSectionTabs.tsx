import { NavLink } from 'react-router-dom'
import { cn } from '@/lib/utils'

const ITEMS = [
  { to: '/db-metadata/inventory', label: '實例總覽' },
  { to: '/db-metadata/objects', label: '資料庫物件' },
]

export function DBMetadataSectionTabs() {
  return (
    <section className="rounded-xl border border-border bg-panel shadow-soft">
      <div className="flex flex-wrap gap-2 px-4 py-3">
        {ITEMS.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            className={({ isActive }) =>
              cn(
                'inline-flex h-10 items-center rounded-lg border px-4 text-[13px] font-semibold transition',
                isActive
                  ? 'border-slate-300 bg-slate-900 text-white'
                  : 'border-border bg-panel-soft text-ink hover:bg-page',
              )
            }
          >
            {item.label}
          </NavLink>
        ))}
      </div>
    </section>
  )
}
