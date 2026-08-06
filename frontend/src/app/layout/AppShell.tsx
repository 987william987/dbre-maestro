import { useEffect, useMemo, useRef, useState } from 'react'
import { AlertTriangle, ArrowLeft, Bell, BriefcaseBusiness, CalendarClock, ChevronDown, CircleHelp, Database, DatabaseZap, FileClock, FilePlus2, KeyRound, LayoutDashboard, LogOut, Settings2, ShieldAlert, ShieldCheck, ShieldEllipsis, SquareTerminal, Ticket, Users } from 'lucide-react'
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { listNotifications, listNotificationSummary, markAllNotificationsRead, markNotificationRead } from '@/modules/notifications/api'
import { openEventStream } from '@/shared/api/client'
import { hasAnyPermission, TICKET_WORKSPACE_PERMISSIONS } from '@/shared/auth/permissions'
import { useAuth } from '@/shared/auth/AuthContext'
import { MAESTRO_REALTIME_EVENT } from '@/shared/realtime/events'
import type { NotificationItem, NotificationSummary } from '@/shared/types/notification'
import { useToast } from '@/shared/ui/ToastContext'

type NavLeafItem = {
  to: string
  label: string
  icon: typeof Ticket
  allowed?: (permissions: string[]) => boolean
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
    key: 'dashboard',
    label: 'Dashboard',
    icon: LayoutDashboard,
    allowed: () => true,
    to: '/dashboard',
  },
  {
    key: 'tickets',
    label: 'Tickets',
    icon: Ticket,
    allowed: (permissions) => hasAnyPermission(permissions, TICKET_WORKSPACE_PERMISSIONS),
    to: '/tickets',
    children: [
      { to: '/tickets', label: 'All Tickets', icon: Ticket },
      { to: '/tickets/new', label: 'New Ticket', icon: FilePlus2, allowed: (permissions) => permissions.includes('tickets.apply') },
    ],
  },
  {
    key: 'sql-editor',
    label: 'SQL Editor',
    icon: SquareTerminal,
    allowed: (permissions) => permissions.includes('sql_editor.read'),
    to: '/sql-editor',
  },
  {
    key: 'scheduled-sql-reports',
    label: 'Scheduled Reports',
    icon: CalendarClock,
    allowed: (permissions) => permissions.includes('scheduled_sql_reports.read') || permissions.includes('scheduled_sql_reports.write'),
    to: '/scheduled-sql-reports',
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
      { to: '/users/resources', label: 'Resources', icon: Users },
      { to: '/users/query-access', label: 'Query Access', icon: Users },
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
    to: '/sql-review-rules/mysql',
    children: [
      { to: '/sql-review-rules/mysql', label: 'MySQL', icon: ShieldCheck },
      { to: '/sql-review-rules/postgresql', label: 'PostgreSQL', icon: ShieldCheck },
      { to: '/sql-review-rules/redis', label: 'Redis', icon: ShieldCheck },
    ],
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
    items: ['/dashboard', '/tickets', '/tickets/new', '/sql-editor', '/scheduled-sql-reports'],
  },
  {
    title: 'Governance',
    items: ['/users', '/db-connections', '/db-metadata/inventory', '/db-metadata/objects', '/masking-rules', '/sql-review-rules/mysql', '/audit-logs', '/settings'],
  },
]

const READ_ONLY_HEADER_NOTICE_ROUTES = [
  { match: (pathname: string) => pathname === '/tickets', read: ['tickets.read'], write: ['tickets.apply', 'tickets.review', 'tickets.execute', 'sql_editor.export_review', 'sql_editor.sensitive_review'] },
  { match: (pathname: string) => pathname.startsWith('/tickets/') && pathname !== '/tickets/new', read: ['tickets.read'], write: ['tickets.apply', 'tickets.review', 'tickets.execute', 'sql_editor.export_review', 'sql_editor.sensitive_review'] },
  { match: (pathname: string) => pathname === '/sql-editor', read: ['sql_editor.read'], write: ['sql_editor.query', 'sql_editor.export', 'sql_editor.sensitive_apply'] },
  { match: (pathname: string) => pathname === '/scheduled-sql-reports', read: ['scheduled_sql_reports.read'], write: ['scheduled_sql_reports.write'] },
  { match: (pathname: string) => pathname.startsWith('/users'), read: ['users.read'], write: ['users.write'] },
  { match: (pathname: string) => pathname === '/db-connections', read: ['db_connections.read'], write: ['db_connections.write'] },
  { match: (pathname: string) => pathname === '/masking-rules', read: ['masking_rules.read'], write: ['masking_rules.write'] },
  { match: (pathname: string) => pathname.startsWith('/sql-review-rules'), read: ['sql_review.read'], write: ['sql_review.write'] },
  { match: (pathname: string) => pathname === '/audit-logs', read: ['audit_logs.read'], write: ['audit_logs.write'] },
  { match: (pathname: string) => pathname === '/settings', read: ['settings.read'], write: ['settings.write'] },
] as const

