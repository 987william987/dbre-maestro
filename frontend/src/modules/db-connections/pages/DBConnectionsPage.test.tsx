import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { DBConnectionsPage } from '@/modules/db-connections/pages/DBConnectionsPage'
import type { DBConnection } from '@/shared/types/dbConnection'
import { ToastProvider } from '@/shared/ui/ToastContext'

vi.mock('@/modules/db-connections/api', () => ({
  listDBConnections: vi.fn(),
  createDBConnection: vi.fn(),
  testDBConnection: vi.fn(),
  deleteDBConnection: vi.fn(),
}))

import { createDBConnection, deleteDBConnection, listDBConnections, testDBConnection } from '@/modules/db-connections/api'

const mockedListDBConnections = vi.mocked(listDBConnections)
const mockedCreateDBConnection = vi.mocked(createDBConnection)
const mockedTestDBConnection = vi.mocked(testDBConnection)
const mockedDeleteDBConnection = vi.mocked(deleteDBConnection)

const connection: DBConnection = {
  id: 5,
  name: 'analytics',
  db_type: 'mysql',
  host: 'db.internal',
  port: 3306,
  database_name: 'analytics',
  username: 'readonly',
  encryption_key_version: 1,
  ssl_mode: 'prefer',
  extra_params: null,
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
  it('建立連線後會重新載入列表', async () => {
    mockedListDBConnections
      .mockResolvedValueOnce({ connections: [] })
      .mockResolvedValueOnce({ connections: [connection] })
    mockedCreateDBConnection.mockResolvedValue(connection)

    renderPage()

    await waitFor(() => expect(mockedListDBConnections).toHaveBeenCalledTimes(1))

    fireEvent.change(screen.getByPlaceholderText('名稱'), { target: { value: 'analytics' } })
    fireEvent.change(screen.getByPlaceholderText('Host'), { target: { value: 'db.internal' } })
    fireEvent.change(screen.getByPlaceholderText('Port'), { target: { value: '3306' } })
    fireEvent.change(screen.getByPlaceholderText('Username'), { target: { value: 'readonly' } })
    fireEvent.change(screen.getByPlaceholderText('Password'), { target: { value: 'secret' } })
    fireEvent.click(screen.getByRole('button', { name: '新增連線' }))

    await waitFor(() => expect(mockedCreateDBConnection).toHaveBeenCalled())
    await waitFor(() => expect(mockedListDBConnections).toHaveBeenCalledTimes(2))
  })

  it('可對連線執行 test', async () => {
    mockedListDBConnections.mockResolvedValue({ connections: [connection] })
    mockedTestDBConnection.mockResolvedValue({ ok: true })

    renderPage()

    await waitFor(() => expect(screen.getAllByText('analytics').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: 'Test' }))

    await waitFor(() => expect(mockedTestDBConnection).toHaveBeenCalledWith(5))
    await waitFor(() => expect(screen.getAllByText('連線測試成功').length).toBeGreaterThan(0))
  })

  it('確認刪除後會呼叫 delete API', async () => {
    mockedListDBConnections
      .mockResolvedValueOnce({ connections: [connection] })
      .mockResolvedValueOnce({ connections: [] })
    mockedDeleteDBConnection.mockResolvedValue(undefined)

    renderPage()

    await waitFor(() => expect(screen.getAllByText('analytics').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    fireEvent.click(screen.getByRole('button', { name: '確認刪除' }))

    await waitFor(() => expect(mockedDeleteDBConnection).toHaveBeenCalledWith(5))
  })
})
