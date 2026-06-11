import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DBConnectionsPage } from '@/modules/db-connections/pages/DBConnectionsPage'
import type { DBConnection } from '@/shared/types/dbConnection'
import { ToastProvider } from '@/shared/ui/ToastContext'

vi.mock('@/modules/db-connections/api', () => ({
  listDBConnections: vi.fn(),
  createDBConnection: vi.fn(),
  testDBConnection: vi.fn(),
  deleteDBConnection: vi.fn(),
  patchDBConnection: vi.fn(),
}))

import { createDBConnection, deleteDBConnection, listDBConnections, patchDBConnection, testDBConnection } from '@/modules/db-connections/api'

const mockedListDBConnections = vi.mocked(listDBConnections)
const mockedCreateDBConnection = vi.mocked(createDBConnection)
const mockedTestDBConnection = vi.mocked(testDBConnection)
const mockedDeleteDBConnection = vi.mocked(deleteDBConnection)
const mockedPatchDBConnection = vi.mocked(patchDBConnection)

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
  beforeEach(() => {
    vi.restoreAllMocks()
    mockedListDBConnections.mockResolvedValue({ connections: [connection] })
  })

  it('從新增按鈕開 drawer 後可建立連線並重新載入列表', async () => {
    mockedListDBConnections
      .mockResolvedValueOnce({ connections: [] })
      .mockResolvedValueOnce({ connections: [connection] })
    mockedCreateDBConnection.mockResolvedValue(connection)

    renderPage()

    await waitFor(() => expect(mockedListDBConnections).toHaveBeenCalledTimes(1))

    fireEvent.click(screen.getByRole('button', { name: '新增連線' }))
    expect(screen.queryByLabelText('Database Name')).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'analytics' } })
    fireEvent.change(screen.getByLabelText('Host'), { target: { value: 'db.internal' } })
    fireEvent.change(screen.getByLabelText('Port'), { target: { value: '3306' } })
    fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'readonly' } })
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'secret' } })
    fireEvent.click(screen.getByRole('button', { name: '建立連線' }))

    await waitFor(() => expect(mockedCreateDBConnection).toHaveBeenCalledWith({
      name: 'analytics',
      db_type: 'mysql',
      host: 'db.internal',
      port: 3306,
      database_name: null,
      username: 'readonly',
      password: 'secret',
      ssl_mode: 'prefer',
    }))
    await waitFor(() => expect(mockedListDBConnections).toHaveBeenCalledTimes(2))
  })

  it('建立 postgres 時會自動使用 postgres database 並隱藏 database 欄位', async () => {
    mockedCreateDBConnection.mockResolvedValue({ ...connection, db_type: 'postgres', port: 5432 })

    renderPage()

    await waitFor(() => expect(screen.getAllByText('analytics').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: '新增連線' }))
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'warehouse' } })
    fireEvent.change(screen.getByLabelText('DB Type'), { target: { value: 'postgres' } })
    fireEvent.change(screen.getByLabelText('Host'), { target: { value: 'pg.internal' } })
    fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'postgres' } })
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'secret' } })
    expect(screen.queryByLabelText('Database Name')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '建立連線' }))

    await waitFor(() => expect(mockedCreateDBConnection).toHaveBeenCalledWith({
      name: 'warehouse',
      db_type: 'postgres',
      host: 'pg.internal',
      port: 5432,
      database_name: 'postgres',
      username: 'postgres',
      password: 'secret',
      ssl_mode: 'prefer',
    }))
  })

  it('從 postgres 切回 mysql 時不會殘留 postgres database name', async () => {
    mockedCreateDBConnection.mockResolvedValue(connection)

    renderPage()

    await waitFor(() => expect(screen.getAllByText('analytics').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: '新增連線' }))
    fireEvent.change(screen.getByLabelText('DB Type'), { target: { value: 'postgres' } })
    expect(screen.queryByLabelText('Database Name')).not.toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('DB Type'), { target: { value: 'mysql' } })
    expect(screen.queryByLabelText('Database Name')).not.toBeInTheDocument()
  })

  it('建立 redis 時可不填 username', async () => {
    mockedCreateDBConnection.mockResolvedValue({ ...connection, id: 6, db_type: 'redis', port: 6379, username: '' })

    renderPage()

    await waitFor(() => expect(screen.getAllByText('analytics').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: '新增連線' }))
    expect(screen.queryByLabelText('Database Name')).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'cache-redis' } })
    fireEvent.change(screen.getByLabelText('DB Type'), { target: { value: 'redis' } })
    fireEvent.change(screen.getByLabelText('Host'), { target: { value: 'redis.internal' } })
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'secret' } })
    fireEvent.click(screen.getByRole('button', { name: '建立連線' }))

    await waitFor(() => expect(mockedCreateDBConnection).toHaveBeenCalledWith({
      name: 'cache-redis',
      db_type: 'redis',
      host: 'redis.internal',
      port: 6379,
      database_name: null,
      username: '',
      password: 'secret',
      ssl_mode: 'prefer',
    }))
  })

  it('可對連線執行 test', async () => {
    mockedTestDBConnection.mockResolvedValue({ ok: true })

    renderPage()

    await waitFor(() => expect(screen.getAllByText('analytics').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: 'Test' }))

    await waitFor(() => expect(mockedTestDBConnection).toHaveBeenCalledWith(5))
    await waitFor(() => expect(screen.getByText('analytics 連線測試成功')).toBeInTheDocument())
    expect(screen.queryByText((content) => content === '連線測試成功')).not.toBeInTheDocument()
  })

  it('連線測試失敗時會將該列浮到最上方並顯示錯誤', async () => {
    const newerConnection: DBConnection = {
      ...connection,
      id: 6,
      name: 'warehouse',
      created_at: '2026-06-10T12:00:00Z',
      updated_at: '2026-06-10T12:00:00Z',
    }
    mockedListDBConnections.mockResolvedValue({ connections: [connection, newerConnection] })
    mockedTestDBConnection.mockRejectedValueOnce(new Error('timeout'))

    renderPage()

    await waitFor(() => expect(screen.getAllByText(/analytics|warehouse/).length).toBeGreaterThan(1))
    fireEvent.click(screen.getAllByRole('button', { name: 'Test' })[0])

    await waitFor(() => expect(screen.getByText('warehouse 連線測試失敗：連線測試失敗')).toBeInTheDocument())

    const rows = screen.getAllByRole('row')
    expect(rows[1]).toHaveTextContent('warehouse')
    expect(rows[1]).not.toHaveTextContent('連線測試失敗')
  })

  it('可開啟編輯 drawer 並更新連線', async () => {
    mockedPatchDBConnection.mockResolvedValue({ ...connection, db_type: 'postgres', ssl_mode: 'require' })

    renderPage()

    await waitFor(() => expect(screen.getAllByText('analytics').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
    fireEvent.change(screen.getByLabelText('DB Type'), { target: { value: 'postgres' } })
    fireEvent.change(screen.getByLabelText('SSL Mode'), { target: { value: 'require' } })
    expect(screen.queryByLabelText('Database Name')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '儲存變更' }))

    await waitFor(() => expect(mockedPatchDBConnection).toHaveBeenCalledWith(5, {
      name: 'analytics',
      db_type: 'postgres',
      host: 'db.internal',
      port: 5432,
      database_name: 'postgres',
      username: 'readonly',
      password: '',
      ssl_mode: 'require',
    }))
  })

  it('mysql 建立時不顯示 database 欄位且固定送 null', async () => {
    mockedCreateDBConnection.mockResolvedValue(connection)

    renderPage()

    await waitFor(() => expect(screen.getAllByText('analytics').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: '新增連線' }))
    expect(screen.queryByLabelText('Database Name')).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'mysql-conn' } })
    fireEvent.change(screen.getByLabelText('Host'), { target: { value: 'db.internal' } })
    fireEvent.change(screen.getByLabelText('Port'), { target: { value: '3306' } })
    fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'root' } })
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'secret' } })
    fireEvent.click(screen.getByRole('button', { name: '建立連線' }))

    await waitFor(() => expect(mockedCreateDBConnection).toHaveBeenCalledWith({
      name: 'mysql-conn',
      db_type: 'mysql',
      host: 'db.internal',
      port: 3306,
      database_name: null,
      username: 'root',
      password: 'secret',
      ssl_mode: 'prefer',
    }))
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