function getReadOnlyHeaderNotice(pathname: string, permissions: string[]) {
  const route = READ_ONLY_HEADER_NOTICE_ROUTES.find((item) => item.match(pathname))
  if (!route) {
    return null
  }
  if (route.read.some((permission) => permissions.includes(permission)) && !route.write.some((permission) => permissions.includes(permission))) {
    return { label: 'Read-only mode' }
  }
  return null
}

const SIDEBAR_COLLAPSE_CURSOR = `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='24' height='24' viewBox='0 0 24 24'%3E%3Cpath d='M3 12 9 7v10Z' fill='%2318171b'/%3E%3Cpath d='m21 12-6-5v10Z' fill='%23c7c7cc'/%3E%3C/svg%3E") 12 12, pointer`
const SIDEBAR_EXPAND_CURSOR = `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='24' height='24' viewBox='0 0 24 24'%3E%3Cpath d='M3 12 9 7v10Z' fill='%23c7c7cc'/%3E%3Cpath d='m21 12-6-5v10Z' fill='%2318171b'/%3E%3C/svg%3E") 12 12, pointer`

function formatAuthMethod(method: string | undefined, provider: string | undefined) {
  const normalizedMethod = (method ?? '').trim().toLowerCase()
  const normalizedProvider = (provider ?? '').trim()
  if (normalizedMethod === 'sso') {
    return normalizedProvider ? `SSO (${normalizedProvider})` : 'SSO'
  }
  if (normalizedMethod === 'lark') {
    return 'Lark'
  }
  if (normalizedMethod === 'password') {
    return 'Password'
  }
  return normalizedMethod || 'Unknown'
}

