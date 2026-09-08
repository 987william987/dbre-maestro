import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AdminQueryConsole } from '@/modules/sql-editor/components/AdminQueryConsole'
import { executeAdminQuery } from '@/modules/sql-editor/api'

vi.mock('@uiw/react-codemirror', () => ({
  default: ({ value, onChange }: { value: string; onChange: (value: string) => void }) => (
    <textarea aria-label="Administrator command" value={value} onChange={(event) => onChange(event.target.value)} />
  ),
}))

vi.mock('@/modules/sql-editor/api', () => ({
  executeAdminQuery: vi.fn(),
}))

const mockedExecuteAdminQuery = vi.mocked(executeAdminQuery)
const connection = {
  id: 1,
  name: 'Primary MySQL',
  db_type: 'mysql',
  host: 'readonly.local',
  port: 3306,
  database_name: 'maestro',
  username: 'reader',
  encryption_key_version: 1,
  ssl_mode: 'prefer',
  created_by: 1,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

describe('AdminQueryConsole', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: vi.fn().mockResolvedValue(undefined) }, configurable: true })
  })

  it('只透過管理員 API 執行，並在獨立 session 紀錄多筆結果', async () => {
    mockedExecuteAdminQuery
      .mockResolvedValueOnce({ columns: [], rows: [], row_count: 0, duration_ms: 8, command: 'UPDATE', affected_rows: 2 })
      .mockResolvedValueOnce({ columns: ['id'], rows: [['7']], row_count: 1, duration_ms: 5, command: 'QUERY', affected_rows: 0 })

    render(
      <AdminQueryConsole
        connection={connection}
        database="maestro"
        schema=""
        endpoint="primary.local:3306"
        credentialRole="readwrite"
        onExit={vi.fn()}
      />,
    )

    const editor = screen.getByLabelText('Administrator command')
    fireEvent.change(editor, { target: { value: 'UPDATE tickets SET status = 2 WHERE id = 7' } })
    fireEvent.click(screen.getByRole('button', { name: 'Execute' }))
    expect(await screen.findByText('UPDATE OK, 2 rows affected')).toBeInTheDocument()

    fireEvent.change(editor, { target: { value: 'SELECT id FROM tickets WHERE id = 7' } })
    fireEvent.click(screen.getByRole('button', { name: 'Execute' }))
    expect(await screen.findByText('7')).toBeInTheDocument()

    expect(screen.getByText(/UPDATE tickets SET status/)).toBeInTheDocument()
    expect(screen.getByText(/SELECT id FROM tickets/)).toBeInTheDocument()
    expect(mockedExecuteAdminQuery).toHaveBeenNthCalledWith(1, {
      db_connection_id: 1,
      sql: 'UPDATE tickets SET status = 2 WHERE id = 7',
      database: 'maestro',
      schema: undefined,
      redis_db_index: undefined,
    })
    expect(mockedExecuteAdminQuery).toHaveBeenCalledTimes(2)
  })

  it('退出由外層卸載 console，且不會把 session 狀態傳回 SQL Editor', async () => {
    const onExit = vi.fn()
    render(
      <AdminQueryConsole
        connection={connection}
        database="maestro"
        schema=""
        endpoint="primary.local:3306"
        credentialRole="readwrite"
        onExit={onExit}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Exit admin mode' }))
    await waitFor(() => expect(onExit).toHaveBeenCalledOnce())
    expect(mockedExecuteAdminQuery).not.toHaveBeenCalled()
  })

  it('用上下鍵瀏覽 session 命令歷史，並在回到底部時恢復草稿', async () => {
    mockedExecuteAdminQuery.mockResolvedValue({ columns: [], rows: [], row_count: 0, duration_ms: 1, command: 'QUERY' })
    render(<AdminQueryConsole connection={connection} database="maestro" schema="" endpoint="primary.local:3306" credentialRole="readwrite" onExit={vi.fn()} />)
    const editor = screen.getByLabelText('Administrator command')

    fireEvent.change(editor, { target: { value: 'SELECT 1' } })
    fireEvent.click(screen.getByRole('button', { name: 'Execute' }))
    await waitFor(() => expect(mockedExecuteAdminQuery).toHaveBeenCalledOnce())
    fireEvent.change(editor, { target: { value: 'draft command' } })
    fireEvent.keyDown(editor, { key: 'ArrowUp' })
    expect(editor).toHaveValue('SELECT 1')
    fireEvent.keyDown(editor, { key: 'ArrowDown' })
    expect(editor).toHaveValue('draft command')
  })

  it('結果支援篩選、豎向展示、複製全部與獨立 CSV 下載', async () => {
    mockedExecuteAdminQuery.mockResolvedValue({ columns: ['id', 'name'], rows: [['1', 'Alice'], ['2', 'Bob']], row_count: 2, duration_ms: 2 })
    const createObjectURL = vi.fn().mockReturnValue('blob:test')
    const revokeObjectURL = vi.fn()
    Object.defineProperty(URL, 'createObjectURL', { value: createObjectURL, configurable: true })
    Object.defineProperty(URL, 'revokeObjectURL', { value: revokeObjectURL, configurable: true })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
    render(<AdminQueryConsole connection={connection} database="maestro" schema="" endpoint="primary.local:3306" credentialRole="readwrite" onExit={vi.fn()} />)

    fireEvent.change(screen.getByLabelText('Administrator command'), { target: { value: 'SELECT id, name FROM users' } })
    fireEvent.click(screen.getByRole('button', { name: 'Execute' }))
    expect(await screen.findByText('Alice')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Filter result 1'), { target: { value: 'Bob' } })
    expect(screen.queryByText('Alice')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Vertical view' }))
    expect(screen.getByText('Bob')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Copy all' }))
    await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalledWith('id\tname\n1\tAlice\n2\tBob'))
    fireEvent.click(screen.getByRole('button', { name: 'Export' }))
    expect(createObjectURL).toHaveBeenCalledOnce()
    expect(click).toHaveBeenCalledOnce()
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:test')
  })

  it('MySQL USE 成功後會保存 session database，讓後續 SHOW TABLES 使用同一個庫', async () => {
    mockedExecuteAdminQuery
      .mockResolvedValueOnce({ columns: [], rows: [], row_count: 0, duration_ms: 1, command: 'USE', affected_rows: 0 })
      .mockResolvedValueOnce({ columns: ['Tables_in_testnet_dbms'], rows: [['users']], row_count: 1, duration_ms: 2 })
    render(<AdminQueryConsole connection={connection} database="" schema="" endpoint="primary.local:3306" credentialRole="readwrite" onExit={vi.fn()} />)
    const editor = screen.getByLabelText('Administrator command')

    fireEvent.change(editor, { target: { value: 'USE `testnet_dbms`;' } })
    fireEvent.click(screen.getByRole('button', { name: 'Execute' }))
    expect(await screen.findByText('USE OK, 0 rows affected')).toBeInTheDocument()

    fireEvent.change(editor, { target: { value: 'SHOW TABLES' } })
    fireEvent.click(screen.getByRole('button', { name: 'Execute' }))
    expect(await screen.findByText('users')).toBeInTheDocument()
    expect(mockedExecuteAdminQuery).toHaveBeenNthCalledWith(1, expect.objectContaining({ database: undefined }))
    expect(mockedExecuteAdminQuery).toHaveBeenNthCalledWith(2, expect.objectContaining({ database: 'testnet_dbms', sql: 'SHOW TABLES' }))
    expect(screen.getByText(/readwrite · testnet_dbms/)).toBeInTheDocument()
  })

  it('PostgreSQL \\c 成功後會切換 database 並清除舊 schema context', async () => {
    mockedExecuteAdminQuery
      .mockResolvedValueOnce({ columns: ['Database'], rows: [['app']], row_count: 1, duration_ms: 1, command: 'QUERY' })
      .mockResolvedValueOnce({ columns: ['Name'], rows: [['users']], row_count: 1, duration_ms: 2, command: 'QUERY' })
    const postgresConnection = { ...connection, name: 'Primary PostgreSQL', db_type: 'postgres', port: 5432 }
    render(<AdminQueryConsole connection={postgresConnection} database="postgres" schema="public" endpoint="primary.local:5432" credentialRole="readwrite" onExit={vi.fn()} />)
    const editor = screen.getByLabelText('Administrator command')

    fireEvent.change(editor, { target: { value: '\\c app' } })
    fireEvent.click(screen.getByRole('button', { name: 'Execute' }))
    expect(await screen.findByText('app')).toBeInTheDocument()

    fireEvent.change(editor, { target: { value: '\\dt' } })
    fireEvent.click(screen.getByRole('button', { name: 'Execute' }))
    expect(await screen.findByText('users')).toBeInTheDocument()
    expect(mockedExecuteAdminQuery).toHaveBeenNthCalledWith(2, expect.objectContaining({ database: 'app', schema: undefined, sql: '\\dt' }))
    expect(screen.getByText(/readwrite · app/)).toBeInTheDocument()
  })
})
