import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DBConnectionsPage } from '@/modules/db-connections/pages/DBConnectionsPage'
import type { DBConnection } from '@/shared/types/dbConnection'
import { ToastProvider } from '@/shared/ui/ToastContext'

vi.mock('@/modules/db-connections/api', () => ({
  listDBConnections: vi.fn(),
  createDBConnection: vi.fn(),
  getDBConnectionBindings: vi.fn(),
  testDBConnection: vi.fn(),
  deleteDBConnection: vi.fn(),
  patchDBConnection: vi.fn(),
}))

import { createDBConnection, deleteDBConnection, getDBConnectionBindings, listDBConnections, patchDBConnection, testDBConnection } from '@/modules/db-connections/api'

const mockedListDBConnections = vi.mocked(listDBConnections)
const mockedCreateDBConnection = vi.mocked(createDBConnection)
const mockedGetDBConnectionBindings = vi.mocked(getDBConnectionBindings)
const mockedTestDBConnection = vi.mocked(testDBConnection)
const mockedDeleteDBConnection = vi.mocked(deleteDBConnection)

function selectOption(label: string, option: string) {
  fireEvent.click(screen.getByRole('button', { name: label }))
  fireEvent.click(screen.getByRole('option', { name: option }))
}
const mockedPatchDBConnection = vi.mocked(patchDBConnection)

const connection: DBConnection = {
  id: 5,
  name: 'analytics',
  db_type: 'mysql',
  host: 'db.internal',
  port: 3306,
  readonly_host: 'db.internal',
  readonly_port: 3306,
  readwrite_host: 'db.internal',
  readwrite_port: 3306,
  database_name: 'analytics',
  username: 'readonly',
  encryption_key_version: 1,
  ssl_mode: 'prefer',
  extra_params: null,
  last_test_status: null,
  last_test_error: null,
  last_tested_at: null,
  created_by: 1,
  created_at: '2026-06-09T10:00:00Z',
  updated_at: '2026-06-09T10:00:00Z',
}

function renderPage() {
  return render(
    <ToastProvider>
      <MemoryRouter>
        <DBConnectionsPage />
      </MemoryRouter>
    </ToastProvider>,
  )
}

