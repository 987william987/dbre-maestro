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

import { listNotifications, markAllNotificationsRead, markNotificationRead } from '@/modules/notifications/api'
import { useAuth } from '@/shared/auth/AuthContext'

const mockedUseAuth = vi.mocked(useAuth)
const mockedListNotifications = vi.mocked(listNotifications)
const mockedMarkNotificationRead = vi.mocked(markNotificationRead)
const mockedMarkAllNotificationsRead = vi.mocked(markAllNotificationsRead)

function renderShell(initialEntry = '/tickets') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <ToastProvider>
        <Routes>
          <Route element={<AppShell />}>
            <Route path="/tickets" element={<div>tickets page</div>} />
            <Route path="/tickets/new" element={<div>new ticket page</div>} />
            <Route path="/tickets/:id" element={<div>ticket detail page</div>} />
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
        permissions: ['tickets.apply', 'db_metadata.read'],
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
    expect(screen.getByText('Inventory')).toBeInTheDocument()
    expect(screen.getByText('Objects')).toBeInTheDocument()
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
        permissions: ['tickets.apply'],
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
    const ticketsToggle = screen.getByRole('button', { name: /Tickets/i })
    expect(ticketsToggle).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText('New Ticket')).toBeInTheDocument()

    fireEvent.click(ticketsToggle)
    expect(ticketsToggle).toHaveAttribute('aria-expanded', 'false')

    fireEvent.click(ticketsToggle)
    expect(ticketsToggle).toHaveAttribute('aria-expanded', 'true')
  })
})
