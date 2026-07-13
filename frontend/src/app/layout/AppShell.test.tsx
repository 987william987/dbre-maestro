import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AppShell } from '@/app/layout/AppShell'
import { ToastProvider } from '@/shared/ui/ToastContext'

vi.mock('@/shared/auth/AuthContext', () => ({
  useAuth: vi.fn(),
}))

vi.mock('@/modules/notifications/api', () => ({
  listNotifications: vi.fn(),
  markNotificationRead: vi.fn(),
  markAllNotificationsRead: vi.fn(),
}))

vi.mock('@/shared/api/client', async () => {
  const actual = await vi.importActual<typeof import('@/shared/api/client')>('@/shared/api/client')
  return {
    ...actual,
    openEventStream: vi.fn(() => () => undefined),
  }
})

import { listNotifications, markAllNotificationsRead, markNotificationRead } from '@/modules/notifications/api'
import { useAuth } from '@/shared/auth/AuthContext'

const mockedUseAuth = vi.mocked(useAuth)
const mockedListNotifications = vi.mocked(listNotifications)
const mockedMarkNotificationRead = vi.mocked(markNotificationRead)
const mockedMarkAllNotificationsRead = vi.mocked(markAllNotificationsRead)
const storage = new Map<string, string>()

function renderShell(initialEntry = '/tickets') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <ToastProvider>
        <Routes>
          <Route element={<AppShell />}>
            <Route path="/tickets" element={<div>tickets page</div>} />
            <Route path="/tickets/new" element={<div>new ticket page</div>} />
            <Route path="/tickets/:id" element={<div>ticket detail page</div>} />
            <Route path="/sql-editor" element={<div>sql editor page</div>} />
            <Route path="/users" element={<div>users page</div>} />
            <Route path="/users/groups" element={<div>auth groups page</div>} />
            <Route path="/users/resources" element={<div>resources page</div>} />
            <Route path="/users/query-access" element={<div>query access page</div>} />
            <Route path="/db-metadata/inventory" element={<div>inventory page</div>} />
            <Route path="/db-metadata/objects" element={<div>objects page</div>} />
          </Route>
        </Routes>
      </ToastProvider>
    </MemoryRouter>,
  )
}

