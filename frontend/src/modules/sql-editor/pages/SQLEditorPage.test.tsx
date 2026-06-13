import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ToastProvider } from '@/shared/ui/ToastContext'
import { SQLEditorPage } from '@/modules/sql-editor/pages/SQLEditorPage'
import { ApiError } from '@/shared/api/client'

vi.mock('@uiw/react-codemirror', () => ({
  default: ({
    value,
    onChange,
    onStatistics,
  }: {
    value: string
    onChange: (value: string) => void
    onStatistics?: (stats: { selectedText: boolean; selectionCode: string }) => void
  }) => (
    <textarea
      aria-label="CodeMirror"
      value={value}
      onChange={(event) => {
        onChange(event.target.value)
        const selectionStart = event.target.selectionStart ?? 0
        const selectionEnd = event.target.selectionEnd ?? 0
        onStatistics?.({
          selectedText: selectionEnd > selectionStart,
          selectionCode: event.target.value.slice(selectionStart, selectionEnd),
        })
      }}
      onSelect={(event) => {
        const target = event.target as HTMLTextAreaElement
        const selectionStart = target.selectionStart ?? 0
        const selectionEnd = target.selectionEnd ?? 0
        onStatistics?.({
          selectedText: selectionEnd > selectionStart,
          selectionCode: target.value.slice(selectionStart, selectionEnd),
        })
      }}
    />
  ),
}))

vi.mock('@/shared/auth/AuthContext', () => ({
  useAuth: vi.fn(),
}))

vi.mock('@/modules/db-connections/api', () => ({
  listDBConnections: vi.fn(),
}))

vi.mock('@/modules/sql-editor/api', () => ({
  executeQuery: vi.fn(),
  listMetadata: vi.fn(),
  listMetadataColumns: vi.fn(),
  listMetadataDefinition: vi.fn(),
  listQueryHistory: vi.fn(),
  listSavedQueries: vi.fn(),
  createSavedQuery: vi.fn(),
  deleteSavedQuery: vi.fn(),
  createSensitiveAccessTicket: vi.fn(),
}))

vi.mock('@/modules/exports/api', () => ({
  createExportRequest: vi.fn(),
}))

import { listDBConnections } from '@/modules/db-connections/api'
import { createExportRequest } from '@/modules/exports/api'
import { createSavedQuery, deleteSavedQuery, executeQuery, listMetadata, listMetadataColumns, listMetadataDefinition, listQueryHistory, listSavedQueries } from '@/modules/sql-editor/api'
import { useAuth } from '@/shared/auth/AuthContext'

const mockedListDBConnections = vi.mocked(listDBConnections)
const mockedExecuteQuery = vi.mocked(executeQuery)
const mockedListMetadata = vi.mocked(listMetadata)
const mockedListMetadataColumns = vi.mocked(listMetadataColumns)
const mockedListMetadataDefinition = vi.mocked(listMetadataDefinition)
const mockedListQueryHistory = vi.mocked(listQueryHistory)
const mockedListSavedQueries = vi.mocked(listSavedQueries)
const mockedCreateSavedQuery = vi.mocked(createSavedQuery)
const mockedDeleteSavedQuery = vi.mocked(deleteSavedQuery)
const mockedCreateExportRequest = vi.mocked(createExportRequest)
const mockedUseAuth = vi.mocked(useAuth)
const storage = new Map<string, string>()

