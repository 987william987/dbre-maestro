import { render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import App from '@/App'

vi.mock('@/shared/auth/AuthContext', () => ({
  useAuth: vi.fn(() => ({
    status: 'authenticated',
    isAuthenticated: true,
    user: {
      id: 1,
      username: 'admin',
      authGroups: ['admin'],
      authGroupDetails: [],
      permissions: ['settings.read', 'sql_review.read'],
      dbConnectionIds: [],
      protected: false,
      isActive: true,
    },
    accessToken: 'token',
    login: vi.fn(),
    logout: vi.fn(),
    clearAuth: vi.fn(),
  })),
}))

vi.mock('@/app/layout/AppShell', async () => {
  const { Outlet } = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return { AppShell: () => <Outlet /> }
})

vi.mock('@/modules/dashboard/pages/DashboardPage', () => ({
  DashboardPage: () => <div>Dashboard destination</div>,
}))
vi.mock('@/modules/settings/pages/SettingsPage', () => ({
  SettingsPage: ({ section }: { section: string }) => <div>Settings destination: {section}</div>,
}))
vi.mock('@/modules/sql-review-rules/pages/SQLReviewRulesPage', () => ({
  SQLReviewRulesPage: () => <div>SQL review destination</div>,
}))

async function renderAt(path: string, destination: string) {
  window.history.replaceState({}, '', path)
  render(<App />)
  expect(await screen.findByText(destination)).toBeInTheDocument()
}

describe('App redirects', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    window.history.replaceState({}, '', '/')
  })

  it('/ 依使用者權限導向 dashboard', async () => {
    await renderAt('/', 'Dashboard destination')
    await waitFor(() => expect(window.location.pathname).toBe('/dashboard'))
  })

  it('/settings 導向 workflow settings', async () => {
    await renderAt('/settings', 'Settings destination: workflow')
    await waitFor(() => expect(window.location.pathname).toBe('/settings/workflow'))
  })

  it('/sql-review-rules 導向 mysql rules', async () => {
    await renderAt('/sql-review-rules', 'SQL review destination')
    await waitFor(() => expect(window.location.pathname).toBe('/sql-review-rules/mysql'))
  })

  it('未知路徑經由首頁 redirect 導向 dashboard', async () => {
    await renderAt('/not-a-real-route', 'Dashboard destination')
    await waitFor(() => expect(window.location.pathname).toBe('/dashboard'))
  })
})
