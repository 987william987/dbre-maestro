import { useEffect, useMemo, useRef, useState } from 'react'
import { Bell, ChevronDown, Database, DatabaseZap, FileClock, FilePlus2, LogOut, Settings2, ShieldAlert, ShieldCheck, SquareTerminal, Ticket, Users } from 'lucide-react'
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { listNotifications, markAllNotificationsRead, markNotificationRead } from '@/modules/notifications/api'
import { hasAnyPermission, TICKET_WORKSPACE_PERMISSIONS } from '@/shared/auth/permissions'
import { useAuth } from '@/shared/auth/AuthContext'
import type { NotificationItem } from '@/shared/types/notification'
import { useToast } from '@/shared/ui/ToastContext'

type NavLeafItem = {
  to: string
  label: string
  icon: typeof Ticket
}

type NavItem = {
  key: string
  label: string
  icon: typeof Ticket
  allowed: (permissions: string[]) => boolean
  to?: string
  children?: NavLeafItem[]
}

const NAV_ITEMS: NavItem[] = [
  {
    key: 'tickets',
    label: 'Tickets',
    icon: Ticket,
    allowed: (permissions) => hasAnyPermission(permissions, TICKET_WORKSPACE_PERMISSIONS),
    to: '/tickets',
    children: [
      { to: '/tickets', label: 'All Tickets', icon: Ticket },
      { to: '/tickets/new', label: 'New Ticket', icon: FilePlus2 },
    ],
  },
  {
    key: 'sql-editor',
    label: 'SQL Editor',
    icon: SquareTerminal,
    allowed: (permissions) => permissions.includes('sql_editor.query'),
    to: '/sql-editor',
  },
  {
    key: 'users',
    label: 'Users',
    icon: Users,
    allowed: (permissions) => permissions.includes('users.read') || permissions.includes('users.write'),
    to: '/users',
    children: [
      { to: '/users', label: 'Users', icon: Users },
      { to: '/users/groups', label: 'Auth Groups', icon: Users },
    ],
  },
  {
    key: 'db-connections',
    label: 'DB Connections',
    icon: Database,
    allowed: (permissions) => permissions.includes('db_connections.read') || permissions.includes('db_connections.write'),
    to: '/db-connections',
  },
  {
    key: 'db-metadata',
    label: 'DB Metadata',
    icon: DatabaseZap,
    allowed: (permissions) => permissions.includes('db_metadata.read'),
    to: '/db-metadata/inventory',
    children: [
      { to: '/db-metadata/inventory', label: 'Inventory', icon: DatabaseZap },
      { to: '/db-metadata/objects', label: 'Objects', icon: DatabaseZap },
    ],
  },
  {
    key: 'masking-rules',
    label: 'Masking Rules',
    icon: ShieldAlert,
    allowed: (permissions) => permissions.includes('masking_rules.read') || permissions.includes('masking_rules.write'),
    to: '/masking-rules',
  },
  {
    key: 'sql-review-rules',
    label: 'SQL Review',
    icon: ShieldCheck,
    allowed: (permissions) => permissions.includes('sql_review.read') || permissions.includes('sql_review.write'),
    to: '/sql-review-rules',
  },
  {
    key: 'audit-logs',
    label: 'Audit Logs',
    icon: FileClock,
    allowed: (permissions) => permissions.includes('audit_logs.read') || permissions.includes('audit_logs.write'),
    to: '/audit-logs',
  },
  {
    key: 'settings',
    label: 'Settings',
    icon: Settings2,
    allowed: (permissions) => permissions.includes('settings.read') || permissions.includes('settings.write'),
    to: '/settings',
  },
]

const NAV_GROUPS = [
  {
    title: 'Workbench',
    items: ['/tickets', '/tickets/new', '/sql-editor'],
  },
  {
    title: 'Governance',
    items: ['/users', '/db-connections', '/db-metadata/inventory', '/db-metadata/objects', '/masking-rules', '/sql-review-rules', '/audit-logs', '/settings'],
  },
]

