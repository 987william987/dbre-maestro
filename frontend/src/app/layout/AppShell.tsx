import { useEffect, useRef, useState } from 'react'
import { ChevronDown, Database, FileClock, FilePlus2, LogOut, Settings2, ShieldAlert, ShieldCheck, SquareTerminal, Ticket, Users } from 'lucide-react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { hasAnyPermission, TICKET_WORKSPACE_PERMISSIONS } from '@/shared/auth/permissions'
import { useAuth } from '@/shared/auth/AuthContext'

type NavItem = {
  to: string
  label: string
  icon: typeof Ticket
  allowed: (permissions: string[]) => boolean
}

const NAV_ITEMS: NavItem[] = [
  {
    to: '/tickets',
    label: 'Tickets',
    icon: Ticket,
    allowed: (permissions) => hasAnyPermission(permissions, TICKET_WORKSPACE_PERMISSIONS),
  },
  {
    to: '/tickets/new',
    label: 'New Ticket',
    icon: FilePlus2,
    allowed: (permissions) => permissions.includes('tickets.apply'),
  },
  {
    to: '/sql-editor',
    label: 'SQL Editor',
    icon: SquareTerminal,
    allowed: (permissions) => permissions.includes('sql_editor.query'),
  },
  {
    to: '/users',
    label: 'Users',
    icon: Users,
    allowed: (permissions) => permissions.includes('users.read') || permissions.includes('users.write'),
  },
  {
    to: '/db-connections',
    label: 'DB Connections',
    icon: Database,
    allowed: (permissions) => permissions.includes('db_connections.read') || permissions.includes('db_connections.write'),
  },
  {
    to: '/masking-rules',
    label: 'Masking Rules',
    icon: ShieldAlert,
    allowed: (permissions) => permissions.includes('masking_rules.read') || permissions.includes('masking_rules.write'),
  },
  {
    to: '/sql-review-rules',
    label: 'SQL Review',
    icon: ShieldCheck,
    allowed: (permissions) => permissions.includes('sql_review.read') || permissions.includes('sql_review.write'),
  },
  {
    to: '/audit-logs',
    label: 'Audit Logs',
    icon: FileClock,
    allowed: (permissions) => permissions.includes('audit_logs.read') || permissions.includes('audit_logs.write'),
  },
  {
    to: '/settings',
    label: 'Settings',
    icon: Settings2,
    allowed: (permissions) => permissions.includes('settings.read') || permissions.includes('settings.write'),
  },
]

const NAV_GROUPS = [
  {
    title: 'Workbench',
    items: ['/tickets', '/tickets/new', '/sql-editor'],
  },
  {
    title: 'Governance',
    items: ['/users', '/db-connections', '/masking-rules', '/sql-review-rules', '/audit-logs', '/settings'],
  },
]

