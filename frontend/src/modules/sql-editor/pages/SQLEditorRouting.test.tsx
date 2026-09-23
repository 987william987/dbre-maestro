import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AppShell } from '@/app/layout/AppShell'
import { SQLEditorPage } from '@/modules/sql-editor/pages/SQLEditorPage'
import { ToastProvider } from '@/shared/ui/ToastContext'

vi.mock('@uiw/react-codemirror', () => ({
  default: ({ value }: { value: string }) => <textarea aria-label="CodeMirror" value={value} readOnly />,
}))

vi.mock('@/shared/auth/AuthContext', () => ({
  useAuth: vi.fn(() => ({
    user: {
      id: 7,
      username: 'admin',
      authGroups: ['admin'],
      authGroupDetails: [],
      permissions: ['sql_editor.read', 'sql_editor.query', 'tickets.read'],
      dbConnectionIds: [1],
      protected: false,
      isActive: true,
    },
    status: 'authenticated',
    isAuthenticated: true,
    accessToken: 'token',
    login: vi.fn(),
    logout: vi.fn(),
    clearAuth: vi.fn(),
  })),
}))

vi.mock('@/modules/notifications/api', () => ({
  listNotifications: vi.fn(async () => ({ notifications: [], total: 0, unread: 0, limit: 10, offset: 0 })),
  listNotificationSummary: vi.fn(async () => ({ pending: 0, review_required: 0, execution_required: 0 })),
  markNotificationRead: vi.fn(),
  markAllNotificationsRead: vi.fn(),
}))

vi.mock('@/shared/api/client', async () => {
  const actual = await vi.importActual<typeof import('@/shared/api/client')>('@/shared/api/client')
  return { ...actual, openEventStream: vi.fn(() => () => undefined) }
})

vi.mock('@/modules/sql-editor/api', async () => {
  const actual = await vi.importActual<typeof import('@/modules/sql-editor/api')>('@/modules/sql-editor/api')
  return {
    ...actual,
    listQueryConnections: vi.fn(async () => ({ connections: [] })),
    getQueryConstraints: vi.fn(async () => ({
      default_limit: 200,
      max_limit: 1000,
      app_timeout_seconds: 30,
      mysql_max_execution_time_ms: 25000,
      postgres_statement_timeout_ms: 25000,
    })),
    listQueryHistory: vi.fn(async () => ({ history: [], total: 0, limit: 20, offset: 0, retention_days: 90 })),
    listSavedQueries: vi.fn(async () => ({ saved_queries: [] })),
  }
})

describe('SQL Editor routing', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('進入 SQL Editor 後仍可透過 AppShell 導覽切換到其他頁面', async () => {
    render(
      <MemoryRouter initialEntries={['/sql-editor']}>
        <ToastProvider>
          <Routes>
            <Route element={<AppShell />}>
              <Route path="/sql-editor" element={<SQLEditorPage />} />
              <Route path="/tickets" element={<div>Tickets destination</div>} />
            </Route>
          </Routes>
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()

    fireEvent.click(screen.getAllByRole('link', { name: 'Tickets' })[0])

    expect(await screen.findByText('Tickets destination')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Run' })).not.toBeInTheDocument()
  })
})