describe('AppShell notifications', () => {
  beforeEach(() => {
    storage.clear()
    Object.defineProperty(window, 'localStorage', {
      value: {
        getItem: (key: string) => storage.get(key) ?? null,
        setItem: (key: string, value: string) => {
          storage.set(key, value)
        },
        removeItem: (key: string) => {
          storage.delete(key)
        },
        clear: () => {
          storage.clear()
        },
      },
      configurable: true,
    })
    window.localStorage.removeItem('dbre-maestro.sidebarCollapsed')
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: {
        id: 1,
        username: 'reviewer',
        authGroups: ['reviewer'],
        authGroupDetails: [],
        permissions: ['tickets.review'],
        dbConnectionIds: [],
        protected: false,
        isActive: true,
      },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedListNotifications.mockResolvedValue({
      notifications: [
        {
          id: 101,
          user_id: 1,
          type: 'ticket_pending_review',
          title: '工單待審批',
          body: '工單 T-101 等待審核',
          resource_type: 'ticket',
          resource_id: 101,
          resource_ref: 'T-101',
          is_read: false,
          created_at: new Date().toISOString(),
        },
      ],
      total: 1,
      unread: 1,
      limit: 8,
      offset: 0,
    })
    mockedMarkNotificationRead.mockResolvedValue(undefined)
    mockedMarkAllNotificationsRead.mockResolvedValue(undefined)
  })

  it('會顯示鈴鐺未讀數並可展開通知下拉', async () => {
    renderShell()

    await waitFor(() => expect(mockedListNotifications).toHaveBeenCalled())
    expect(screen.getByLabelText('Notifications')).toBeInTheDocument()
    expect(screen.getByText('1')).toBeInTheDocument()

    fireEvent.click(screen.getByLabelText('Notifications'))

    expect(screen.getByText('Notifications')).toBeInTheDocument()
    expect(screen.getByText('工單待審批')).toBeInTheDocument()
    expect(screen.getByText('工單 T-101 等待審核')).toBeInTheDocument()
  })

  it('可將全部通知標示已讀', async () => {
    renderShell()

    await waitFor(() => expect(mockedListNotifications).toHaveBeenCalled())
    fireEvent.click(screen.getByLabelText('Notifications'))
    fireEvent.click(screen.getByText('Mark all read'))

    await waitFor(() => expect(mockedMarkAllNotificationsRead).toHaveBeenCalled())
  })

  it('有子項目的主導航會自動展開目前所在頁面', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: {
        id: 1,
        username: 'dba',
        authGroups: ['dba'],
        authGroupDetails: [],
        permissions: ['tickets.read', 'tickets.apply', 'db_metadata.read'],
        dbConnectionIds: [],
        protected: false,
        isActive: true,
      },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })

    renderShell('/db-metadata/objects')

    await waitFor(() => expect(mockedListNotifications).toHaveBeenCalled())
    expect(screen.getByRole('button', { name: /DB Metadata/i })).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getAllByText('Governance').length).toBeGreaterThan(0)
    expect(screen.getAllByText('DB Metadata').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Objects').length).toBeGreaterThan(0)
    expect(screen.getByText('Inventory')).toBeInTheDocument()
  })

  it('可手動展開與收合子導航', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: {
        id: 1,
        username: 'operator',
        authGroups: ['operator'],
        authGroupDetails: [],
        permissions: ['tickets.read', 'tickets.apply'],
        dbConnectionIds: [],
        protected: false,
        isActive: true,
      },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })

    renderShell('/tickets')

    await waitFor(() => expect(mockedListNotifications).toHaveBeenCalled())
    expect(screen.getAllByText('Workbench').length).toBeGreaterThan(0)
    const ticketsToggle = screen.getByRole('button', { name: 'Tickets' })
    expect(ticketsToggle).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText('New Ticket')).toBeInTheDocument()

    fireEvent.click(ticketsToggle)
    expect(ticketsToggle).toHaveAttribute('aria-expanded', 'false')

    fireEvent.click(ticketsToggle)
    expect(ticketsToggle).toHaveAttribute('aria-expanded', 'true')
  })

  it('tickets breadcrumb 會顯示頁面說明 popover', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: {
        id: 1,
        username: 'operator',
        authGroups: ['operator'],
        authGroupDetails: [],
        permissions: ['tickets.read', 'tickets.apply'],
        dbConnectionIds: [],
        protected: false,
        isActive: true,
      },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })

    renderShell('/tickets')

    await waitFor(() => expect(mockedListNotifications).toHaveBeenCalled())
    fireEvent.click(screen.getByRole('button', { name: 'Show All Tickets Guide' }))

    expect(screen.getByText('All Tickets Guide')).toBeInTheDocument()
    expect(screen.getByText(/Description is hidden by default/)).toBeInTheDocument()
  })

  it('sql editor breadcrumb 會顯示頁面說明 popover', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: {
        id: 1,
        username: 'developer',
        authGroups: ['developer'],
        authGroupDetails: [],
        permissions: ['sql_editor.read'],
        dbConnectionIds: [],
        protected: false,
        isActive: true,
      },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })

    renderShell('/sql-editor')

    await waitFor(() => expect(mockedListNotifications).toHaveBeenCalled())
    fireEvent.click(screen.getByRole('button', { name: 'Show SQL Editor Guide' }))

    expect(screen.getByText('SQL Editor Guide')).toBeInTheDocument()
    expect(screen.getByText(/Select a DB connection/)).toBeInTheDocument()
  })

  it('桌面側欄可以收合成 icon rail 並再展開', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: {
        id: 1,
        username: 'operator',
        authGroups: ['operator'],
        authGroupDetails: [],
        permissions: ['tickets.read', 'tickets.apply', 'sql_editor.read'],
        dbConnectionIds: [],
        protected: false,
        isActive: true,
      },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })

    renderShell('/tickets')

    await waitFor(() => expect(mockedListNotifications).toHaveBeenCalled())
    const sidebarSubtitle = screen.getByText('Operations Control Plane')
    expect(sidebarSubtitle).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Collapse sidebar' }))

    expect(screen.getByRole('button', { name: 'Expand sidebar' })).toBeInTheDocument()
    expect(sidebarSubtitle.parentElement).toHaveClass('opacity-0')
    expect(sidebarSubtitle.parentElement).toHaveClass('max-w-0')
    expect(window.localStorage.getItem('dbre-maestro.sidebarCollapsed')).toBe('true')

    fireEvent.click(screen.getByRole('button', { name: 'Expand sidebar' }))

    expect(screen.getByRole('button', { name: 'Collapse sidebar' })).toBeInTheDocument()
    expect(sidebarSubtitle.parentElement).toHaveClass('opacity-100')
  })

  it('users groups 路由會對應到 Auth Groups breadcrumb', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: {
        id: 1,
        username: 'admin',
        authGroups: ['admin'],
        authGroupDetails: [],
        permissions: ['users.read'],
        dbConnectionIds: [],
        protected: false,
        isActive: true,
      },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })

    renderShell('/users/groups')

    await waitFor(() => expect(mockedListNotifications).toHaveBeenCalled())
    expect(screen.getByRole('button', { name: /Users/i })).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getAllByText('Governance').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Auth Groups').length).toBeGreaterThan(0)
  })

  it('users resources 路由會展開 Users 導航並顯示 Resources', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: {
        id: 1,
        username: 'admin',
        authGroups: ['admin'],
        authGroupDetails: [],
        permissions: ['users.read'],
        dbConnectionIds: [],
        protected: false,
        isActive: true,
      },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })

    renderShell('/users/resources')

    await waitFor(() => expect(mockedListNotifications).toHaveBeenCalled())
    expect(screen.getByRole('button', { name: /Users/i })).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getAllByText('Resources').length).toBeGreaterThan(0)
  })

  it('users query access 路由會展開 Users 導航並顯示 Query Access', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: {
        id: 1,
        username: 'admin',
        authGroups: ['admin'],
        authGroupDetails: [],
        permissions: ['users.read'],
        dbConnectionIds: [],
        protected: false,
        isActive: true,
      },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })

    renderShell('/users/query-access')

    await waitFor(() => expect(mockedListNotifications).toHaveBeenCalled())
    expect(screen.getByRole('button', { name: /Users/i })).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getAllByText('Query Access').length).toBeGreaterThan(0)
  })
})