export function AppShell() {
  const { user, logout } = useAuth()
  const navigate = useNavigate()
  const [menuOpen, setMenuOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement | null>(null)

  if (!user) {
    return null
  }

  useEffect(() => {
    function handlePointerDown(event: MouseEvent) {
      if (!menuRef.current?.contains(event.target as Node)) {
        setMenuOpen(false)
      }
    }

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        setMenuOpen(false)
      }
    }

    document.addEventListener('mousedown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('mousedown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [])

  const handleLogout = async () => {
    setMenuOpen(false)
    await logout()
    navigate('/login', { replace: true })
  }

  const navItems = NAV_ITEMS.filter((item) => item.allowed(user.permissions))

  return (
    <div className="flex h-screen text-ink">
      <aside className="hidden w-64 shrink-0 flex-col border-r border-border bg-panel lg:flex">
        <div className="flex items-center gap-2.5 border-b border-border px-4 py-3.5">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-brand text-white">
            <span className="text-sm font-bold">M</span>
          </div>
          <div className="min-w-0">
            <p className="truncate text-[13px] font-semibold leading-tight text-ink">DBRE Maestro</p>
            <p className="truncate text-[11px] leading-tight text-muted">Control Plane</p>
          </div>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-3 py-4">
          {NAV_GROUPS.map((group) => {
            const items = navItems.filter((item) => group.items.includes(item.to))
            if (items.length === 0) {
              return null
            }

            return (
              <div key={group.title} className="mb-6">
                <p className="px-2 text-[11px] font-medium text-muted">{group.title}</p>
                <nav className="mt-1.5 flex flex-col gap-0.5">
                  {items.map((item) => {
                    const Icon = item.icon
                    return (
                      <NavLink key={item.to} to={item.to} className="block">
                        {({ isActive }) => (
                          <div
                            className={cn(
                              'flex items-center gap-2.5 rounded-md px-2.5 py-2 text-[13px] font-medium transition-colors',
                              isActive
                                ? 'bg-panel-soft text-ink'
                                : 'text-muted hover:bg-panel-soft/60 hover:text-ink',
                            )}
                          >
                            <Icon className="h-4 w-4 shrink-0" />
                            <span>{item.label}</span>
                          </div>
                        )}
                      </NavLink>
                    )
                  })}
                </nav>
              </div>
            )
          })}
        </div>

        <div className="border-t border-border px-4 py-3">
          <div className="flex items-center gap-2.5">
            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-panel-soft text-[12px] font-semibold uppercase text-ink">
              {user.username.slice(0, 2)}
            </div>
            <div className="min-w-0 flex-1">
              <p className="truncate text-[13px] font-semibold leading-tight text-ink">{user.username}</p>
              <p className="truncate text-[11px] leading-tight text-muted">
                {user.authGroups.length > 0 ? user.authGroups.join(', ') : 'No group'}
              </p>
            </div>
            <button
              type="button"
              onClick={handleLogout}
              title="Sign out"
              className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-panel-soft hover:text-ink"
            >
              <LogOut className="h-4 w-4" />
            </button>
          </div>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="relative z-20 shrink-0 border-b border-border bg-panel px-4 sm:px-6">
          <div className="flex h-14 items-center gap-3">
            <div className="min-w-0 flex-1">
              <p className="truncate text-[13px] font-semibold text-ink">DBRE Maestro</p>
              <p className="hidden truncate text-[11px] text-muted sm:block">Operations Control Plane</p>
            </div>

            <div ref={menuRef} className="relative">
              <button
                type="button"
                onClick={() => setMenuOpen((current) => !current)}
                className={cn(
                  'inline-flex h-8 items-center gap-1.5 rounded-md border border-border bg-panel px-3 text-[12px] font-medium text-ink transition-colors',
                  menuOpen ? 'bg-panel-soft' : 'hover:bg-panel-soft',
                )}
              >
                <span>{user.username}</span>
                <ChevronDown className={cn('h-3.5 w-3.5 text-muted transition-transform', menuOpen && 'rotate-180')} />
              </button>

              {menuOpen ? (
                <div className="absolute right-0 top-[calc(100%+0.35rem)] z-30 min-w-[200px] rounded-lg border border-border bg-panel p-1 shadow-card">
                  <div className="flex items-center justify-between rounded-md px-2 py-2">
                    <span className="text-[11px] font-medium text-muted">Account</span>
                    <span className="inline-flex items-center rounded-pill border border-border bg-panel-soft px-2 py-0.5 text-[10px] font-medium text-ink">
                      {user.username}
                    </span>
                  </div>

                  <div className="my-1 h-px bg-border" />

                  <div className="flex items-center justify-between gap-2 rounded-md px-2 py-2">
                    <span className="shrink-0 text-[11px] font-medium text-muted">Auth Groups</span>
                    <div className="flex flex-wrap justify-end gap-1">
                      {user.authGroups.length > 0 ? (
                        user.authGroups.map((group) => (
                          <span
                            key={group}
                            className="inline-flex items-center rounded-pill border border-border bg-panel-soft px-2 py-0.5 text-[10px] font-medium text-ink"
                          >
                            {group}
                          </span>
                        ))
                      ) : (
                        <span className="text-[11px] text-muted">No group</span>
                      )}
                    </div>
                  </div>

                  <div className="my-1 h-px bg-border" />

                  <button
                    type="button"
                    onClick={handleLogout}
                    className="flex h-8 w-full items-center justify-between rounded-md px-2 text-[12px] font-medium text-ink transition-colors hover:bg-panel-soft"
                  >
                    <span>Sign out</span>
                    <LogOut className="h-3.5 w-3.5 text-muted" />
                  </button>
                </div>
              ) : null}
            </div>
          </div>

          <nav className="flex gap-1.5 overflow-x-auto pb-2 lg:hidden">
            {navItems.map((item) => {
              const Icon = item.icon
              return (
                <NavLink
                  key={item.to}
                  to={item.to}
                  className={({ isActive }) => cn(
                    'inline-flex shrink-0 items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-[12px] font-medium transition-colors',
                    isActive
                      ? 'border-brand bg-brand text-white'
                      : 'border-border bg-panel text-muted hover:bg-panel-soft hover:text-ink',
                  )}
                >
                  <Icon className="h-3.5 w-3.5" />
                  <span>{item.label}</span>
                </NavLink>
              )
            })}
            <button
              type="button"
              onClick={handleLogout}
              className="inline-flex shrink-0 items-center gap-1.5 rounded-md border border-border bg-panel px-2.5 py-1.5 text-[12px] font-medium text-muted transition-colors hover:bg-panel-soft hover:text-ink"
            >
              <LogOut className="h-3.5 w-3.5" />
              登出
            </button>
          </nav>
        </header>

        <main className="min-h-0 flex-1 overflow-y-auto bg-page">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
