import { useEffect, useRef, useState } from 'react'
import { ChevronDown, Database, FileClock, FilePlus2, LogOut, Ticket } from 'lucide-react'
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

const NAV_GROUPS = [
  {
    title: 'Workbench',
    items: ['/tickets', '/tickets/new'],
  },
  {
    title: 'Governance',
    items: ['/db-connections', '/audit-logs'],
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

  const navItems = NAV_ITEMS.filter((item) => item.allowed(user.authGroups))

  return (
    <div className="min-h-screen text-ink">
      <div className="mx-auto flex min-h-screen max-w-[1680px] gap-3 px-3 py-3 sm:px-4 sm:py-4">
        <aside className="surface-enter hidden w-[220px] shrink-0 rounded-[24px] border border-white/60 bg-sidebar px-3 py-3 shadow-card backdrop-blur-xl lg:flex lg:flex-col">
          <div className="rounded-[16px] border border-white/75 bg-white/70 px-3 py-3 shadow-soft">
            <div className="flex items-center gap-2.5">
              <div className="flex h-9 w-9 items-center justify-center rounded-[12px] bg-brand text-white shadow-soft">
                <span className="font-display text-sm font-black">M</span>
              </div>
              <div>
                <p className="text-[10px] font-bold uppercase tracking-[0.22em] text-faint">DBRE Maestro</p>
                <p className="mt-0.5 text-[14px] font-bold tracking-tight text-ink">Control Plane</p>
              </div>
            </div>
            <p className="mt-2.5 text-[11px] leading-6 text-muted">
              用同一個工作區處理申請、審核、執行與稽核。
            </p>
          </div>

          <div className="mt-6">
            {NAV_GROUPS.map((group) => {
              const items = navItems.filter((item) => group.items.includes(item.to))
              if (items.length === 0) {
                return null
              }

              return (
                <div key={group.title} className="mb-5">
                  <p className="px-2 text-[10px] font-bold uppercase tracking-[0.24em] text-faint">{group.title}</p>
                  <nav className="mt-2.5 flex flex-col gap-1">
                    {items.map((item) => {
                      const Icon = item.icon
                      return (
                        <NavLink
                          key={item.to}
                          to={item.to}
                          className="block"
                        >
                          {({ isActive }) => (
                            <div
                              className={cn(
                                'group flex items-center gap-2.5 rounded-[12px] px-2.5 py-2 text-[12px] font-semibold transition-all duration-200',
                                isActive
                                  ? 'border border-white/85 bg-white text-ink shadow-soft'
                                  : 'border border-transparent text-muted hover:bg-white/75 hover:text-ink',
                              )}
                            >
                              <span
                                className={cn(
                                  'flex h-7 w-7 items-center justify-center rounded-[10px] border transition-colors',
                                  isActive
                                    ? 'border-panel-soft bg-panel-soft text-ink'
                                    : 'border-white/70 bg-panel-soft text-muted group-hover:text-ink',
                                )}
                              >
                                <Icon className="h-[14px] w-[14px]" />
                              </span>
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

        </aside>

        <div className="flex min-w-0 flex-1 flex-col gap-3">
          <header className="surface-enter relative z-20 overflow-visible rounded-[22px] border border-white/65 bg-white/78 px-4 py-3 shadow-card backdrop-blur-xl sm:px-4">
            <div className="flex items-center gap-3">
              <div className="min-w-0 flex-1 pl-1">
                <p className="text-[10px] font-bold uppercase tracking-[0.22em] text-faint">Workspace</p>
                <div className="mt-1 flex min-w-0 items-center gap-2">
                  <p className="truncate text-[13px] font-semibold tracking-tight text-ink">DBRE Maestro</p>
                  <span className="hidden text-[12px] text-faint sm:inline">/</span>
                  <p className="hidden text-[12px] text-muted sm:block">Operations Control Plane</p>
                </div>
              </div>

              <div ref={menuRef} className="relative">
                <button
                  type="button"
                  onClick={() => setMenuOpen((current) => !current)}
                  className={cn(
                    'inline-flex h-9 items-center gap-1.5 rounded-[14px] border px-3 text-[12px] font-semibold tracking-tight text-ink shadow-soft transition-all duration-150',
                    menuOpen
                      ? 'border-border bg-panel-soft shadow-[0_6px_16px_rgba(15,23,42,0.05)]'
                      : 'border-white/80 bg-white hover:border-border hover:bg-panel-soft hover:shadow-[0_8px_18px_rgba(15,23,42,0.05)]',
                  )}
                >
                  <span>{user.username}</span>
                  <ChevronDown className={cn('h-[14px] w-[14px] text-muted transition-transform', menuOpen && 'rotate-180')} />
                </button>

                {menuOpen ? (
                  <div className="absolute right-0 top-[calc(100%+0.35rem)] z-30 min-w-[184px] overflow-hidden rounded-[14px] border border-border/80 bg-[rgba(255,255,255,0.98)] p-1 shadow-[0_14px_28px_rgba(15,23,42,0.10)] backdrop-blur-xl">
                    <div className="px-1 pt-1">
                      <div className="flex items-center justify-between rounded-[10px] px-2 py-2 transition-colors hover:bg-panel-soft">
                        <span className="text-[10px] font-bold uppercase tracking-[0.14em] text-faint">Account</span>
                        <span className="inline-flex items-center rounded-full border border-border bg-white px-2 py-0.5 text-[9px] font-semibold tracking-[0.01em] text-ink">
                          {user.username}
                        </span>
                      </div>
                    </div>

                    <div className="my-1 h-px bg-border/70" />

                    <div className="px-1">
                      <div className="flex items-center justify-between rounded-[10px] px-2 py-2 transition-colors hover:bg-panel-soft">
                        <span className="text-[10px] font-bold uppercase tracking-[0.14em] text-faint">Auth Groups</span>
                        <div className="flex flex-wrap justify-end gap-1">
                          {user.authGroups.length > 0 ? (
                            user.authGroups.map((group) => (
                              <span
                                key={group}
                                className="inline-flex items-center rounded-full border border-border bg-white px-2 py-0.5 text-[9px] font-semibold tracking-[0.01em] text-ink"
                              >
                                {group}
                              </span>
                            ))
                          ) : (
                            <span className="text-[10px] font-medium text-muted">No group</span>
                          )}
                        </div>
                      </div>
                    </div>

                    <div className="my-1 h-px bg-border/70" />

                    <div className="px-1 pb-1">
                      <button
                        type="button"
                        onClick={handleLogout}
                        className="inline-flex h-9 w-full items-center justify-between rounded-[10px] border border-transparent bg-white px-2 text-[12px] font-semibold text-ink transition-colors hover:bg-panel-soft"
                      >
                        <span className="text-[10px] font-bold uppercase tracking-[0.14em] text-faint">Sign out</span>
                        <LogOut className="h-[14px] w-[14px] text-ink" />
                      </button>
                    </div>
                  </div>
                ) : null}
              </div>
            </div>

            <nav className="mt-3 flex gap-2 overflow-x-auto pb-1 lg:hidden">
              {navItems.map((item) => {
                const Icon = item.icon
                return (
                  <NavLink
                    key={item.to}
                    to={item.to}
                    className={({ isActive }) => cn(
                      'inline-flex shrink-0 items-center gap-2 rounded-[12px] border px-3 py-2 text-[13px] font-semibold transition-all',
                      isActive
                        ? 'border-brand bg-brand text-white'
                        : 'border-white/75 bg-panel-soft text-ink hover:bg-white',
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
                className="inline-flex shrink-0 items-center gap-2 rounded-[12px] border border-white/75 bg-panel-soft px-3 py-2 text-[13px] font-semibold text-ink transition-colors hover:bg-white"
              >
                <LogOut className="h-4 w-4" />
                登出
              </button>
            </nav>
          </header>

          <main className="surface-enter relative z-0 min-h-[calc(100vh-6.4rem)] min-w-0 rounded-[24px] border border-white/70 bg-white/84 shadow-card backdrop-blur-xl">
            <Outlet />
          </main>
        </div>
      </div>
    </div>
  )
}
