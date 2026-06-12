import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { ProtectedRoute } from '@/app/router/ProtectedRoute'
import { RoleRoute } from '@/app/router/RoleRoute'

vi.mock('@/shared/auth/AuthContext', () => ({
  useAuth: vi.fn(),
}))

import { useAuth } from '@/shared/auth/AuthContext'

const mockedUseAuth = vi.mocked(useAuth)

describe('route guards', () => {
  it('未登入時 ProtectedRoute 會導去 /login', () => {
    mockedUseAuth.mockReturnValue({
      status: 'anonymous',
      isAuthenticated: false,
      user: null,
      accessToken: null,
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })

    render(
      <MemoryRouter initialEntries={['/tickets']}>
        <Routes>
          <Route path="/login" element={<div>login page</div>} />
          <Route element={<ProtectedRoute />}>
            <Route path="/tickets" element={<div>tickets page</div>} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )

    expect(screen.getByText('login page')).toBeInTheDocument()
  })

  it('角色不符時 RoleRoute 會導回 /tickets', () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: {
        id: 1,
        username: 'dev',
        authGroups: ['developer'],
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

    render(
      <MemoryRouter initialEntries={['/audit-logs']}>
        <Routes>
          <Route path="/tickets" element={<div>tickets page</div>} />
          <Route element={<RoleRoute allowedPermissions={['audit_logs.read']} />}>
            <Route path="/audit-logs" element={<div>audit logs</div>} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )

    expect(screen.getByText('tickets page')).toBeInTheDocument()
  })

  it('audit_logs.write 也可以通過 Audit Logs 頁面守衛', () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: {
        id: 2,
        username: 'auditor',
        authGroups: ['admin'],
        authGroupDetails: [],
        permissions: ['audit_logs.write'],
        dbConnectionIds: [],
        protected: false,
        isActive: true,
      },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })

    render(
      <MemoryRouter initialEntries={['/audit-logs']}>
        <Routes>
          <Route path="/tickets" element={<div>tickets page</div>} />
          <Route element={<RoleRoute allowedPermissions={['audit_logs.read', 'audit_logs.write']} />}>
            <Route path="/audit-logs" element={<div>audit logs</div>} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )

    expect(screen.getByText('audit logs')).toBeInTheDocument()
  })
})