describe('SQLEditorPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    storage.clear()
    mockedUseAuth.mockReturnValue({
      user: {
        id: 7,
        username: 'admin',
        authGroups: ['admin'],
        authGroupDetails: [],
        permissions: [],
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
    })

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

    mockedListDBConnections.mockResolvedValue({
      connections: [
        {
          id: 1,
          name: 'Primary MySQL',
          db_type: 'mysql',
          host: 'db.local',
          port: 3306,
          database_name: 'maestro',
          username: 'root',
          encryption_key_version: 1,
          ssl_mode: 'prefer',
          extra_params: null,
          created_by: 1,
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
      ],
    })

    mockedListMetadata.mockResolvedValue({
      db_type: 'mysql',
      level: 'table',
      database: 'maestro',
      items: [
        {
          kind: 'table',
          database: 'maestro',
          schema: 'maestro',
          name: 'tickets',
          engine: 'InnoDB',
          row_count: 12,
          data_size_bytes: 1024,
          index_size_bytes: 512,
          comment: '',
        },
      ],
    })

    mockedListMetadataColumns.mockResolvedValue({
      schema: 'maestro',
      table: 'tickets',
      columns: [
        {
          name: 'id',
          data_type: 'bigint',
          column_type: 'bigint unsigned',
          is_nullable: 'NO',
          default: '',
          comment: '',
        },
      ],
    })
    mockedListMetadataDefinition.mockResolvedValue({
      database: 'maestro',
      schema: 'maestro',
      table: 'tickets',
      definition: 'CREATE TABLE `tickets` (\n  `id` bigint unsigned NOT NULL\n);',
    })
    mockedListQueryHistory.mockResolvedValue({
      history: [],
    })
    mockedListSavedQueries.mockResolvedValue({
      saved_queries: [],
    })
    mockedCreateSavedQuery.mockResolvedValue({
      id: 1,
      label: 'Query 1',
      db_connection_id: 1,
      db_connection_name: 'Primary MySQL',
      database_name: 'maestro',
      schema_name: null,
      redis_db_index: null,
      sql_content: 'SELECT 1;',
      created_at: '2026-06-11T00:00:00Z',
      updated_at: '2026-06-11T00:00:00Z',
    })
    mockedDeleteSavedQuery.mockResolvedValue(undefined)
  })

  it('會以 user.id 為 namespace 保存 editor 狀態', async () => {
    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL Editor')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('CodeMirror'), {
      target: { value: 'SELECT * FROM tickets;' },
    })

    await waitFor(() => {
      const saved = window.localStorage.getItem('dbre_maestro.sql_editor.7')
      expect(saved).toContain('SELECT * FROM tickets;')
    })
  })

  it('執行查詢後會顯示結果並可建立匯出請求', async () => {
    mockedExecuteQuery.mockResolvedValue({
      columns: ['id', 'title'],
      raw_columns: ['id', 'title'],
      sensitive_column_indexes: [],
      rows: [[1, 'Test ticket']],
      row_count: 1,
      duration_ms: 18,
    })

    mockedCreateExportRequest.mockResolvedValue({
      ticket_id: 10,
      ticket_no: 'T-010',
      status: 'pending_review',
      contains_sensitive: false,
      scope_count: 1,
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL Editor')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))

    fireEvent.click(screen.getByText('Run Query'))

    expect(await screen.findByText('Test ticket')).toBeInTheDocument()
    expect(mockedExecuteQuery).toHaveBeenCalledWith({
      db_connection_id: 1,
      sql: 'SELECT 1;',
      database: undefined,
      schema: undefined,
      redis_db_index: undefined,
    })

    fireEvent.click(screen.getByText('EXPORT'))

    await waitFor(() => {
      expect(mockedCreateExportRequest).toHaveBeenCalledWith({
        db_connection_id: 1,
        sql_content: 'SELECT 1;',
        database_name: undefined,
        schema_name: undefined,
      })
    })
  })

  it('使用者反白部分 SQL 時，只應執行反白的內容', async () => {
    mockedExecuteQuery.mockResolvedValue({
      columns: ['id'],
      raw_columns: ['id'],
      sensitive_column_indexes: [],
      rows: [[1]],
      row_count: 1,
      duration_ms: 12,
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL Editor')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))

    const editor = screen.getByLabelText('CodeMirror') as HTMLTextAreaElement
    fireEvent.change(editor, {
      target: { value: 'SELECT 1;\nSELECT * FROM tickets;' },
    })
    editor.setSelectionRange(10, 32)
    fireEvent.select(editor)

    fireEvent.click(screen.getByText('Run Query'))

    await waitFor(() => {
      expect(mockedExecuteQuery).toHaveBeenCalledWith({
        db_connection_id: 1,
        sql: 'SELECT * FROM tickets;',
        database: undefined,
        schema: undefined,
        redis_db_index: undefined,
      })
    })
  })

  it('執行查詢後應自動切回 Result 分頁', async () => {
    mockedExecuteQuery.mockResolvedValue({
      columns: ['id'],
      raw_columns: ['id'],
      sensitive_column_indexes: [],
      rows: [[1]],
      row_count: 1,
      duration_ms: 12,
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL Editor')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(screen.getByRole('button', { name: 'History' }))
    fireEvent.click(screen.getByText('Run Query'))

    expect(await screen.findByText('1 rows / 12 ms')).toBeInTheDocument()
  })

  it('初始進入 SQL Editor 時不應自動選擇第一個實例，也不應自動載入 metadata', async () => {
    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL Editor')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Asset Selector' })).toBeInTheDocument()
    expect(mockedListMetadata).not.toHaveBeenCalled()
  })

  it('只應展示使用者有 resource 權限的實例', async () => {
    mockedUseAuth.mockReturnValue({
      user: {
        id: 7,
        username: 'reader',
        authGroups: ['reader'],
        authGroupDetails: [],
        permissions: ['sql_editor.query'],
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
    })
    mockedListDBConnections.mockResolvedValue({
      connections: [
        {
          id: 1,
          name: 'Primary MySQL',
          db_type: 'mysql',
          host: 'db.local',
          port: 3306,
          database_name: 'maestro',
          username: 'root',
          encryption_key_version: 1,
          ssl_mode: 'prefer',
          extra_params: null,
          created_by: 1,
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
        {
          id: 2,
          name: 'Hidden Redis',
          db_type: 'redis',
          host: 'redis.local',
          port: 6379,
          database_name: null,
          username: '',
          encryption_key_version: 1,
          ssl_mode: 'prefer',
          extra_params: null,
          created_by: 1,
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
      ],
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL Editor')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    expect(screen.getByText('Primary MySQL')).toBeInTheDocument()
    expect(screen.queryByText('Hidden Redis')).not.toBeInTheDocument()
  })

  it('沒有任何 resource 權限時，下拉不應展示實例', async () => {
    mockedUseAuth.mockReturnValue({
      user: {
        id: 8,
        username: 'no-resource',
        authGroups: ['reader'],
        authGroupDetails: [],
        permissions: ['sql_editor.query'],
        dbConnectionIds: [],
        protected: false,
        isActive: true,
      },
      status: 'authenticated',
      isAuthenticated: true,
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL Editor')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    expect(screen.getByText('沒有符合的 asset。')).toBeInTheDocument()
  })

  it('只有在使用者手動展開 root connection 時才載入 metadata', async () => {
    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL Editor')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))

    expect(mockedListMetadata).not.toHaveBeenCalled()

    fireEvent.click(screen.getByLabelText('Toggle Primary MySQL'))

    await waitFor(() => {
      expect(mockedListMetadata).toHaveBeenCalledTimes(1)
      expect(mockedListMetadata).toHaveBeenCalledWith(1)
    })
  })

  it('global.sensitive 啟用時會顯示 override 提示', async () => {
    mockedUseAuth.mockReturnValue({
      user: {
        id: 7,
        username: 'admin',
        authGroups: ['admin'],
        authGroupDetails: [],
        permissions: ['global.sensitive'],
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
    })
    mockedExecuteQuery.mockResolvedValue({
      columns: ['id'],
      raw_columns: ['id'],
      sensitive_column_indexes: [],
      rows: [[1]],
      row_count: 1,
      duration_ms: 12,
      sensitive_override_active: true,
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL Editor')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(screen.getByText('Run Query'))

    expect(await screen.findByText(/Sensitive override active/i)).toBeInTheDocument()
  })

  it('查詢結果 rows 形狀異常時不應讓頁面崩潰', async () => {
    mockedExecuteQuery.mockResolvedValue({
      columns: ['id'],
      raw_columns: ['id'],
      sensitive_column_indexes: [],
      rows: [null as unknown as Array<string | number | boolean | null>],
      row_count: 1,
      duration_ms: 12,
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL Editor')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(screen.getByText('Run Query'))

    expect(await screen.findByText('1 rows / 12 ms')).toBeInTheDocument()
  })

  it('查詢失敗時只應在下方顯示一次錯誤訊息', async () => {
    mockedExecuteQuery.mockRejectedValue(new ApiError(422, 'query failed: syntax error', null))

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL Editor')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(screen.getByText('Run Query'))

    await waitFor(() => {
      expect(screen.getAllByText('query failed: syntax error')).toHaveLength(1)
    })
  })

  it('點擊資料表後會切到 Object Meta 分頁並顯示表結構', async () => {
    mockedListMetadata.mockReset()
    mockedListMetadata
      .mockResolvedValueOnce({
        db_type: 'mysql',
        level: 'database',
        items: [
          { kind: 'database', name: 'maestro' },
        ],
      })
      .mockResolvedValueOnce({
        db_type: 'mysql',
        level: 'table',
        database: 'maestro',
        items: [
          {
            kind: 'table',
            database: 'maestro',
            schema: 'maestro',
            name: 'tickets',
            engine: 'InnoDB',
            row_count: 12,
            data_size_bytes: 1024,
            index_size_bytes: 512,
            comment: '',
          },
        ],
      })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL Editor')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(screen.getByLabelText('Toggle Primary MySQL'))

    expect(await screen.findByText('maestro')).toBeInTheDocument()
    fireEvent.click(screen.getByLabelText('Toggle maestro'))
    expect(await screen.findByText('tickets')).toBeInTheDocument()
    fireEvent.click(screen.getByText('tickets'))

    expect(screen.getByRole('button', { name: 'Object Meta' })).toBeInTheDocument()
    expect(await screen.findByText('id')).toBeInTheDocument()
    expect(screen.getByText('bigint unsigned')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Definition' }))
    expect(await screen.findByText(/CREATE TABLE `tickets`/)).toBeInTheDocument()
  })

  it('可儲存常用 SQL、開啟 Saved 清單並刪除', async () => {
    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL Editor')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(screen.getByText('Save'))

    await waitFor(() => {
      expect(mockedCreateSavedQuery).toHaveBeenCalledWith({
        label: 'Query 1',
        db_connection_id: 1,
        database: undefined,
        schema: undefined,
        redis_db_index: undefined,
        sql_content: 'SELECT 1;',
      })
    })

    fireEvent.click(screen.getByText('Saved'))
    expect(await screen.findByLabelText('Delete saved query Query 1')).toBeInTheDocument()
    expect(screen.getAllByText('SELECT 1;').length).toBeGreaterThan(1)

    fireEvent.click(screen.getByLabelText('Delete saved query Query 1'))
    expect(await screen.findByText('刪除常用 SQL')).toBeInTheDocument()
    const deleteButtons = screen.getAllByText('刪除')
    fireEvent.click(deleteButtons[deleteButtons.length - 1])

    await waitFor(() => {
      expect(mockedDeleteSavedQuery).toHaveBeenCalledWith(1)
      expect(screen.queryByLabelText('Delete saved query Query 1')).not.toBeInTheDocument()
    })
  })

  it('查詢結果表頭顯示 display columns，但保留 raw_columns 給其他用途', async () => {
    mockedExecuteQuery.mockResolvedValue({
      columns: ['id', 'user_id', 'account_id'],
      raw_columns: ['t_deposit.id', 't_deposit.user_id', 't_deposit.account_id'],
      sensitive_column_indexes: [],
      rows: [[1, 2, 3]],
      row_count: 1,
      duration_ms: 12,
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL Editor')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(screen.getByText('Run Query'))

    expect(await screen.findByText('user_id')).toBeInTheDocument()
    expect(screen.getByText('account_id')).toBeInTheDocument()
    expect(screen.queryByText('t_deposit.user_id')).not.toBeInTheDocument()
  })

  it('敏感欄位會標紅且查詢結果支援分頁', async () => {
    mockedExecuteQuery.mockResolvedValue({
      columns: ['id', 'email'],
      raw_columns: ['t_user.id', 't_user.email'],
      sensitive_column_indexes: [1],
      rows: Array.from({ length: 51 }, (_, index) => [index + 1, `user-${index + 1}@example.com`]),
      row_count: 51,
      duration_ms: 1280,
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL Editor')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(screen.getByText('Run Query'))

    expect(await screen.findByText('Executed in 1.28s')).toBeInTheDocument()
    expect(screen.getByText('51 rows / 1280 ms / Page 1 / 2')).toBeInTheDocument()
    expect(screen.getByText('Showing 1–50 of 51')).toBeInTheDocument()
    expect(screen.getByText('user-1@example.com')).toBeInTheDocument()
    expect(screen.queryByText('user-51@example.com')).not.toBeInTheDocument()

    const emailHeader = screen.getByText('email').closest('th')
    expect(emailHeader?.className).toContain('text-[#b9381f]')

    fireEvent.click(screen.getByRole('button', { name: 'Next' }))

    expect(await screen.findByText('Showing 51–51 of 51')).toBeInTheDocument()
    expect(screen.getByText('user-51@example.com')).toBeInTheDocument()
    expect(screen.queryByText('user-1@example.com')).not.toBeInTheDocument()
  })

  it('MySQL 未指定 database 時會先顯示 database 清單', async () => {
    mockedListMetadata.mockReset()
    mockedUseAuth.mockReturnValue({
      user: {
        id: 7,
        username: 'admin',
        authGroups: ['admin'],
        authGroupDetails: [],
        permissions: ['sql_editor.query'],
        dbConnectionIds: [2],
        protected: false,
        isActive: true,
      },
      status: 'authenticated',
      isAuthenticated: true,
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedListDBConnections.mockResolvedValueOnce({
      connections: [
        {
          id: 2,
          name: 'Shared MySQL',
          db_type: 'mysql',
          host: 'db.local',
          port: 3306,
          database_name: null,
          username: 'root',
          encryption_key_version: 1,
          ssl_mode: 'prefer',
          extra_params: null,
          created_by: 1,
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
      ],
    })
    mockedListMetadata.mockImplementation(async (_connectionId, params) => {
      if (!params?.database) {
        return {
          db_type: 'mysql',
          level: 'database',
          items: [
            { kind: 'database', name: 'analytics' },
            { kind: 'database', name: 'maestro' },
          ],
        }
      }

      if (params.database === 'analytics') {
        return {
          db_type: 'mysql',
          level: 'table',
          database: 'analytics',
          items: [
            { kind: 'table', database: 'analytics', schema: 'analytics', name: 'orders' },
          ],
        }
      }

      return {
        db_type: 'mysql',
        level: 'table',
        database: 'maestro',
        items: [
          { kind: 'table', database: 'maestro', schema: 'maestro', name: 'tickets' },
        ],
      }
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL Editor')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Shared MySQL'))
    fireEvent.click(screen.getByLabelText('Toggle Shared MySQL'))

    await waitFor(() => {
      expect(mockedListMetadata).toHaveBeenCalledWith(2)
      expect(screen.getAllByText('Shared MySQL').length).toBeGreaterThan(0)
      expect(screen.getByText('analytics')).toBeInTheDocument()
    })
  })

  it('MySQL 即使設定 landing database，也會先顯示 database 清單', async () => {
    mockedListMetadata.mockReset()
    mockedUseAuth.mockReturnValue({
      user: {
        id: 7,
        username: 'admin',
        authGroups: ['admin'],
        authGroupDetails: [],
        permissions: ['sql_editor.query'],
        dbConnectionIds: [5],
        protected: false,
        isActive: true,
      },
      status: 'authenticated',
      isAuthenticated: true,
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedListDBConnections.mockResolvedValueOnce({
      connections: [
        {
          id: 5,
          name: 'Configured MySQL',
          db_type: 'mysql',
          host: 'db.local',
          port: 3306,
          database_name: 'maestro',
          username: 'root',
          encryption_key_version: 1,
          ssl_mode: 'prefer',
          extra_params: null,
          created_by: 1,
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
      ],
    })
    mockedListMetadata.mockResolvedValue({
      db_type: 'mysql',
      level: 'database',
      items: [
        { kind: 'database', name: 'analytics' },
        { kind: 'database', name: 'maestro' },
      ],
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL Editor')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Configured MySQL'))
    fireEvent.click(screen.getByLabelText('Toggle Configured MySQL'))

    await waitFor(() => {
      expect(mockedListMetadata).toHaveBeenCalledWith(5)
      expect(screen.getAllByText('Configured MySQL').length).toBeGreaterThan(0)
      expect(screen.getByText('analytics')).toBeInTheDocument()
      expect(screen.getAllByText('maestro').length).toBeGreaterThan(0)
    })
  })

  it('Postgres 即使設定 landing database，也會先顯示 database 清單', async () => {
    mockedListMetadata.mockReset()
    mockedUseAuth.mockReturnValue({
      user: {
        id: 7,
        username: 'admin',
        authGroups: ['admin'],
        authGroupDetails: [],
        permissions: ['sql_editor.query'],
        dbConnectionIds: [4],
        protected: false,
        isActive: true,
      },
      status: 'authenticated',
      isAuthenticated: true,
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedListDBConnections.mockResolvedValueOnce({
      connections: [
        {
          id: 4,
          name: 'Shared Postgres',
          db_type: 'postgres',
          host: 'pg.local',
          port: 5432,
          database_name: 'postgres',
          username: 'postgres',
          encryption_key_version: 1,
          ssl_mode: 'prefer',
          extra_params: null,
          created_by: 1,
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
      ],
    })
    mockedListMetadata.mockResolvedValue({
      db_type: 'postgres',
      level: 'database',
      items: [
        { kind: 'database', name: 'analytics' },
        { kind: 'database', name: 'postgres' },
      ],
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL Editor')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Shared Postgres'))
    fireEvent.click(screen.getByLabelText('Toggle Shared Postgres'))

    await waitFor(() => {
      expect(mockedListMetadata).toHaveBeenCalledWith(4)
      expect(screen.getAllByText('Shared Postgres').length).toBeGreaterThan(0)
      expect(screen.getByText('analytics')).toBeInTheDocument()
      expect(screen.getAllByText('postgres').length).toBeGreaterThan(0)
    })
  })

  it('Explorer Search 會過濾資產節點', async () => {
    mockedListMetadata.mockReset()
    mockedUseAuth.mockReturnValue({
      user: {
        id: 7,
        username: 'admin',
        authGroups: ['admin'],
        authGroupDetails: [],
        permissions: ['sql_editor.query'],
        dbConnectionIds: [3],
        protected: false,
        isActive: true,
      },
      status: 'authenticated',
      isAuthenticated: true,
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedListDBConnections.mockResolvedValueOnce({
      connections: [
        {
          id: 3,
          name: 'Search MySQL',
          db_type: 'mysql',
          host: 'db.local',
          port: 3306,
          database_name: null,
          username: 'root',
          encryption_key_version: 1,
          ssl_mode: 'prefer',
          extra_params: null,
          created_by: 1,
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
      ],
    })
    mockedListMetadata.mockResolvedValue({
      db_type: 'mysql',
      level: 'database',
      items: [
        { kind: 'database', name: 'analytics' },
        { kind: 'database', name: 'maestro' },
      ],
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL Editor')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Search MySQL'))
    fireEvent.click(screen.getByLabelText('Toggle Search MySQL'))

    await waitFor(() => {
      expect(screen.getByText('analytics')).toBeInTheDocument()
    })

    fireEvent.change(screen.getByLabelText('Explorer Search'), {
      target: { value: 'maes' },
    })

    await waitFor(() => {
      expect(screen.getAllByText('maestro').length).toBeGreaterThan(0)
    })
  })

  it('Explorer Search 可以搜尋未展開的 table', async () => {
    mockedListMetadata.mockReset()
    mockedUseAuth.mockReturnValue({
      user: {
        id: 7,
        username: 'admin',
        authGroups: ['admin'],
        authGroupDetails: [],
        permissions: ['sql_editor.query'],
        dbConnectionIds: [6],
        protected: false,
        isActive: true,
      },
      status: 'authenticated',
      isAuthenticated: true,
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedListDBConnections.mockResolvedValueOnce({
      connections: [
        {
          id: 6,
          name: 'Search Tables MySQL',
          db_type: 'mysql',
          host: 'db.local',
          port: 3306,
          database_name: null,
          username: 'root',
          encryption_key_version: 1,
          ssl_mode: 'prefer',
          extra_params: null,
          created_by: 1,
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
      ],
    })
    mockedListMetadata.mockImplementation(async (_connectionId, params) => {
      if (!params?.database) {
        return {
          db_type: 'mysql',
          level: 'database',
          items: [
            { kind: 'database', name: 'analytics' },
            { kind: 'database', name: 'maestro' },
          ],
        }
      }

      if (params.database === 'analytics') {
        return {
          db_type: 'mysql',
          level: 'table',
          database: 'analytics',
          items: [
            { kind: 'table', database: 'analytics', schema: 'analytics', name: 'orders' },
          ],
        }
      }

      return {
        db_type: 'mysql',
        level: 'table',
        database: 'maestro',
        items: [
          { kind: 'table', database: 'maestro', schema: 'maestro', name: 'tickets' },
        ],
      }
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL Editor')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Search Tables MySQL'))

    fireEvent.change(screen.getByLabelText('Explorer Search'), {
      target: { value: 'tickets' },
    })

    await waitFor(() => {
      expect(screen.getByText('tickets')).toBeInTheDocument()
      expect(screen.queryByText('orders')).not.toBeInTheDocument()
    })

    const metadataCallCount = mockedListMetadata.mock.calls.length
    fireEvent.click(screen.getByText('tickets'))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Object Meta' })).toBeInTheDocument()
    })
    expect(mockedListMetadata).toHaveBeenCalledTimes(metadataCallCount)
    expect(screen.queryByText('Searching assets...')).not.toBeInTheDocument()
  })
})