describe('DBConnectionsPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    mockedListDBConnections.mockResolvedValue({ connections: [connection] })
    mockedGetDBConnectionBindings.mockResolvedValue({
      db_connection_id: 5,
      direct_users: [],
      effective_users: [],
      auth_groups: [],
    })
  })

  it('creates a connection from the new connection drawer and reloads the list', async () => {
    mockedListDBConnections
      .mockResolvedValueOnce({ connections: [] })
      .mockResolvedValueOnce({ connections: [connection] })
    mockedCreateDBConnection.mockResolvedValue(connection)

    renderPage()

    await waitFor(() => expect(mockedListDBConnections).toHaveBeenCalledTimes(1))

    fireEvent.click(screen.getByRole('button', { name: 'New Connection' }))
    expect(screen.queryByLabelText('Database Name')).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'analytics' } })
    fireEvent.change(screen.getByLabelText('Readonly Host'), { target: { value: 'db.internal' } })
    fireEvent.change(screen.getByLabelText('Readonly Port'), { target: { value: '3306' } })
    fireEvent.change(screen.getByLabelText('Readonly Username'), { target: { value: 'readonly' } })
    fireEvent.change(screen.getByLabelText('Readonly Password'), { target: { value: 'secret' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create Connection' }))

    await waitFor(() => expect(mockedCreateDBConnection).toHaveBeenCalledWith({
      name: 'analytics',
      db_type: 'mysql',
      host: 'db.internal',
      port: 3306,
      readonly_host: 'db.internal',
      readonly_port: 3306,
      readwrite_host: '',
      readwrite_port: 3306,
      database_name: null,
      username: 'readonly',
      password: 'secret',
      ssl_mode: 'prefer',
      credentials: [{ credential_role: 'readonly', username: 'readonly', password: 'secret' }],
    }))
    await waitFor(() => expect(mockedListDBConnections).toHaveBeenCalledTimes(2))
  })

  it('uses the postgres database automatically and keeps the database field hidden for postgres', async () => {
    mockedCreateDBConnection.mockResolvedValue({ ...connection, db_type: 'postgres', port: 5432 })

    renderPage()

    await waitFor(() => expect(screen.getAllByText('analytics').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: 'New Connection' }))
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'warehouse' } })
    selectOption('DB Type', 'PostgreSQL')
    fireEvent.change(screen.getByLabelText('Readonly Host'), { target: { value: 'pg.internal' } })
    fireEvent.change(screen.getByLabelText('Readonly Username'), { target: { value: 'postgres' } })
    fireEvent.change(screen.getByLabelText('Readonly Password'), { target: { value: 'secret' } })
    expect(screen.queryByLabelText('Database Name')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Create Connection' }))

    await waitFor(() => expect(mockedCreateDBConnection).toHaveBeenCalledWith({
      name: 'warehouse',
      db_type: 'postgres',
      host: 'pg.internal',
      port: 5432,
      readonly_host: 'pg.internal',
      readonly_port: 5432,
      readwrite_host: '',
      readwrite_port: 5432,
      database_name: 'postgres',
      username: 'postgres',
      password: 'secret',
      ssl_mode: 'prefer',
      credentials: [{ credential_role: 'readonly', username: 'postgres', password: 'secret' }],
    }))
  })

  it('does not retain the postgres database name when switching back to mysql', async () => {
    mockedCreateDBConnection.mockResolvedValue(connection)

    renderPage()

    await waitFor(() => expect(screen.getAllByText('analytics').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: 'New Connection' }))
    selectOption('DB Type', 'PostgreSQL')
    expect(screen.queryByLabelText('Database Name')).not.toBeInTheDocument()

    selectOption('DB Type', 'MySQL / MariaDB')
    expect(screen.queryByLabelText('Database Name')).not.toBeInTheDocument()
  })

  it('allows redis connections without a username', async () => {
    mockedCreateDBConnection.mockResolvedValue({ ...connection, id: 6, db_type: 'redis', port: 6379, username: '' })

    renderPage()

    await waitFor(() => expect(screen.getAllByText('analytics').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: 'New Connection' }))
    expect(screen.queryByLabelText('Database Name')).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'cache-redis' } })
    selectOption('DB Type', 'Redis')
    fireEvent.change(screen.getByLabelText('Readonly Host'), { target: { value: 'redis.internal' } })
    fireEvent.change(screen.getByLabelText('Readonly Password'), { target: { value: 'secret' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create Connection' }))

    await waitFor(() => expect(mockedCreateDBConnection).toHaveBeenCalledWith({
      name: 'cache-redis',
      db_type: 'redis',
      host: 'redis.internal',
      port: 6379,
      readonly_host: 'redis.internal',
      readonly_port: 6379,
      readwrite_host: '',
      readwrite_port: 6379,
      database_name: null,
      username: '',
      password: 'secret',
      ssl_mode: 'prefer',
      credentials: [{ credential_role: 'readonly', username: '', password: 'secret' }],
    }))
  })

  it('tests a connection', async () => {
    mockedTestDBConnection.mockResolvedValue({
      ok: true,
      last_test_status: 'passed',
      last_tested_at: '2026-06-16T12:00:00Z',
      results: [
        { credential_role: 'readonly', ok: true },
        { credential_role: 'readwrite', ok: true },
      ],
    })

    renderPage()

    await waitFor(() => expect(screen.getAllByText('analytics').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: 'Test' }))

    await waitFor(() => expect(mockedTestDBConnection).toHaveBeenCalledWith(5))
    await waitFor(() => expect(screen.getByText('analytics connection test succeeded for readonly and readwrite')).toBeInTheDocument())
    expect(screen.queryByText((content) => content === 'Connection test succeeded')).not.toBeInTheDocument()
  })

  it('moves a failed connection test row to the top and shows the error', async () => {
    const newerConnection: DBConnection = {
      ...connection,
      id: 6,
      name: 'warehouse',
      created_at: '2026-06-10T12:00:00Z',
      updated_at: '2026-06-10T12:00:00Z',
    }
    mockedListDBConnections.mockResolvedValue({ connections: [connection, newerConnection] })
    mockedTestDBConnection.mockResolvedValueOnce({
      ok: false,
      error: 'readonly: timeout; readwrite: access denied',
      last_test_status: 'failed',
      last_test_error: 'readonly: timeout; readwrite: access denied',
      last_tested_at: '2026-06-16T12:00:00Z',
      results: [
        { credential_role: 'readonly', ok: false, error: 'timeout' },
        { credential_role: 'readwrite', ok: false, error: 'access denied' },
      ],
    })

    renderPage()

    await waitFor(() => expect(screen.getAllByText(/analytics|warehouse/).length).toBeGreaterThan(1))
    fireEvent.click(screen.getAllByRole('button', { name: 'Test' })[0])

    await waitFor(() => expect(screen.getByText('warehouse connection test failed: readonly: timeout; readwrite: access denied')).toBeInTheDocument())

    const rows = screen.getAllByRole('row')
    expect(rows[1]).toHaveTextContent('warehouse')
    expect(rows[1]).not.toHaveTextContent('Connection test failed')
  })

  it('opens the edit drawer and updates the connection', async () => {
    mockedPatchDBConnection.mockResolvedValue({ ...connection, db_type: 'postgres', ssl_mode: 'require' })

    renderPage()

    await waitFor(() => expect(screen.getAllByText('analytics').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
    selectOption('DB Type', 'PostgreSQL')
    selectOption('SSL Mode', 'require')
    expect(screen.queryByLabelText('Database Name')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))

    await waitFor(() => expect(mockedPatchDBConnection).toHaveBeenCalledWith(5, {
      name: 'analytics',
      db_type: 'postgres',
      host: 'db.internal',
      port: 5432,
      readonly_host: 'db.internal',
      readonly_port: 5432,
      readwrite_host: 'db.internal',
      readwrite_port: 5432,
      database_name: 'postgres',
      username: 'readonly',
      password: '',
      ssl_mode: 'require',
      credentials: [{ credential_role: 'readonly', username: 'readonly', password: '' }],
    }))
  })

  it('keeps the database field hidden for mysql and always submits null', async () => {
    mockedCreateDBConnection.mockResolvedValue(connection)

    renderPage()

    await waitFor(() => expect(screen.getAllByText('analytics').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: 'New Connection' }))
    expect(screen.queryByLabelText('Database Name')).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'mysql-conn' } })
    fireEvent.change(screen.getByLabelText('Readonly Host'), { target: { value: 'db.internal' } })
    fireEvent.change(screen.getByLabelText('Readonly Port'), { target: { value: '3306' } })
    fireEvent.change(screen.getByLabelText('Readonly Username'), { target: { value: 'root' } })
    fireEvent.change(screen.getByLabelText('Readonly Password'), { target: { value: 'secret' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create Connection' }))

    await waitFor(() => expect(mockedCreateDBConnection).toHaveBeenCalledWith({
      name: 'mysql-conn',
      db_type: 'mysql',
      host: 'db.internal',
      port: 3306,
      readonly_host: 'db.internal',
      readonly_port: 3306,
      readwrite_host: '',
      readwrite_port: 3306,
      database_name: null,
      username: 'root',
      password: 'secret',
      ssl_mode: 'prefer',
      credentials: [{ credential_role: 'readonly', username: 'root', password: 'secret' }],
    }))
  })

  it('calls the delete API after confirmation', async () => {
    mockedListDBConnections
      .mockResolvedValueOnce({ connections: [connection] })
      .mockResolvedValueOnce({ connections: [] })
    mockedDeleteDBConnection.mockResolvedValue(undefined)

    renderPage()

    await waitFor(() => expect(screen.getAllByText('analytics').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm Delete' }))

    await waitFor(() => expect(mockedDeleteDBConnection).toHaveBeenCalledWith(5))
  })

  it('supports pagination for the connection list', async () => {
    mockedListDBConnections.mockResolvedValue({
      connections: Array.from({ length: 21 }, (_, index) => ({
        ...connection,
        id: index + 1,
        name: `conn-${index + 1}`,
        created_at: `2026-06-${String(index + 1).padStart(2, '0')}T10:00:00Z`,
        updated_at: `2026-06-${String(index + 1).padStart(2, '0')}T10:00:00Z`,
      })),
    })

    renderPage()

    await waitFor(() => expect(screen.getByText('conn-21')).toBeInTheDocument())
    expect(screen.queryByText('conn-1')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Next' }))

    await waitFor(() => expect(screen.getByText('conn-1')).toBeInTheDocument())
    expect(screen.queryByText('conn-21')).not.toBeInTheDocument()
  })
})