export function AppShell() {
  const { user, logout } = useAuth()
  const location = useLocation()
  const navigate = useNavigate()
  const { pushToast } = useToast()
  const [menuOpen, setMenuOpen] = useState(false)
  const [notificationOpen, setNotificationOpen] = useState(false)
  const [expandedNavKeys, setExpandedNavKeys] = useState<string[]>([])
  const [notifications, setNotifications] = useState<NotificationItem[]>([])
  const [unreadCount, setUnreadCount] = useState(0)
  const menuRef = useRef<HTMLDivElement | null>(null)
  const notificationRef = useRef<HTMLDivElement | null>(null)
  const bootstrappedNotificationsRef = useRef(false)
  const seenNotificationIDsRef = useRef<Set<number>>(new Set())

  if (!user) {
    return null
  }

  useEffect(() => {
    function handlePointerDown(event: MouseEvent) {
      const target = event.target as Node
      if (!menuRef.current?.contains(target)) {
        setMenuOpen(false)
      }
      if (!notificationRef.current?.contains(target)) {
        setNotificationOpen(false)
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

  useEffect(() => {
    let cancelled = false

    async function loadNotifications(showToastForNew: boolean) {
      try {
        const response = await listNotifications(8, 0)
        if (cancelled) {
          return
        }

        setNotifications(response.notifications)
        setUnreadCount(response.unread)

        const nextIDs = new Set(response.notifications.map((item) => item.id))
        if (!bootstrappedNotificationsRef.current) {
          seenNotificationIDsRef.current = nextIDs
          bootstrappedNotificationsRef.current = true
          return
        }

        if (!showToastForNew) {
          seenNotificationIDsRef.current = nextIDs
          return
        }

        const newNotifications = response.notifications.filter((item) => !seenNotificationIDsRef.current.has(item.id))
        seenNotificationIDsRef.current = nextIDs

        for (const notification of newNotifications) {
          if (notification.type === 'ticket_pending_review') {
            pushToast(`有工單待審批：${notification.title}`, 'info', { placement: 'center', durationMs: 3200 })
          }
          if (notification.type === 'ticket_pending_execution') {
            pushToast(`有工單待執行：${notification.title}`, 'info', { placement: 'center', durationMs: 3200 })
          }
        }
      } catch {
        if (!bootstrappedNotificationsRef.current) {
          bootstrappedNotificationsRef.current = true
        }
      }
    }

    void loadNotifications(false)
    const timer = window.setInterval(() => {
      void loadNotifications(true)
    }, 30000)

    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [pushToast])

  const handleLogout = async () => {
    setMenuOpen(false)
    setNotificationOpen(false)
    await logout()
    navigate('/login', { replace: true })
  }

  async function handleMarkAllRead() {
    try {
      await markAllNotificationsRead()
      setNotifications((current) => current.map((item) => ({ ...item, is_read: true })))
      setUnreadCount(0)
    } catch {
      pushToast('通知標示已讀失敗。', 'error')
    }
  }

  async function handleOpenNotification(notification: NotificationItem) {
    if (!notification.is_read) {
      try {
        await markNotificationRead(notification.id)
        setNotifications((current) =>
          current.map((item) => (item.id === notification.id ? { ...item, is_read: true } : item)),
        )
        setUnreadCount((current) => Math.max(0, current - 1))
      } catch {
        pushToast('通知標示已讀失敗。', 'error')
        return
      }
    }

    setNotificationOpen(false)
    if (notification.resource_type === 'ticket' && notification.resource_id) {
      navigate(`/tickets/${notification.resource_id}`)
    }
  }

  const navItems = useMemo(
    () => NAV_ITEMS.filter((item) => item.allowed(user.permissions)),
    [user.permissions],
  )

  useEffect(() => {
    setExpandedNavKeys((current) => {
      const required = navItems
        .filter((item) => item.children?.some((child) => isPathActive(location.pathname, child.to)))
        .map((item) => item.key)
      const next = new Set([...current, ...required])
      return Array.from(next)
    })
  }, [location.pathname, navItems])

  useEffect(() => {
    setExpandedNavKeys((current) => current.filter((key) => navItems.some((item) => item.key === key)))
  }, [navItems])

  function toggleNavGroup(key: string) {
    setExpandedNavKeys((current) => (current.includes(key) ? current.filter((item) => item !== key) : [...current, key]))
  }

  return (
    <div className="flex h-screen text-ink">
      <aside className="hidden w-64 shrink-0 flex-col border-r border-border bg-panel lg:flex">
        <div className="flex h-14 items-center gap-2.5 border-b border-border px-4">
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
            const items = navItems.filter((item) => (item.to ? group.items.includes(item.to) : false))
            if (items.length === 0) {
              return null
            }

            return (
              <div key={group.title} className="mb-6">
                <p className="px-2 text-[11px] font-medium text-muted">{group.title}</p>
                <nav className="mt-2 flex flex-col gap-1">
                  {items.map((item) => {
                    const Icon = item.icon
                    const hasChildren = (item.children?.length ?? 0) > 0
                    const isExpanded = expandedNavKeys.includes(item.key)
                    const isActive = hasChildren
                      ? item.children?.some((child) => isPathActive(location.pathname, child.to)) ?? false
                      : item.to != null && isPathActive(location.pathname, item.to)

                    if (!hasChildren && item.to) {
                      return (
                        <NavLink key={item.key} to={item.to} className="block">
                          {({ isActive: linkActive }) => (
                            <div
                              className={cn(
                                'flex items-center gap-2.5 rounded-xl px-3 py-2.5 text-[13px] font-medium transition-colors',
                                linkActive
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
                    }

                    return (
                      <div key={item.key} className="rounded-xl">
                        <button
                          type="button"
                          onClick={() => toggleNavGroup(item.key)}
                          className={cn(
                            'flex w-full items-center gap-2.5 rounded-xl px-3 py-2.5 text-left text-[13px] font-medium transition-colors',
                            isActive
                              ? 'bg-panel-soft text-ink'
                              : 'text-muted hover:bg-panel-soft/60 hover:text-ink',
                          )}
                          aria-expanded={isExpanded}
                        >
                          <Icon className="h-4 w-4 shrink-0" />
                          <span className="min-w-0 flex-1 truncate">{item.label}</span>
                          <ChevronDown
                            className={cn(
                              'h-4 w-4 shrink-0 text-faint transition-transform',
                              isExpanded ? 'rotate-180' : 'rotate-0',
                            )}
                          />
                        </button>
                        <div className={cn('overflow-hidden transition-all duration-200 ease-in-out', isExpanded ? 'max-h-40' : 'max-h-0')}>
                          <div className="ml-5 mt-1 border-l border-border pb-1 pl-3">
                            <div className="flex flex-col gap-0.5">
                              {item.children?.map((child) => (
                                <NavLink key={child.to} to={child.to} className="block" end>
                                  {({ isActive: childActive }) => (
                                    <div
                                      className={cn(
                                        'flex items-center rounded-lg px-3 py-1.5 text-[12px] font-medium transition-colors',
                                        childActive
                                          ? 'bg-panel-soft text-ink'
                                          : 'text-muted hover:bg-panel-soft/60 hover:text-ink',
                                      )}
                                    >
                                      {child.label}
                                    </div>
                                  )}
                                </NavLink>
                              ))}
                            </div>
                          </div>
                        </div>
                      </div>
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

            <div ref={notificationRef} className="relative">
              <button
                type="button"
                onClick={() => setNotificationOpen((current) => !current)}
                className={cn(
                  'relative inline-flex h-8 w-8 items-center justify-center rounded-md border border-border bg-panel text-muted transition-colors',
                  notificationOpen ? 'bg-panel-soft text-ink' : 'hover:bg-panel-soft hover:text-ink',
                )}
                aria-label="Notifications"
              >
                <Bell className="h-4 w-4" />
                {unreadCount > 0 ? (
                  <span className="absolute -right-1 -top-1 inline-flex min-w-[18px] items-center justify-center rounded-full bg-danger px-1.5 py-0.5 text-[10px] font-bold leading-none text-white">
                    {unreadCount > 99 ? '99+' : unreadCount}
                  </span>
                ) : null}
              </button>

              {notificationOpen ? (
                <div className="absolute right-0 top-[calc(100%+0.35rem)] z-30 w-[360px] max-w-[calc(100vw-2rem)] overflow-hidden rounded-xl border border-border bg-panel shadow-card">
                  <div className="flex items-center justify-between border-b border-border px-4 py-3">
                    <p className="text-[15px] font-semibold text-ink">Notifications</p>
                    <button
                      type="button"
                      onClick={() => void handleMarkAllRead()}
                      disabled={unreadCount === 0}
                      className="text-[12px] font-medium text-muted transition-colors hover:text-ink disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      Mark all read
                    </button>
                  </div>

                  {notifications.length === 0 ? (
                    <div className="px-4 py-5 text-[13px] text-muted">No notifications.</div>
                  ) : (
                    <div className="max-h-[420px] overflow-y-auto">
                      {notifications.map((notification) => (
                        <button
                          key={notification.id}
                          type="button"
                          onClick={() => void handleOpenNotification(notification)}
                          className={cn(
                            'flex w-full items-start gap-3 border-b border-border px-4 py-3 text-left transition-colors last:border-b-0 hover:bg-panel-soft',
                            !notification.is_read && 'bg-white',
                          )}
                        >
                          <span className={cn('mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full', notification.is_read ? 'bg-border' : 'bg-brand')} />
                          <span className="min-w-0 flex-1">
                            <span className="flex items-start justify-between gap-3">
                              <span className="line-clamp-1 text-[14px] font-semibold text-ink">{notification.title}</span>
                              <span className="shrink-0 text-[11px] text-muted">{formatRelativeTime(notification.created_at)}</span>
                            </span>
                            <span className="mt-1 block text-[12px] leading-5 text-muted">{notification.body}</span>
                          </span>
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              ) : null}
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
              Sign out
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

function isPathActive(pathname: string, target: string) {
  if (target === '/tickets') {
    return pathname === '/tickets' || pathname.startsWith('/tickets/')
  }
  if (target === '/users') {
    return pathname === '/users'
  }
  return pathname === target || pathname.startsWith(`${target}/`)
}

function formatRelativeTime(value: string) {
  const target = new Date(value)
  const diffMs = Date.now() - target.getTime()
  if (!Number.isFinite(diffMs)) {
    return '剛剛'
  }

  const diffMinutes = Math.floor(diffMs / 60000)
  if (diffMinutes < 1) {
    return '剛剛'
  }
  if (diffMinutes < 60) {
    return `${diffMinutes}m ago`
  }

  const diffHours = Math.floor(diffMinutes / 60)
  if (diffHours < 24) {
    return `${diffHours}h ago`
  }

  const diffDays = Math.floor(diffHours / 24)
  if (diffDays === 1) {
    return 'Yesterday'
  }
  return `${diffDays}d ago`
}