const PAGE_HELP = [
  {
    match: (pathname: string) => pathname === '/tickets',
    title: 'All Tickets Guide',
    items: [
      'Pending Review, Pending Execution, and Needs Admin Attention tickets are pinned above completed history.',
      'Use column filters to choose visible fields; Description is hidden by default to keep the table compact.',
      'Visibility is scoped by permissions and workflow participation. For example, regular users only see their own tickets, while reviewers and executors can see tickets assigned by workflow scope.',
      'DB Connection and Database identify the target instance and database for SQL-related tickets.',
    ],
  },
  {
    match: (pathname: string) => pathname === '/tickets/new',
    title: 'New Ticket Guide',
    items: [
      'Select the ticket type first; the form adapts required fields and workflow behavior by type.',
      'DML, DDL, Redis, query access, sensitive query access, and SQL export requests all enter workflow resolution after submission.',
      'SQL review and validation run before submission where applicable, including sensitive field and Redis sensitive prefix checks.',
      'Workflow rules determine reviewers, executors, approval requirement, and whether non-production tickets can auto-execute after approval.',
      'Use the ticket list for broad multi-database or multi-table permission requests instead of SQL Editor quick requests.',
    ],
  },
  {
    match: (pathname: string) => pathname.startsWith('/tickets/') && pathname !== '/tickets/new',
    title: 'Ticket Detail Guide',
    items: [
      'The timeline shows current workflow stage, review status, execution status, and terminal result.',
      'Available actions depend on your role, workflow assignment, and the current ticket status.',
      'Review, reject, withdraw, execute, and retry actions are recorded in audit logs.',
      'Statement results are compact by default; expand SQL only when you need to inspect full statements.',
      'Debug / Resolution Trace is only shown to admin users when the ticket needs admin attention.',
    ],
  },
  {
    match: (pathname: string) => pathname === '/sql-editor',
    title: 'SQL Editor Guide',
    items: [
      'Select a DB connection and database first; object browsing and query execution are based on the active workspace context.',
      'You can open multiple sub-workspaces to work with different connections or databases without losing the current editor state.',
      'Run Query supports keyboard shortcuts: Cmd+Enter on macOS, Ctrl+Enter on Windows/Linux. The editor executes one SQL statement at a time; if multiple statements exist, highlight one statement to run only that selection.',
      'SQL result row limits and query timeout are controlled by backend settings, including separate MySQL and PostgreSQL timeout policies.',
      'Frequently used SQL can be saved and reused from saved queries.',
      'Exports are split into normal and sensitive flows. Sensitive exports always require approval; normal exports may skip approval depending on admin workflow settings.',
      'If query permission is missing, use the quick request action to create an access ticket from the current connection, database, and table context.',
      'Quick access requests only cover the current context. For multiple databases or tables, submit a dedicated ticket from the ticket workflow.',
      'Temporary sensitive-data access is also generated from the current context and enters the approval workflow automatically.',
      'Successful executions are recorded in history and audit logs when the operation reaches the backend. History shows only your own latest 20 query records.',
    ],
  },
  {
    match: (pathname: string) => pathname === '/scheduled-sql-reports',
    title: 'Scheduled SQL Reports Guide',
    items: [
      'Scheduled reports run approved read-only SQL on a cron schedule and deliver CSV files to selected Lark users.',
      'Only SELECT, WITH, and SHOW statements are accepted when saving a report.',
      'Sensitive columns are rejected during save; use ticket/export workflows for sensitive data.',
      'Recipients must be selected explicitly, and report execution follows the configured connection and database context.',
    ],
  },
  {
    match: (pathname: string) => pathname.startsWith('/users'),
    title: 'User Management Guide',
    items: [
      'Manage users, auth groups, direct permissions, database scopes, resources, and query access rules.',
      'Permission changes take effect only after saving and confirming the summary.',
      'Protected admin users have stricter guardrails for password, MFA, active status, auth group, direct permission, and DB scope changes.',
      'Lark OAuth can bind users by enterprise email and open_id, but does not grant elevated DBA/admin permissions automatically.',
    ],
  },
  {
    match: (pathname: string) => pathname === '/db-connections',
    title: 'DB Connections Guide',
    items: [
      'DB connections support MySQL, PostgreSQL, and Redis endpoints.',
      'Readonly credentials are used by SQL Editor and metadata browsing; readwrite credentials are used for ticket execution and validation that requires writes.',
      'Leave database empty when you want SQL Editor to browse available databases automatically.',
      'Host policy can warn or enforce allowlist/denylist checks based on deployment settings.',
    ],
  },
  {
    match: (pathname: string) => pathname === '/db-metadata/inventory',
    title: 'Inventory Guide',
    items: [
      'Inventory shows AWS inventory snapshots rather than real-time live status.',
      'Mapping is based on exact matches between discovered endpoints and DB Connection host values.',
      'Use column filters to hide noisy fields while keeping cluster, instance, endpoint, mapping, and sync fields available.',
      'Missing inventory usually means IAM discovery permission, region, or inventory sync errors need to be checked in logs.',
    ],
  },
  {
    match: (pathname: string) => pathname === '/db-metadata/objects',
    title: 'Objects Guide',
    items: [
      'Objects shows scheduled metadata snapshots for MySQL and PostgreSQL databases.',
      'The page does not query live object metadata on demand; results depend on the latest sync job.',
      'Connection-level failures can come from database permissions, schema visibility, or snapshot write errors.',
      'Use filters and visible columns to focus on database/schema/table size and sync timestamp.',
    ],
  },
  {
    match: (pathname: string) => pathname === '/masking-rules',
    title: 'Mask DSL Guide',
    href: '/masking-rules/dsl-guide',
    items: [],
  },
  {
    match: (pathname: string) => pathname === '/masking-rules/dsl-guide',
    title: 'Mask DSL Guide',
    items: [
      'Use column_name, match_type, mask_mode, and mask_config to define a masking rule payload.',
      'Use exact match for one known column name, and regex when one rule should match multiple related columns.',
      'mask_config is JSON; modes without parameters can use an empty object.',
      'This guide is reference material for DBAs before creating or reviewing masking rules.',
    ],
  },
  {
    match: (pathname: string) => pathname.startsWith('/sql-review-rules'),
    title: 'SQL Review Rules Guide',
    items: [
      'SQL review rules control parser and validation checks by engine.',
      'MySQL review rules are implemented; PostgreSQL and Redis tabs are reserved for follow-up rule expansion.',
      'Rules can enforce create/alter safety requirements, reject risky statements, and configure thresholds.',
      'Enabled rules affect ticket review and validation before a workflow proceeds.',
    ],
  },
  {
    match: (pathname: string) => pathname === '/audit-logs',
    title: 'Audit Logs Guide',
    items: [
      'Audit logs record logins, tickets, exports, query attempts, permission changes, and platform configuration changes.',
      'Use common filters for action type, actor, resource, status, and date ranges.',
      'Complex details are available from the row detail panel instead of being expanded in the table.',
      'Sensitive or blocked access attempts can still be audited even when history entries are not created.',
    ],
  },
  {
    match: (pathname: string) => pathname === '/settings',
    title: 'Platform Settings Guide',
    items: [
      'Settings control SQL Editor timeout policy, workflow behavior, Lark integration, and DB metadata scan settings.',
      'AWS access still relies on the runtime IAM role; database credentials are managed in DB Connections.',
      'Production-sensitive controls should be reviewed carefully because they affect workflow execution and access policy.',
      'Save changes only after reviewing validation messages and the resulting configuration.',
    ],
  },
  {
    match: (pathname: string) => pathname === '/account/sessions',
    title: 'Account Sessions Guide',
    items: [
      'Review active refresh sessions for your account.',
      'Revoke sessions you no longer recognize or no longer use.',
      'Revoking all other sessions keeps the current browser session active.',
      'Revoking the current session signs you out and returns you to login.',
    ],
  },
  {
    match: (pathname: string) => pathname === '/account/access-scopes',
    title: 'Access Scopes Guide',
    items: [
      'Submission DB Scope lists the database connections where you can submit workflow tickets.',
      'Query Access Scope lists your active query permissions, including permissions inherited from auth groups.',
      'Expiring access is highlighted so you can renew before it expires.',
      'Use Request access to submit a query access ticket when a database or table is missing.',
    ],
  },
]

