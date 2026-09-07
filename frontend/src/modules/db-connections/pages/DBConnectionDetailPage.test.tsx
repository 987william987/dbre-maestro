import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DBConnectionDetailPage } from '@/modules/db-connections/pages/DBConnectionDetailPage'

vi.mock('@/modules/db-connections/api', () => ({
  getDBConnectionOverview: vi.fn(),
  listDBConnectionDatabases: vi.fn(),
  listDBConnectionAccounts: vi.fn(),
}))
vi.mock('@/shared/auth/AuthContext', () => ({ useAuth: vi.fn() }))

import { getDBConnectionOverview, listDBConnectionAccounts, listDBConnectionDatabases } from '@/modules/db-connections/api'
import { useAuth } from '@/shared/auth/AuthContext'

const connection = { id: 7, name: 'orders-primary', db_type: 'mysql', host: 'db.internal', port: 3306, username: 'reader', encryption_key_version: 1, ssl_mode: 'require', created_by: 1, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' }

function renderPage(view: 'overview' | 'databases' | 'accounts') {
  return render(<MemoryRouter initialEntries={[`/db-connections/7/${view}`]}><Routes><Route path="/db-connections/:id/:view" element={<DBConnectionDetailPage view={view} />} /><Route path="/db-connections" element={<div>connections</div>} /></Routes></MemoryRouter>)
}

describe('DBConnectionDetailPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(useAuth).mockReturnValue({ user: { permissions: ['db_connections.overview', 'db_connections.databases', 'db_connections.accounts'] } } as ReturnType<typeof useAuth>)
    vi.mocked(getDBConnectionOverview).mockResolvedValue(connection)
  })

  it('shows database size and collation from the connection snapshot', async () => {
    vi.mocked(listDBConnectionDatabases).mockResolvedValue({ connection, total: 1, items: [{ snapshot_at: '2026-09-07T00:00:00Z', database_name: 'orders', character_set_name: 'utf8mb4', collation_name: 'utf8mb4_0900_ai_ci', table_count: 12, data_size_bytes: 1048576, index_size_bytes: 524288 }] })
    renderPage('databases')
    expect(await screen.findByText('orders')).toBeInTheDocument()
    expect(screen.getByText('1.00 MB')).toBeInTheDocument()
    expect(screen.getByText('512.00 KB')).toBeInTheDocument()
    expect(screen.getByText('utf8mb4_0900_ai_ci')).toBeInTheDocument()
  })

  it('prefers native grant SQL over fragmented privilege rows', async () => {
    vi.mocked(listDBConnectionAccounts).mockResolvedValue({ connection, scan_status: null, accounts: [{ principal_key: "'app'@'%'", principal_name: 'app', principal_host: '%', principal_type: 'user', can_login: true, is_superuser: false, inherits_roles: true, can_create_role: false, can_create_database: false, can_replicate: false, can_bypass_rls: false, is_locked: false }], grants: [{ principal_key: "'app'@'%'", grant_kind: 'native_statement', grant_statement: "GRANT SELECT ON `orders`.* TO `app`@`%`", is_grantable: false }, { principal_key: "'app'@'%'", grant_kind: 'privilege', scope_type: 'table', database_name: 'orders', object_name: 'invoices', privilege_type: 'SELECT', is_grantable: false }] })
    renderPage('accounts')
    expect(await screen.findByText('app')).toBeInTheDocument()
    expect(screen.getByText("GRANT SELECT ON `orders`.* TO `app`@`%`")).toBeInTheDocument()
    expect(screen.queryByText('GRANT SELECT ON orders.invoices')).not.toBeInTheDocument()
  })

  it('shows PostgreSQL role attributes and hides built-in pg roles', async () => {
    vi.mocked(listDBConnectionAccounts).mockResolvedValue({ connection: { ...connection, db_type: 'postgres' }, scan_status: null, accounts: [
      { principal_key: 'app', principal_name: 'app', principal_type: 'user', can_login: true, is_superuser: false, inherits_roles: false, can_create_role: false, can_create_database: true, can_replicate: false, can_bypass_rls: false, is_locked: false },
      { principal_key: 'pg_monitor', principal_name: 'pg_monitor', principal_type: 'role', can_login: false, is_superuser: false, inherits_roles: true, can_create_role: false, can_create_database: false, can_replicate: false, can_bypass_rls: false, is_locked: false },
    ], grants: [] })
    renderPage('accounts')
    expect(await screen.findByText('No inheritance, Create DB')).toBeInTheDocument()
    expect(screen.getByText('Can login')).toBeInTheDocument()
    expect(screen.getByText('Infinity')).toBeInTheDocument()
    expect(screen.queryByText('pg_monitor')).not.toBeInTheDocument()
  })

  it('hides account navigation without the account permission', async () => {
    vi.mocked(useAuth).mockReturnValue({ user: { permissions: ['db_connections.overview'] } } as ReturnType<typeof useAuth>)
    renderPage('overview')
    expect(await screen.findByText('orders-primary')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Accounts' })).not.toBeInTheDocument()
  })
})
