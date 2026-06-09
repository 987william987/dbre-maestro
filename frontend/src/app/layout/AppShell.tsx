import { Database, FileClock, FilePlus2, LogOut, Ticket } from 'lucide-react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { useAuth } from '@/shared/auth/AuthContext'

type NavItem = {
  to: string
  label: string
  icon: typeof Ticket
  allowed: (groups: string[]) => boolean
}

const NAV_ITEMS: NavItem[] = [
  {
    to: '/tickets',
    label: 'Tickets',
    icon: Ticket,
    allowed: () => true,
  },
  {
    to: '/tickets/new',
    label: 'New Ticket',
    icon: FilePlus2,
    allowed: () => true,
  },
  {
    to: '/db-connections',
    label: 'DB Connections',
    icon: Database,
    allowed: (groups) => groups.includes('dba') || groups.includes('admin'),
  },
  {
    to: '/audit-logs',
    label: 'Audit Logs',
    icon: FileClock,
    allowed: (groups) => groups.includes('dba') || groups.includes('admin'),
  },
]

export function AppShell() {
  const { user, logout } = useAuth()
  const navigate = useNavigate()

  if (!user) {
    return null
  }

  const handleLogout = async () => {
    await logout()
    navigate('/login', { replace: true })
  }

  return (
    <div className="min-h-screen bg-page text-ink">
      <div className="mx-auto flex min-h-screen max-w-[1600px] gap-4 px-4 py-4 sm:px-5">
        <aside className="hidden w-[252px] shrink-0 rounded-card border border-border bg-sidebar p-4 shadow-soft lg:flex lg:flex-col">
          <div className="flex items-center gap-3 rounded-card border border-border bg-panel-soft px-3 py-3">
            <div className="flex h-11 w-11 items-center justify-center rounded-control bg-brand text-white shadow-soft">
              <span className="font-display text-lg font-black">M</span>
            </div>
            <div>
              <p className="text-[11px] font-bold uppercase tracking-[0.24em] text-faint">DBRE Maestro</p>
              <p className="text-sm font-semibold text-ink">治理工作台</p>
            </div>
          </div>

          <div className="mt-6">
            <p className="px-2 text-[11px] font-bold uppercase tracking-[0.2em] text-faint">Workspace</p>
            <nav className="mt-3 flex flex-col gap-1">
              {NAV_ITEMS.filter((item) => item.allowed(user.authGroups)).map((item) => {
                const Icon = item.icon
                return (
                  <NavLink
                    key={item.to}
                    to={item.to}
                    className={({ isActive }) => cn(
                      'flex items-center gap-3 rounded-control px-3 py-2.5 text-sm font-semibold transition-colors',
                      isActive
                        ? 'bg-panel-soft text-ink'
                        : 'text-muted hover:bg-panel-soft hover:text-ink',
                    )}
                  >
                    <Icon className="h-4 w-4" />
                    <span>{item.label}</span>
                  </NavLink>
                )
              })}
            </nav>
          </div>

          <div className="mt-auto rounded-card border border-border bg-panel p-3">
            <p className="text-xs font-semibold text-ink">{user.username}</p>
            <div className="mt-2 flex flex-wrap gap-1.5">
              {user.authGroups.map((group) => (
                <span
                  key={group}
                  className="rounded-pill border border-border bg-panel-soft px-2 py-1 text-[11px] font-semibold uppercase tracking-wide text-muted"
                >
                  {group}
                </span>
              ))}
            </div>
            <button
              type="button"
              onClick={handleLogout}
              className="mt-4 inline-flex w-full items-center justify-center gap-2 rounded-control border border-border bg-panel px-3 py-2 text-sm font-semibold text-ink transition-colors hover:bg-page"
            >
              <LogOut className="h-4 w-4" />
              登出
            </button>
          </div>
        </aside>

        <div className="flex min-w-0 flex-1 flex-col gap-4">
          <header className="rounded-card border border-border bg-panel px-4 py-3 shadow-soft sm:px-5">
            <div className="flex items-center justify-between gap-4">
              <div>
                <p className="text-[11px] font-bold uppercase tracking-[0.2em] text-faint">Signed In</p>
                <h1 className="mt-1 font-display text-xl font-black tracking-tight text-ink">DBRE Maestro</h1>
              </div>
              <div className="text-right">
                <p className="text-sm font-semibold text-ink">{user.username}</p>
                <p className="text-xs text-muted">目前角色：{user.authGroups.join(' / ')}</p>
              </div>
            </div>

            <nav className="mt-4 flex gap-2 overflow-x-auto pb-1 lg:hidden">
              {NAV_ITEMS.filter((item) => item.allowed(user.authGroups)).map((item) => {
                const Icon = item.icon
                return (
                  <NavLink
                    key={item.to}
                    to={item.to}
                    className={({ isActive }) => cn(
                      'inline-flex shrink-0 items-center gap-2 rounded-control border px-3 py-2 text-sm font-semibold transition-colors',
                      isActive
                        ? 'border-brand bg-brand text-white'
                        : 'border-border bg-panel-soft text-ink hover:bg-page',
                    )}
                  >
                    <Icon className="h-4 w-4" />
                    <span>{item.label}</span>
                  </NavLink>
                )
              })}
              <button
                type="button"
                onClick={handleLogout}
                className="inline-flex shrink-0 items-center gap-2 rounded-control border border-border bg-panel-soft px-3 py-2 text-sm font-semibold text-ink transition-colors hover:bg-page"
              >
                <LogOut className="h-4 w-4" />
                登出
              </button>
            </nav>
          </header>

          <main className="min-h-[calc(100vh-7rem)] min-w-0 rounded-card border border-border bg-panel shadow-soft">
            <Outlet />
          </main>
        </div>
      </div>
    </div>
  )
}