function findNavGroupTitle(pathname: string, items: NavItem[]) {
  for (const group of NAV_GROUPS) {
    const groupItems = items.filter((item) => (item.to ? group.items.includes(item.to) : false))
    for (const item of groupItems) {
      if (item.children?.some((child) => isPathActive(pathname, child.to))) {
        return group.title
      }
      if (item.to != null && isPathActive(pathname, item.to)) {
        return group.title
      }
    }
  }
  return 'DBRE Maestro'
}

function groupIcon(title: string) {
  switch (title) {
    case 'Workbench':
      return BriefcaseBusiness
    case 'Governance':
      return ShieldEllipsis
    default:
      return BriefcaseBusiness
  }
}

function findActiveNavChild(pathname: string, item: NavItem) {
  if (!item.children) {
    return null
  }
  return [...item.children]
    .sort((left, right) => right.to.length - left.to.length)
    .find((child) => isPathActive(pathname, child.to)) ?? null
}

export function AppShell() {
  const { user, logout } = useAuth()
  const location = useLocation()
  const navigate = useNavigate()
  const { pushToast } = useToast()
  const [menuOpen, setMenuOpen] = useState(false)
  const [notificationOpen, setNotificationOpen] = useState(false)
  const [pageHelpOpen, setPageHelpOpen] = useState(false)
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => {
    try {
      return window.localStorage.getItem('dbre-maestro.sidebarCollapsed') === 'true'
    } catch {
      return false
    }
  })
  const [expandedNavKeys, setExpandedNavKeys] = useState<string[]>([])
  const [notifications, setNotifications] = useState<NotificationItem[]>([])
  const [notificationSummary, setNotificationSummary] = useState<NotificationSummary>({ pending: 0, review_required: 0, execution_required: 0 })
  const [unreadCount, setUnreadCount] = useState(0)
  const notificationActionCount = notificationSummary.pending + notificationSummary.review_required + notificationSummary.execution_required
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
        const [response, summary] = await Promise.all([
          listNotifications(10, 0),
          listNotificationSummary(),
        ])
        if (cancelled) {
          return
        }

        setNotifications(response.notifications)
        setNotificationSummary(summary)
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
            pushToast(`Ticket pending review: ${notification.title}`, 'info', { placement: 'center', durationMs: 3200 })
          }
          if (notification.type === 'ticket_pending_execution') {
            pushToast(`Ticket pending execution: ${notification.title}`, 'info', { placement: 'center', durationMs: 3200 })
          }
        }
      } catch {
        if (!bootstrappedNotificationsRef.current) {
          bootstrappedNotificationsRef.current = true
        }
      }
    }

    void loadNotifications(false)
    const stopStream = openEventStream('/events/stream', {
      onEvent: (message) => {
        window.dispatchEvent(new CustomEvent(MAESTRO_REALTIME_EVENT, { detail: message }))
        if (message.event === 'notification.created') {
          void loadNotifications(true)
        } else if (message.event === 'ticket.updated') {
          void listNotificationSummary().then(setNotificationSummary).catch(() => undefined)
        }
      },
    })

    return () => {
      cancelled = true
      stopStream()
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
      pushToast('Failed to mark notifications as read.', 'error')
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
        pushToast('Failed to mark notifications as read.', 'error')
        return
      }
    }

    setNotificationOpen(false)
    if (notification.resource_type === 'ticket' && notification.resource_ref) {
      navigate(`/tickets/${notification.resource_ref}`)
    }
  }

  function openTicketQueue() {
    setNotificationOpen(false)
    navigate('/tickets')
  }

  const navItems = useMemo(
    () =>
      NAV_ITEMS
        .filter((item) => item.allowed(user.permissions))
        .map((item) => ({
          ...item,
          children: item.children?.filter((child) => child.allowed == null || child.allowed(user.permissions)),
        })),
    [user.permissions],
  )
  const activeNavItem = useMemo(
    () =>
      navItems.find((item) => {
        if (findActiveNavChild(location.pathname, item)) {
          return true
        }
        return item.to != null && isPathActive(location.pathname, item.to)
      }) ?? null,
    [location.pathname, navItems],
  )
  const activeNavChild = useMemo(
    () => (activeNavItem ? findActiveNavChild(location.pathname, activeNavItem) : null),
    [activeNavItem, location.pathname],
  )
  const currentSectionTitle = useMemo(
    () => findNavGroupTitle(location.pathname, navItems),
    [location.pathname, navItems],
  )
  const CurrentSectionIcon = groupIcon(currentSectionTitle)
  const pageHelp = useMemo(
    () => PAGE_HELP.find((help) => help.match(location.pathname)) ?? null,
    [location.pathname],
  )
  const breadcrumbItems = useMemo(() => {
    const items = ['DBRE Maestro']
    if (activeNavItem?.label) {
      items.push(activeNavItem.label)
    }
    if (activeNavChild?.label && activeNavChild.label !== activeNavItem?.label) {
      items.push(activeNavChild.label)
    }
    return items
  }, [activeNavChild, activeNavItem])
  const headerNotice = useMemo(() => {
    return getReadOnlyHeaderNotice(location.pathname, user.permissions)
  }, [location.pathname, user.permissions])
  const authMethodLabel = useMemo(() => formatAuthMethod(user.authMethod, user.authProvider), [user.authMethod, user.authProvider])

  useEffect(() => {
    setExpandedNavKeys((current) => {
      const required = navItems
        .filter((item) => findActiveNavChild(location.pathname, item))
        .map((item) => item.key)
      const next = new Set([...current, ...required])
      return Array.from(next)
    })
  }, [location.pathname, navItems])

  useEffect(() => {
    setExpandedNavKeys((current) => current.filter((key) => navItems.some((item) => item.key === key)))
  }, [navItems])

  useEffect(() => {
    setPageHelpOpen(false)
  }, [location.pathname])

  useEffect(() => {
    if (!pageHelpOpen) {
      return undefined
    }
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        setPageHelpOpen(false)
      }
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [pageHelpOpen])

  useEffect(() => {
    try {
      window.localStorage.setItem('dbre-maestro.sidebarCollapsed', sidebarCollapsed ? 'true' : 'false')
    } catch {
      // Ignore storage failures; the sidebar still works for the current session.
    }
  }, [sidebarCollapsed])

  function toggleNavGroup(key: string) {
    setExpandedNavKeys((current) => (current.includes(key) ? current.filter((item) => item !== key) : [...current, key]))
  }

  return (
    <div className="flex h-screen text-ink">
      <aside className={cn(
        'group/sidebar relative hidden shrink-0 flex-col border-r border-border bg-sidebar transition-[width] duration-[360ms] ease-[cubic-bezier(0.22,1,0.36,1)] lg:flex',
        sidebarCollapsed ? 'w-[72px]' : 'w-64',
      )}>
        <button
          type="button"
          onClick={() => setSidebarCollapsed((current) => !current)}
          aria-label={sidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}
          title={sidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}
          className="absolute -right-2 top-0 z-30 hidden h-full w-4 focus-visible:outline-none lg:block before:absolute before:left-1/2 before:top-0 before:h-full before:w-[2px] before:-translate-x-1/2 before:rounded-full before:bg-ink/20 before:opacity-0 before:transition-opacity before:duration-150 hover:before:opacity-100 focus-visible:before:opacity-100"
          style={{ cursor: sidebarCollapsed ? SIDEBAR_EXPAND_CURSOR : SIDEBAR_COLLAPSE_CURSOR }}
        />

        <div className={cn('flex h-16 items-center gap-3 px-4 transition-all duration-[360ms] ease-[cubic-bezier(0.22,1,0.36,1)]', sidebarCollapsed && 'justify-center px-3')}>
          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-border bg-transparent">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="h-4 w-4">
              <path d="M3 20 L3 4 L12 13 L21 4 L21 20" />
            </svg>
          </div>
          <div className={cn(
            'min-w-0 flex-1 overflow-hidden whitespace-nowrap transition-[max-width,opacity,transform] ease-out',
            sidebarCollapsed
              ? 'max-w-0 -translate-x-1 opacity-0 duration-[180ms]'
              : 'max-w-[172px] translate-x-0 opacity-100 delay-100 duration-300',
          )}>
            <p className="truncate text-[14px] font-semibold leading-tight text-ink">DBRE Maestro</p>
            <p className="truncate text-[12px] leading-tight text-muted">Operations Control Plane</p>
          </div>
        </div>

        <div className={cn('min-h-0 flex-1 overflow-y-auto py-4 transition-[padding] duration-[360ms] ease-[cubic-bezier(0.22,1,0.36,1)]', sidebarCollapsed ? 'px-2' : 'px-3')}>
          {NAV_GROUPS.map((group) => {
            const items = navItems.filter((item) => (item.to ? group.items.includes(item.to) : false))
            if (items.length === 0) {
              return null
            }

            return (
              <div key={group.title} className={cn('mb-6 transition-[margin] duration-[360ms] ease-[cubic-bezier(0.22,1,0.36,1)]', sidebarCollapsed && 'mb-3')}>
                <p
                  className={cn(
                    'overflow-hidden whitespace-nowrap px-2 text-[11px] font-medium text-muted transition-[max-height,opacity,transform] ease-out',
                    sidebarCollapsed ? 'max-h-0 -translate-x-1 opacity-0 duration-[180ms]' : 'max-h-5 translate-x-0 opacity-100 delay-100 duration-300',
                  )}
                >
                  {group.title}
                </p>
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
                        <NavLink key={item.key} to={item.to} aria-label={item.label} title={sidebarCollapsed ? item.label : undefined} className="group/navitem relative block">
                          {({ isActive: linkActive }) => (
                            <>
                              <div
                                className={cn(
                                  'flex h-10 items-center rounded-xl text-[13px] font-medium transition-[padding,color,background-color,box-shadow] duration-[360ms] ease-[cubic-bezier(0.22,1,0.36,1)]',
                                  sidebarCollapsed ? 'justify-center px-0' : 'gap-2.5 px-3',
                                  linkActive
                                    ? 'bg-panel-soft text-ink'
                                    : 'text-muted hover:bg-panel-soft/60 hover:text-ink',
                                )}
                              >
                                <Icon className="h-4 w-4 shrink-0" />
                                <span className={cn(
                                  'min-w-0 flex-1 overflow-hidden whitespace-nowrap transition-[max-width,opacity,transform] ease-out',
                                  sidebarCollapsed ? 'max-w-0 -translate-x-1 opacity-0 duration-[180ms]' : 'max-w-[170px] translate-x-0 opacity-100 delay-100 duration-300',
                                )}>
                                  {item.label}
                                </span>
                              </div>
                              <span className={cn(
                                'pointer-events-none absolute left-[calc(100%+10px)] top-1/2 z-40 -translate-y-1/2 whitespace-nowrap rounded-md bg-ink px-2.5 py-1.5 text-[12px] font-medium text-white opacity-0 shadow-card transition-opacity',
                                sidebarCollapsed ? 'group-hover/navitem:opacity-100 group-focus-visible/navitem:opacity-100' : 'hidden',
                              )}>
                                {item.label}
                              </span>
                            </>
                          )}
                        </NavLink>
                      )
                    }

                    return (
                      <div key={item.key} className="rounded-xl">
                        <button
                          type="button"
                          onClick={() => {
                            if (sidebarCollapsed && item.to) {
                              navigate(item.to)
                              return
                            }
                            toggleNavGroup(item.key)
                          }}
                          className={cn(
                            'group/navitem relative flex h-10 w-full items-center rounded-xl text-left text-[13px] font-medium transition-[padding,color,background-color,box-shadow] duration-[360ms] ease-[cubic-bezier(0.22,1,0.36,1)]',
                            sidebarCollapsed ? 'justify-center px-0' : 'gap-2.5 px-3',
                            isActive
                              ? 'bg-panel-soft text-ink'
                              : 'text-muted hover:bg-panel-soft/60 hover:text-ink',
                          )}
                          aria-expanded={isExpanded}
                          aria-label={item.label}
                          title={sidebarCollapsed ? item.label : undefined}
                        >
                          <Icon className="h-4 w-4 shrink-0" />
                          <span className={cn(
                            'min-w-0 flex-1 overflow-hidden whitespace-nowrap transition-[max-width,opacity,transform] ease-out',
                            sidebarCollapsed ? 'max-w-0 -translate-x-1 opacity-0 duration-[180ms]' : 'max-w-[150px] translate-x-0 opacity-100 delay-100 duration-300',
                          )}>
                            {item.label}
                          </span>
                          <ChevronDown
                            className={cn(
                              'h-4 w-4 shrink-0 text-faint transition-[max-width,opacity,transform] ease-out',
                              sidebarCollapsed ? 'max-w-0 opacity-0 duration-[180ms]' : 'max-w-4 opacity-100 delay-100 duration-300',
                              isExpanded ? 'rotate-180' : 'rotate-0',
                            )}
                          />
                          <span className={cn(
                            'pointer-events-none absolute left-[calc(100%+10px)] top-1/2 z-40 -translate-y-1/2 whitespace-nowrap rounded-md bg-ink px-2.5 py-1.5 text-[12px] font-medium text-white opacity-0 shadow-card transition-opacity',
                            sidebarCollapsed ? 'group-hover/navitem:opacity-100 group-focus-visible/navitem:opacity-100' : 'hidden',
                          )}>
                            {item.label}
                          </span>
                        </button>
                        <div className={cn(
                          'overflow-hidden transition-[max-height,opacity] ease-in-out',
                          !sidebarCollapsed && isExpanded ? 'max-h-40 opacity-100 delay-100 duration-300' : 'max-h-0 opacity-0 duration-[180ms]',
                        )}>
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

        <div className={cn('border-t border-border py-3 transition-[padding] duration-[360ms] ease-[cubic-bezier(0.22,1,0.36,1)]', sidebarCollapsed ? 'px-2' : 'px-4')}>
          <div className={cn('flex items-center gap-2.5 transition-all duration-[360ms] ease-[cubic-bezier(0.22,1,0.36,1)]', sidebarCollapsed && 'flex-col gap-2')}>
            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-panel-soft text-[12px] font-semibold uppercase text-ink">
              {user.username.slice(0, 2)}
            </div>
            <div className={cn(
              'min-w-0 flex-1 overflow-hidden whitespace-nowrap transition-[max-width,opacity,transform] ease-out',
              sidebarCollapsed ? 'max-w-0 -translate-x-1 opacity-0 duration-[180ms]' : 'max-w-[150px] translate-x-0 opacity-100 delay-100 duration-300',
            )}>
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
        <header className="relative z-20 shrink-0 border-b border-border bg-panel">
          <div className="flex h-16 items-center gap-3 border-b border-border px-4 sm:px-6">
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2.5">
                <span className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-border bg-panel-soft text-ink">
                  <CurrentSectionIcon className="h-4 w-4" />
                </span>
                <p className="truncate text-[15px] font-medium text-ink">{currentSectionTitle}</p>
              </div>
            </div>

            <div ref={notificationRef} className="relative">
              <button
                type="button"
                onClick={() => setNotificationOpen((current) => !current)}
                className={cn(
                  'relative inline-flex h-8 w-8 items-center justify-center rounded-md border border-border bg-panel text-muted transition-colors',
                  notificationOpen ? 'bg-panel-soft text-ink' : 'hover:bg-panel-soft hover:text-ink',
                )}
                aria-label={`Notifications${notificationActionCount > 0 ? `, ${notificationActionCount} pending actions` : ''}`}
              >
                <Bell className="h-4 w-4" />
                {notificationActionCount > 0 ? (
                  <span className="absolute -right-1 -top-1 inline-flex min-w-[18px] items-center justify-center rounded-full bg-danger px-1.5 py-0.5 text-[10px] font-bold leading-none text-white">
                    {notificationActionCount > 99 ? '99+' : notificationActionCount}
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

                  <div className="flex items-center gap-4 border-b border-border bg-panel-soft px-4 py-2 text-[12px] leading-5">
                    <NotificationSummaryItem label="Pending" count={notificationSummary.pending} onClick={openTicketQueue} />
                    <NotificationSummaryItem label="Review" count={notificationSummary.review_required} onClick={openTicketQueue} />
                    <NotificationSummaryItem label="Execute" count={notificationSummary.execution_required} onClick={openTicketQueue} />
                  </div>

                  {notifications.length === 0 ? (
                    <div className="px-4 py-5 text-[13px] text-muted">No notifications.</div>
                  ) : (
                    <div className="max-h-[420px] overflow-auto">
                      {notifications.map((notification) => (
                        <button
                          key={notification.id}
                          type="button"
                          onClick={() => void handleOpenNotification(notification)}
                          className={cn(
                            'grid min-w-full w-max grid-cols-[auto_max-content_max-content_max-content] items-center gap-3 border-b border-border px-4 py-2.5 text-left transition-colors last:border-b-0 hover:bg-panel-soft',
                            !notification.is_read && 'bg-white',
                          )}
                        >
                          <span className={cn('h-2.5 w-2.5 shrink-0 rounded-full', notification.is_read ? 'bg-border' : 'bg-brand')} />
                          <span className="whitespace-nowrap text-[12px] leading-5 text-ink">{formatNotificationResource(notification)}</span>
                          <span className="whitespace-nowrap text-[12px] leading-5 text-ink">{formatNotificationType(notification)}</span>
                          <span className="whitespace-nowrap text-[11px] text-muted">{formatRelativeTime(notification.created_at)}</span>
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
                    <span className="shrink-0 text-[11px] font-medium text-muted">Signed in via</span>
                    <span className="inline-flex items-center rounded-pill border border-border bg-panel-soft px-2 py-0.5 text-[10px] font-medium text-ink">
                      {authMethodLabel}
                    </span>
                  </div>

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
                    onClick={() => {
                      setMenuOpen(false)
                      navigate('/account/access-scopes')
                    }}
                    className="flex h-8 w-full items-center justify-between rounded-md px-2 text-[12px] font-medium text-ink transition-colors hover:bg-panel-soft"
                  >
                    <span>Access Scopes</span>
                    <KeyRound className="h-3.5 w-3.5 text-muted" />
                  </button>

                  <button
                    type="button"
                    onClick={() => {
                      setMenuOpen(false)
                      navigate('/account/sessions')
                    }}
                    className="flex h-8 w-full items-center justify-between rounded-md px-2 text-[12px] font-medium text-ink transition-colors hover:bg-panel-soft"
                  >
                    <span>Sessions</span>
                    <ShieldCheck className="h-3.5 w-3.5 text-muted" />
                  </button>

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

          <div className="hidden h-14 items-center justify-between gap-4 px-4 sm:px-6 lg:flex">
            <div className="flex min-w-0 items-center gap-3 text-[14px]">
              <div className="flex min-w-0 items-center gap-3 overflow-x-auto">
                {breadcrumbItems.map((item, index) => (
                  <div key={`${item}-${index}`} className="flex shrink-0 items-center gap-3">
                    {index > 0 ? <span className="text-muted">/</span> : null}
                    <span className={cn(index === breadcrumbItems.length - 1 ? 'font-medium text-ink' : 'text-muted')}>
                      {item}
                    </span>
                  </div>
                ))}
              </div>
              {pageHelp ? (
                <div className="relative shrink-0">
                  <button
                    type="button"
                    aria-label={`Show ${pageHelp.title}`}
                    aria-expanded={pageHelp.href ? undefined : pageHelpOpen}
                    onClick={() => {
                      if (pageHelp.href) {
                        navigate(pageHelp.href)
                        return
                      }
                      setPageHelpOpen((current) => !current)
                    }}
                    className={cn(
                      'inline-flex h-7 w-7 items-center justify-center text-muted transition-colors',
                      !pageHelp.href && pageHelpOpen ? 'text-ink' : 'hover:text-ink',
                    )}
                  >
                    <CircleHelp className="h-4 w-4" />
                  </button>
                  {!pageHelp.href && pageHelpOpen ? (
                    <div className="absolute left-0 top-[calc(100%+8px)] z-30 max-h-[min(640px,calc(100vh-9rem))] w-[780px] max-w-[calc(100vw-2rem)] overflow-y-auto rounded-xl border border-border bg-white p-4 text-left shadow-[0_22px_45px_rgba(15,23,42,0.14)]">
                      <div className="flex items-start justify-between gap-3">
                        <div>
                          <p className="text-[13px] font-semibold text-ink">{pageHelp.title}</p>
                          <p className="mt-1 text-[12px] leading-5 text-muted">Quick notes for this workspace.</p>
                        </div>
                      </div>
                      <ul className="mt-3 grid gap-2 text-[12px] leading-5 text-muted">
                        {pageHelp.items.map((item) => (
                          <li key={item} className="flex gap-2">
                            <span className="mt-2 h-1 w-1 shrink-0 rounded-full bg-faint" />
                            <span>{item}</span>
                          </li>
                        ))}
                      </ul>
                    </div>
                  ) : null}
                </div>
              ) : null}
              {location.pathname === '/masking-rules/dsl-guide' ? (
                <button
                  type="button"
                  onClick={() => navigate('/masking-rules')}
                  className="inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md border border-border bg-white px-2 text-[12px] font-semibold text-ink transition hover:bg-panel-soft"
                >
                  <ArrowLeft className="h-3.5 w-3.5" />
                  Back To Rules
                </button>
              ) : null}
            </div>
            {headerNotice ? (
              <div className="inline-flex h-8 shrink-0 items-center gap-2 rounded-lg border border-amber-200 bg-amber-50 px-3 text-[12px] font-medium text-amber-800">
                <AlertTriangle className="h-3.5 w-3.5" />
                {headerNotice.label}
              </div>
            ) : null}
          </div>

          <nav className="flex gap-1.5 overflow-x-auto px-4 pb-3 pt-2 lg:hidden">
            {navItems.map((item) => {
              if (!item.to) {
                return null
              }
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

        <main className="min-h-0 flex-1 overflow-y-auto bg-white">
          <Outlet />
        </main>
      </div>
    </div>
  )
}

function NotificationSummaryItem({ label, count, onClick }: { label: string; count: number; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="inline-flex items-center gap-1.5 rounded-md px-1.5 py-1 text-muted transition-colors hover:bg-panel hover:text-ink"
    >
      <span>{label}</span>
      <span className="font-semibold text-ink">{count}</span>
    </button>
  )
}

function formatNotificationResource(notification: NotificationItem) {
  return notification.resource_ref || notification.title || notification.body.split('\n').find((line) => line.trim() !== '')?.trim() || notification.type
}

function formatNotificationType(notification: NotificationItem) {
  if (notification.type === 'ticket_executed' && notification.title.includes('失敗')) {
    return 'Execution failed'
  }

  switch (notification.type) {
    case 'ticket_submitted':
      return 'Submitted'
    case 'ticket_pending_review':
      return 'Pending review'
    case 'ticket_auto_approved':
      return 'Auto approved'
    case 'ticket_pending_execution':
      return 'Pending execution'
    case 'ticket_approved':
      return 'Approved'
    case 'ticket_rejected':
      return 'Rejected'
    case 'ticket_executed':
      return 'Completed'
    case 'ticket_execution_failed':
      return 'Execution failed'
    case 'ticket_needs_admin_attention':
      return 'Needs admin attention'
    case 'ticket_revoked':
      return 'Revoked'
    case 'ticket_withdrawn':
      return 'Withdrawn'
    case 'export_approved':
      return 'Export approved'
    case 'export_rejected':
      return 'Export rejected'
    default:
      return notification.type.replace(/_/g, ' ')
  }
}

function isPathActive(pathname: string, target: string) {
  if (target === '/tickets') {
    return pathname === '/tickets' || pathname.startsWith('/tickets/')
  }
  return pathname === target || pathname.startsWith(`${target}/`)
}

function formatRelativeTime(value: string) {
  const target = new Date(value)
  const diffMs = Date.now() - target.getTime()
  if (!Number.isFinite(diffMs)) {
    return 'Just now'
  }

  const diffMinutes = Math.floor(diffMs / 60000)
  if (diffMinutes < 1) {
    return 'Just now'
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
