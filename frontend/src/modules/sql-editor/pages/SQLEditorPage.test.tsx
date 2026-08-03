import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ToastProvider } from '@/shared/ui/ToastContext'
import { SQLEditorPage } from '@/modules/sql-editor/pages/SQLEditorPage'
import { clearSQLEditorWorkspaceSnapshot, getSQLEditorWorkspaceSnapshot } from '@/modules/sql-editor/workspaceMemory'
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

vi.mock('@/modules/sql-editor/api', () => ({
  cancelQueryExecution: vi.fn(),
  listQueryConnections: vi.fn(),
  getQueryConstraints: vi.fn(),
  executeQuery: vi.fn(),
  listMetadata: vi.fn(),
  listMetadataSearchIndex: vi.fn(),
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

import { createExportRequest } from '@/modules/exports/api'
import { cancelQueryExecution, createSavedQuery, createSensitiveAccessTicket, deleteSavedQuery, executeQuery, getQueryConstraints, listMetadata, listMetadataColumns, listMetadataDefinition, listMetadataSearchIndex, listQueryConnections, listQueryHistory, listSavedQueries } from '@/modules/sql-editor/api'
import { useAuth } from '@/shared/auth/AuthContext'

const mockedListQueryConnections = vi.mocked(listQueryConnections)
const mockedGetQueryConstraints = vi.mocked(getQueryConstraints)
const mockedExecuteQuery = vi.mocked(executeQuery)
const mockedCancelQueryExecution = vi.mocked(cancelQueryExecution)
const mockedListMetadata = vi.mocked(listMetadata)
const mockedListMetadataSearchIndex = vi.mocked(listMetadataSearchIndex)
const mockedListMetadataColumns = vi.mocked(listMetadataColumns)
const mockedListMetadataDefinition = vi.mocked(listMetadataDefinition)
const mockedListQueryHistory = vi.mocked(listQueryHistory)
const mockedListSavedQueries = vi.mocked(listSavedQueries)
const mockedCreateSavedQuery = vi.mocked(createSavedQuery)
const mockedDeleteSavedQuery = vi.mocked(deleteSavedQuery)
const mockedCreateExportRequest = vi.mocked(createExportRequest)
const mockedCreateSensitiveAccessTicket = vi.mocked(createSensitiveAccessTicket)
const mockedUseAuth = vi.mocked(useAuth)
const storage = new Map<string, string>()

describe('SQLEditorPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.clearAllMocks()
    clearSQLEditorWorkspaceSnapshot()
    storage.clear()
    mockedUseAuth.mockReturnValue({
      user: {
        id: 7,
        username: 'admin',
        authGroups: ['admin'],
        authGroupDetails: [],
        permissions: ['sql_editor.read', 'sql_editor.query', 'sql_editor.export', 'sql_editor.sensitive_apply', 'tickets.apply'],
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
    mockedCancelQueryExecution.mockResolvedValue({ cancel_requested: true })

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

    mockedListQueryConnections.mockResolvedValue({
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
    mockedGetQueryConstraints.mockResolvedValue({
      default_limit: 200,
      max_limit: 1000,
      app_timeout_seconds: 30,
      mysql_max_execution_time_ms: 25000,
      postgres_statement_timeout_ms: 25000,
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
    mockedListMetadataSearchIndex.mockResolvedValue({
      db_type: 'mysql',
      limit: 50000,
      truncated: false,
      items: [
        { kind: 'database', name: 'maestro', schema: 'maestro' },
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
      total: 0,
      limit: 20,
      offset: 0,
      retention_days: 90,
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

  it('read-only 模式不載入需要 query 權限的資料', async () => {
    mockedUseAuth.mockReturnValue({
      user: {
        id: 8,
        username: 'readonly',
        authGroups: ['readonly'],
        authGroupDetails: [],
        permissions: ['sql_editor.read'],
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

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    expect(mockedListQueryConnections).not.toHaveBeenCalled()
    expect(mockedGetQueryConstraints).not.toHaveBeenCalled()
    expect(mockedListQueryHistory).not.toHaveBeenCalled()
    expect(mockedListSavedQueries).not.toHaveBeenCalled()
  })

  it('同一個瀏覽器執行環境內重新掛載會保留 workspace 草稿，但不保留查詢結果', async () => {
    mockedListMetadata.mockImplementation(async (_connectionID, params) => {
      if (params?.database) {
        return {
          db_type: 'mysql',
          level: 'table',
          database: params.database,
          items: [
            {
              kind: 'table',
              database: params.database,
              schema: params.database,
              name: 'tickets',
              engine: 'InnoDB',
              row_count: 12,
              data_size_bytes: 1024,
              index_size_bytes: 512,
              comment: '',
            },
          ],
        }
      }

      return {
        db_type: 'mysql',
        level: 'database',
        items: [
          { kind: 'database', name: 'maestro' },
        ],
      }
    })
    mockedExecuteQuery.mockResolvedValue({
      columns: ['id', 'title'],
      raw_columns: ['id', 'title'],
      sensitive_column_indexes: [],
      rows: [['1', 'Test ticket']],
      row_count: 1,
      duration_ms: 18,
    })

    const { unmount } = render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    expect(screen.queryByText('Run read-only queries, browse metadata, and keep query history and saved queries in one workspace. Create export requests directly from the result panel.')).not.toBeInTheDocument()
    expect(screen.getByText('Select a connection to browse objects.')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('CodeMirror'), {
      target: { value: 'SELECT * FROM tickets;' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(await screen.findByText('maestro'))
    expect(await screen.findByText('tickets')).toBeInTheDocument()
    fireEvent.click(screen.getByText('Run'))
    expect(await screen.findByText('Test ticket')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'New Tab' }))

    expect(screen.getAllByText(/Query \d+/)).toHaveLength(2)
    await waitFor(() => {
      const snapshot = getSQLEditorWorkspaceSnapshot<unknown>('7')
      expect(JSON.stringify(snapshot)).toContain('tickets')
    })

    unmount()

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    expect(screen.getAllByText(/Query \d+/)).toHaveLength(2)
    fireEvent.click(screen.getByText('Query 1'))
    expect((screen.getByLabelText('CodeMirror') as HTMLTextAreaElement).value).toBe('SELECT * FROM tickets;')
    expect(await screen.findByText('maestro')).toBeInTheDocument()
    expect(screen.getByText('tickets')).toBeInTheDocument()
    expect(screen.queryByText('Test ticket')).not.toBeInTheDocument()
    expect(screen.getByText('No query has been executed yet.')).toBeInTheDocument()
  })

  it('點擊集群/庫名/表名節點時，同時展開並把名稱複製到剪貼簿', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })

    mockedListMetadata.mockImplementation(async (_connectionID, params) => {
      if (params?.database) {
        return {
          db_type: 'mysql',
          level: 'table',
          database: params.database,
          items: [
            {
              kind: 'table',
              database: params.database,
              schema: params.database,
              name: 'tickets',
              engine: 'InnoDB',
              row_count: 12,
              data_size_bytes: 1024,
              index_size_bytes: 512,
              comment: '',
            },
          ],
        }
      }

      return {
        db_type: 'mysql',
        level: 'database',
        items: [
          { kind: 'database', name: 'maestro' },
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

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))

    // Selecting a connection from the picker list also copies its name, and
    // shows a bare "Copied" indicator positioned next to the click, not a
    // bottom-right toast with the object name baked in.
    fireEvent.click(screen.getByText('Primary MySQL'), { clientX: 100, clientY: 200 })
    expect(await screen.findByText('maestro')).toBeInTheDocument()
    expect(writeText).toHaveBeenNthCalledWith(1, 'Primary MySQL')
    const firstIndicator = await screen.findByText('Copied')
    expect(firstIndicator.style.left).toBe('114px')
    expect(firstIndicator.style.top).toBe('200px')

    // Clicking the database node (an AssetTree node) both expands it (table
    // node appears) and copies its label — the two happen together.
    fireEvent.click(screen.getByText('maestro'), { clientX: 50, clientY: 60 })
    expect(await screen.findByText('tickets')).toBeInTheDocument()
    expect(writeText).toHaveBeenNthCalledWith(2, 'maestro')
    const secondIndicator = await screen.findByText('Copied')
    expect(secondIndicator.style.left).toBe('64px')
    expect(secondIndicator.style.top).toBe('60px')

    // Table nodes don't expand (no children), but the click still copies.
    fireEvent.click(screen.getByText('tickets'))
    expect(writeText).toHaveBeenNthCalledWith(3, 'tickets')
    expect(await screen.findByText('Copied')).toBeInTheDocument()

    vi.unstubAllGlobals()
  })

  it('access token 更新時不應清空 SQL Editor workspace', async () => {
    let accessToken = 'token-before-refresh'
    mockedUseAuth.mockImplementation(() => ({
      user: {
        id: 7,
        username: 'admin',
        authGroups: ['admin'],
        authGroupDetails: [],
        permissions: ['sql_editor.read', 'sql_editor.query', 'sql_editor.export', 'sql_editor.sensitive_apply', 'tickets.apply'],
        dbConnectionIds: [1],
        protected: false,
        isActive: true,
      },
      status: 'authenticated',
      isAuthenticated: true,
      accessToken,
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    }))

    const { rerender } = render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('CodeMirror'), {
      target: { value: 'SELECT * FROM tickets;' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))

    accessToken = 'token-after-refresh'
    rerender(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect((screen.getByLabelText('CodeMirror') as HTMLTextAreaElement).value).toBe('SELECT * FROM tickets;')
    expect(screen.getAllByText('Primary MySQL').length).toBeGreaterThan(0)
  })

  it('依照目前選取的連線類型顯示 SQL Editor timeout', async () => {
    mockedGetQueryConstraints.mockResolvedValueOnce({
      default_limit: 200,
      max_limit: 1000,
      app_timeout_seconds: 60,
      mysql_max_execution_time_ms: 45000,
      postgres_statement_timeout_ms: 25000,
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    expect(await screen.findByText('Timeout 60s')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(await screen.findByText('Primary MySQL'))
    expect(await screen.findByText('Timeout 45s')).toBeInTheDocument()
    await waitFor(() => expect(mockedListMetadata).toHaveBeenCalledWith(1))

    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    expect(screen.queryByText('Selected')).not.toBeInTheDocument()
  })

  it('執行查詢後會先顯示匯出確認窗，確認後才建立匯出請求', async () => {
    mockedExecuteQuery.mockResolvedValue({
      columns: ['id', 'title'],
      raw_columns: ['id', 'title'],
      sensitive_column_indexes: [],
      rows: [['1', 'Test ticket']],
      row_count: 1,
      duration_ms: 18,
      query_context_token: 'query-context-token',
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

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))

    fireEvent.click(screen.getByText('Run'))

    expect(await screen.findByText('Test ticket')).toBeInTheDocument()
    expect(mockedExecuteQuery).toHaveBeenCalledWith(expect.objectContaining({
      db_connection_id: 1,
      sql: 'SELECT 1;',
      database: undefined,
      schema: undefined,
      redis_db_index: undefined,
      query_execution_id: expect.any(String),
    }), expect.any(AbortSignal))

    fireEvent.click(screen.getByText('EXPORT'))

    expect(screen.getByText('Confirm Export Request')).toBeInTheDocument()
    expect(screen.getByText('Asset Context')).toBeInTheDocument()
    expect(screen.getAllByText('Primary MySQL').length).toBeGreaterThan(0)
    expect(screen.getAllByText('SELECT 1;').length).toBeGreaterThan(0)
    expect(mockedCreateExportRequest).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Confirm and Export' })).toBeDisabled()

    fireEvent.change(screen.getByLabelText('Export Reason'), { target: { value: 'Monthly compliance report' } })
    fireEvent.click(screen.getByRole('button', { name: 'Confirm and Export' }))

    await waitFor(() => {
      expect(mockedCreateExportRequest).toHaveBeenCalledWith({
        db_connection_id: 1,
        sql_content: 'SELECT 1;',
        database_name: undefined,
        schema_name: undefined,
        query_context_token: 'query-context-token',
        reason: 'Monthly compliance report',
      })
    })
  })

  it('Sensitive Access 會先要求輸入時長，再進確認窗建立工單', async () => {
    mockedUseAuth.mockReturnValue({
      user: {
        id: 7,
        username: 'admin',
        authGroups: ['admin'],
        authGroupDetails: [],
        permissions: ['sql_editor.read', 'sql_editor.query', 'sql_editor.sensitive_apply'],
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
      sensitive_column_indexes: [0],
      rows: [['1']],
      row_count: 1,
      duration_ms: 12,
      query_context_token: 'sensitive-query-context-token',
    })
    mockedCreateSensitiveAccessTicket.mockResolvedValue({
      ticket_id: 11,
      ticket_no: 'T-011',
      status: 'pending_review',
      scope_count: 1,
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(screen.getByText('Run'))
    expect(await screen.findByText('1 rows / 12 ms')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Sensitive Access' }))

    expect(screen.getByText('Set Sensitive Access Duration')).toBeInTheDocument()
    expect(screen.getByText('Quick Select')).toBeInTheDocument()
    expect(screen.getByText('Access Preview')).toBeInTheDocument()
    expect(screen.getByText('10 minutes')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Requested Access Duration (minutes)'), { target: { value: '120' } })
    expect(screen.getByText('2 hours')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))

    expect(screen.getByText('Confirm Sensitive Access Request')).toBeInTheDocument()
    expect(screen.getByText('Requested Access Duration')).toBeInTheDocument()
    expect(screen.getByText('120 minutes')).toBeInTheDocument()
    expect(mockedCreateSensitiveAccessTicket).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Confirm and Submit' }))
    expect(mockedCreateSensitiveAccessTicket).not.toHaveBeenCalled()

    fireEvent.change(screen.getByLabelText('Access Reason'), { target: { value: 'Need to verify customer identity.' } })
    fireEvent.click(screen.getByRole('button', { name: 'Confirm and Submit' }))

    await waitFor(() => {
      expect(mockedCreateSensitiveAccessTicket).toHaveBeenCalledWith({
        db_connection_id: 1,
        sql_content: 'SELECT 1;',
        database_name: undefined,
        schema_name: undefined,
        approved_duration_minutes: 120,
        query_context_token: 'sensitive-query-context-token',
        reason: 'Need to verify customer identity.',
      })
    })
  })

  it('使用者反白部分 SQL 時，只應執行反白的內容', async () => {
    mockedExecuteQuery.mockResolvedValue({
      columns: ['id'],
      raw_columns: ['id'],
      sensitive_column_indexes: [],
      rows: [['1']],
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

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))

    const editor = screen.getByLabelText('CodeMirror') as HTMLTextAreaElement
    fireEvent.change(editor, {
      target: { value: 'SELECT 1;\nSELECT * FROM tickets;' },
    })
    editor.setSelectionRange(10, 32)
    fireEvent.select(editor)

    fireEvent.click(screen.getByText('Run'))

    await waitFor(() => {
      expect(mockedExecuteQuery).toHaveBeenCalledWith(expect.objectContaining({
        db_connection_id: 1,
        sql: 'SELECT * FROM tickets;',
        database: undefined,
        schema: undefined,
        redis_db_index: undefined,
        query_execution_id: expect.any(String),
      }), expect.any(AbortSignal))
    })
  })

  it('執行查詢後應自動切回 Result 分頁', async () => {
    mockedExecuteQuery.mockResolvedValue({
      columns: ['id'],
      raw_columns: ['id'],
      sensitive_column_indexes: [],
      rows: [['1']],
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

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(screen.getByRole('button', { name: 'History' }))
    fireEvent.click(screen.getByText('Run'))

    expect(await screen.findByText('1 rows / 12 ms')).toBeInTheDocument()
  })

  it('查詢執行中可以按 Stop 中斷，不會顯示錯誤訊息', async () => {
    mockedExecuteQuery.mockImplementation((_payload, signal) => new Promise((_resolve, reject) => {
      signal?.addEventListener('abort', () => {
        reject(new DOMException('The user aborted a request.', 'AbortError'))
      })
    }))

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Stop' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(screen.getByRole('button', { name: 'History' }))

    fireEvent.click(screen.getByText('Run'))

    // Run and Stop are the same button — it swaps in place while running.
    const stopButton = await screen.findByRole('button', { name: 'Stop' })
    expect(screen.queryByRole('button', { name: 'Run' })).not.toBeInTheDocument()
    expect(screen.getAllByText('Running...').length).toBeGreaterThan(0)

    fireEvent.click(stopButton)

    await waitFor(() => {
      const payload = mockedExecuteQuery.mock.calls[0]?.[0] as { query_execution_id?: string }
      expect(mockedCancelQueryExecution).toHaveBeenCalledWith(payload.query_execution_id)
    })
    expect(await screen.findByText('Query stopped.')).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Stop' })).not.toBeInTheDocument()
    expect(screen.queryByText('Query execution failed.')).not.toBeInTheDocument()
  })

  it('Redis command 執行中不顯示 Stop，也不呼叫 cancel API', async () => {
    mockedUseAuth.mockReturnValue({
      user: {
        id: 7,
        username: 'admin',
        authGroups: ['admin'],
        authGroupDetails: [],
        permissions: ['sql_editor.read', 'sql_editor.query'],
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
    mockedListQueryConnections.mockResolvedValue({
      connections: [
        {
          id: 2,
          name: 'Redis Cache',
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
    mockedExecuteQuery.mockReturnValue(new Promise(() => undefined) as ReturnType<typeof executeQuery>)

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Redis Cache'))
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))

    await screen.findAllByRole('button', { name: 'Running...' })
    for (const runningButton of screen.getAllByRole('button', { name: 'Running...' })) {
      expect(runningButton).toBeDisabled()
    }
    expect(screen.queryByRole('button', { name: 'Stop' })).not.toBeInTheDocument()
    expect(mockedCancelQueryExecution).not.toHaveBeenCalled()
  })

  it('點擊 Explain 會用 EXPLAIN 包裝目前 SQL 後執行', async () => {
    mockedExecuteQuery.mockResolvedValue({
      columns: ['id', 'select_type'],
      raw_columns: ['id', 'select_type'],
      sensitive_column_indexes: [],
      rows: [['1', 'SIMPLE']],
      row_count: 1,
      duration_ms: 9,
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.change(screen.getByLabelText('CodeMirror'), {
      target: { value: 'SELECT * FROM tickets' },
    })

    fireEvent.click(screen.getByRole('button', { name: 'Explain' }))

    await waitFor(() => {
      expect(mockedExecuteQuery).toHaveBeenCalledWith(expect.objectContaining({
        db_connection_id: 1,
        sql: 'EXPLAIN SELECT * FROM tickets;',
        database: undefined,
        schema: undefined,
        redis_db_index: undefined,
        query_execution_id: expect.any(String),
      }), expect.any(AbortSignal))
    })
  })

  it('點擊 Format 會更新 editor 內容', async () => {
    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    const editor = screen.getByLabelText('CodeMirror') as HTMLTextAreaElement
    fireEvent.change(editor, {
      target: { value: 'select id, title from tickets where id = 1' },
    })

    fireEvent.click(screen.getByRole('button', { name: 'Format' }))

    expect((screen.getByLabelText('CodeMirror') as HTMLTextAreaElement).value).toContain('SELECT')
    expect((screen.getByLabelText('CodeMirror') as HTMLTextAreaElement).value).toContain('\nFROM')
  })

  it('初始進入 SQL Editor 時不應自動選擇第一個實例，也不應自動載入 metadata', async () => {
    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
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
        permissions: ['sql_editor.read', 'sql_editor.query'],
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
    mockedListQueryConnections.mockResolvedValue({
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

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
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
        permissions: ['sql_editor.read', 'sql_editor.query'],
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

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    expect(screen.getByText('No matching assets.')).toBeInTheDocument()
  })

  it('選取實例後會自動載入第一層 database', async () => {
    mockedListMetadata.mockResolvedValueOnce({
      db_type: 'mysql',
      level: 'database',
      items: [
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

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))

    await waitFor(() => {
      expect(mockedListMetadata).toHaveBeenCalledTimes(1)
      expect(mockedListMetadata).toHaveBeenCalledWith(1)
    })
    expect(screen.queryByLabelText('Toggle Primary MySQL')).not.toBeInTheDocument()
    expect(screen.getByText('maestro')).toBeInTheDocument()
  })

  it('metadata 載入失敗時只顯示前端暫時訊息，不外露底層錯誤', async () => {
    mockedListMetadata.mockRejectedValueOnce(new ApiError(
      500,
      'query metadata failed: pg_hba.conf rejects connection for host "10.183.27.22"',
      { error: 'query metadata failed: pg_hba.conf rejects connection for host "10.183.27.22"' },
    ))

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))

    expect(await screen.findByText('Metadata is temporarily unavailable. Please try again later.')).toBeInTheDocument()
    expect(screen.queryByText(/pg_hba\.conf rejects connection/i)).not.toBeInTheDocument()
  })

  it('global.sensitive 啟用時會顯示 override 提示', async () => {
    mockedUseAuth.mockReturnValue({
      user: {
        id: 7,
        username: 'admin',
        authGroups: ['admin'],
        authGroupDetails: [],
        permissions: ['sql_editor.read', 'sql_editor.query', 'global.sensitive'],
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
      rows: [['1']],
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

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(screen.getByText('Run'))

    expect(await screen.findByText(/Sensitive override active/i)).toBeInTheDocument()
  })

  it('查詢結果 rows 形狀異常時不應讓頁面崩潰', async () => {
    mockedExecuteQuery.mockResolvedValue({
      columns: ['id'],
      raw_columns: ['id'],
      sensitive_column_indexes: [],
      rows: [null as unknown as Array<string | null>],
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

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(screen.getByText('Run'))

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

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(screen.getByText('Run'))

    await waitFor(() => {
      expect(screen.getAllByText('query failed: syntax error')).toHaveLength(1)
    })
    expect(screen.getByRole('button', { name: 'Query Access' }).parentElement).not.toHaveAttribute('data-attention-active')
  })

  it('缺 Query Access 時只保留常駐 Query Access 入口', async () => {
    mockedExecuteQuery.mockRejectedValue(new ApiError(422, 'You do not have query access to maestro.tickets', null))

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(screen.getByText('Run'))

    expect(await screen.findByText('You do not have query access to maestro.tickets')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: 'Query Access' })).toHaveLength(1)
    expect(screen.queryByRole('button', { name: 'Apply Query Access' })).not.toBeInTheDocument()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Query Access' }).parentElement).toHaveAttribute('data-attention-active', 'true')
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

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))

    expect(await screen.findByText('maestro')).toBeInTheDocument()
    fireEvent.click(screen.getByText('maestro'))
    expect(await screen.findByText('tickets')).toBeInTheDocument()
    fireEvent.click(screen.getByText('tickets'))

    expect(screen.getByRole('button', { name: 'Object Meta' })).toBeInTheDocument()
    expect((screen.getByLabelText('CodeMirror') as HTMLTextAreaElement).value).toBe('SELECT * FROM tickets;')
    expect(await screen.findByText('id')).toBeInTheDocument()
    expect(screen.getByText('bigint unsigned')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Definition' }))
    expect(await screen.findByText(/CREATE TABLE `tickets`/)).toBeInTheDocument()
  })

  it('點擊資料表時不會覆蓋使用者已輸入的 SQL', async () => {
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

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('CodeMirror'), {
      target: { value: 'SELECT id FROM users;' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))

    expect(await screen.findByText('maestro')).toBeInTheDocument()
    fireEvent.click(screen.getByText('maestro'))
    expect(await screen.findByText('tickets')).toBeInTheDocument()
    fireEvent.click(screen.getByText('tickets'))

    expect((screen.getByLabelText('CodeMirror') as HTMLTextAreaElement).value).toBe('SELECT id FROM users;')
  })

  it('可儲存常用 SQL、開啟 Saved 清單並刪除', async () => {
    mockedCreateSavedQuery.mockResolvedValueOnce({
      id: 1,
      label: 'User lookup',
      db_connection_id: 1,
      db_connection_name: 'Primary MySQL',
      database_name: null,
      schema_name: null,
      redis_db_index: null,
      sql_content: 'SELECT 1;',
      created_at: '2026-06-11T00:00:00Z',
      updated_at: '2026-06-11T00:00:00Z',
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(screen.getByText('Save'))
    expect(await screen.findByText('Save SQL')).toBeInTheDocument()
    expect(screen.getByText('SQL To Save')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Alias'), { target: { value: 'User lookup' } })
    const saveButtons = screen.getAllByRole('button', { name: 'Save' })
    fireEvent.click(saveButtons[saveButtons.length - 1])

    await waitFor(() => {
      expect(mockedCreateSavedQuery).toHaveBeenCalledWith({
        label: 'User lookup',
        db_connection_id: 1,
        database: undefined,
        schema: undefined,
        redis_db_index: undefined,
        sql_content: 'SELECT 1;',
      })
    })

    fireEvent.click(screen.getAllByRole('button', { name: 'Saved' })[0])
    expect(await screen.findByLabelText('Delete saved query User lookup')).toBeInTheDocument()
    expect(screen.getAllByText('SELECT 1;').length).toBeGreaterThan(1)

    fireEvent.click(screen.getByLabelText('Delete saved query User lookup'))
    expect(await screen.findByText('Delete Saved Query')).toBeInTheDocument()
    const deleteButtons = screen.getAllByText('Delete')
    fireEvent.click(deleteButtons[deleteButtons.length - 1])

    await waitFor(() => {
      expect(mockedDeleteSavedQuery).toHaveBeenCalledWith(1)
      expect(screen.queryByLabelText('Delete saved query User lookup')).not.toBeInTheDocument()
    })
  })

  it('反白部分 SQL 時只會將選取片段儲存為常用 SQL', async () => {
    mockedCreateSavedQuery.mockResolvedValueOnce({
      id: 2,
      label: 'Claim list',
      db_connection_id: 1,
      db_connection_name: 'Primary MySQL',
      database_name: null,
      schema_name: null,
      redis_db_index: null,
      sql_content: 'SELECT * FROM act_claim;',
      created_at: '2026-06-11T00:00:00Z',
      updated_at: '2026-06-11T00:00:00Z',
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))

    const editor = screen.getByLabelText('CodeMirror') as HTMLTextAreaElement
    fireEvent.change(editor, {
      target: { value: 'SELECT 1;\nSELECT * FROM act_claim;' },
    })
    editor.setSelectionRange(10, 34)
    fireEvent.select(editor)

    fireEvent.click(screen.getByText('Save'))
    expect(await screen.findByText('Save SQL')).toBeInTheDocument()
    expect(screen.getByText('SELECT * FROM act_claim;')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Alias'), { target: { value: 'Claim list' } })
    const saveButtons = screen.getAllByRole('button', { name: 'Save' })
    fireEvent.click(saveButtons[saveButtons.length - 1])

    await waitFor(() => {
      expect(mockedCreateSavedQuery).toHaveBeenCalledWith({
        label: 'Claim list',
        db_connection_id: 1,
        database: undefined,
        schema: undefined,
        redis_db_index: undefined,
        sql_content: 'SELECT * FROM act_claim;',
      })
    })
  })

  it('套用 History 查詢時保留工作區標題並同步顯示 database context', async () => {
    mockedListMetadata.mockReset()
    mockedListMetadata.mockImplementation(async (_connectionId, params) => {
      if (!params?.database) {
        return {
          db_type: 'mysql',
          level: 'database',
          items: [{
            kind: 'database',
            name: 'maestro',
            database: 'maestro',
            schema: 'maestro',
          }],
        }
      }
      return {
        db_type: 'mysql',
        level: 'table',
        database: params.database,
        items: [{
          kind: 'table',
          name: 'tickets',
          database: params.database,
          schema: params.database,
        }],
      }
    })
    mockedListQueryHistory.mockResolvedValueOnce({
      history: [{
        id: 9,
        db_connection_id: 1,
        db_connection_name: 'Primary MySQL',
        database_name: 'maestro',
        schema_name: null,
        redis_db_index: null,
        sql_content: 'SELECT * FROM tickets;',
        row_count: 12,
        duration_ms: 123,
        created_at: '2026-07-22T12:50:20Z',
      }],
      total: 1,
      limit: 20,
      offset: 0,
      retention_days: 90,
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByText('History'))

    expect(await screen.findByText('SELECT * FROM tickets;')).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: 'SQL' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: 'Connection' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: 'Schema' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: 'Rows' })).toBeInTheDocument()
    expect(screen.getByText('Primary MySQL')).toBeInTheDocument()
    expect(screen.getByText('maestro')).toBeInTheDocument()
    expect(screen.getByText('12')).toBeInTheDocument()
    expect(screen.getByText('123 ms')).toBeInTheDocument()

    fireEvent.click(screen.getByText('SELECT * FROM tickets;'))

    await waitFor(() => {
      expect(screen.getByLabelText('Close Query 1')).toBeInTheDocument()
      expect(mockedListMetadata).toHaveBeenCalledWith(1, { database: 'maestro' })
    })
  })

  it('History 超過一頁時使用 Pagination 載入下一頁', async () => {
    mockedListQueryHistory
      .mockResolvedValueOnce({
        history: [{
          id: 9,
          db_connection_id: 1,
          db_connection_name: 'Primary MySQL',
          database_name: 'maestro',
          schema_name: null,
          redis_db_index: null,
          sql_content: 'SELECT * FROM tickets;',
          row_count: 12,
          duration_ms: 123,
          created_at: '2026-07-22T12:50:20Z',
        }],
        total: 21,
        limit: 20,
        offset: 0,
        retention_days: 90,
      })
      .mockResolvedValueOnce({
        history: [{
          id: 10,
          db_connection_id: 1,
          db_connection_name: 'Primary MySQL',
          database_name: 'maestro',
          schema_name: null,
          redis_db_index: null,
          sql_content: 'SELECT * FROM tickets_archive;',
          row_count: 3,
          duration_ms: 88,
          created_at: '2026-07-21T12:50:20Z',
        }],
        total: 21,
        limit: 20,
        offset: 20,
        retention_days: 90,
      })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByText('History'))

    expect(await screen.findByText('SELECT * FROM tickets;')).toBeInTheDocument()
    expect(screen.getByText('Showing 1–1 of 21')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Next' }))

    expect(await screen.findByText('SELECT * FROM tickets_archive;')).toBeInTheDocument()
    expect(mockedListQueryHistory).toHaveBeenLastCalledWith(20, 20)
  })

  it('查詢結果表頭顯示 display columns，但保留 raw_columns 給其他用途', async () => {
    mockedExecuteQuery.mockResolvedValue({
      columns: ['id', 'user_id', 'account_id'],
      raw_columns: ['t_deposit.id', 't_deposit.user_id', 't_deposit.account_id'],
      sensitive_column_indexes: [],
      rows: [['1', '2', '3']],
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

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(screen.getByText('Run'))

    expect(await screen.findByText('user_id')).toBeInTheDocument()
    expect(screen.getByText('account_id')).toBeInTheDocument()
    expect(screen.queryByText('t_deposit.user_id')).not.toBeInTheDocument()
  })

  it('敏感欄位會顯示提示圖示與 tooltip，且查詢結果支援分頁', async () => {
    mockedExecuteQuery.mockResolvedValue({
      columns: ['id', 'email'],
      raw_columns: ['t_user.id', 't_user.email'],
      sensitive_column_indexes: [1],
      rows: Array.from({ length: 51 }, (_, index) => [String(index + 1), `user-${index + 1}@example.com`]),
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

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(screen.getByText('Run'))

    expect(await screen.findByText('Executed in 1.28s')).toBeInTheDocument()
    expect(screen.getByText('51 rows / 1280 ms / Page 1 / 2')).toBeInTheDocument()
    expect(screen.getByText('Showing 1–50 of 51')).toBeInTheDocument()
    expect(screen.getByText('user-1@example.com')).toBeInTheDocument()
    expect(screen.queryByText('user-51@example.com')).not.toBeInTheDocument()

    const emailHeader = screen.getByText('email').closest('th')
    expect(emailHeader?.querySelector('svg')).toBeInTheDocument()
    expect(screen.getByText('email').closest('span')?.className).toContain('text-[#b9381f]')
    expect(screen.getByText('Sensitive column')).toBeInTheDocument()

    const idHeader = screen.getByText('id').closest('th')
    expect(idHeader?.querySelector('svg')).not.toBeInTheDocument()

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
        permissions: ['sql_editor.read', 'sql_editor.query'],
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
    mockedListQueryConnections.mockResolvedValueOnce({
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

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Shared MySQL'))

    await waitFor(() => {
      expect(mockedListMetadata).toHaveBeenCalledWith(2)
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
        permissions: ['sql_editor.read', 'sql_editor.query'],
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
    mockedListQueryConnections.mockResolvedValueOnce({
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

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Configured MySQL'))

    await waitFor(() => {
      expect(mockedListMetadata).toHaveBeenCalledWith(5)
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
        permissions: ['sql_editor.read', 'sql_editor.query'],
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
    mockedListQueryConnections.mockResolvedValueOnce({
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

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Shared Postgres'))

    await waitFor(() => {
      expect(mockedListMetadata).toHaveBeenCalledWith(4)
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
        permissions: ['sql_editor.read', 'sql_editor.query'],
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
    mockedListQueryConnections.mockResolvedValueOnce({
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

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Search MySQL'))

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
        permissions: ['sql_editor.read', 'sql_editor.query'],
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
    mockedListQueryConnections.mockResolvedValueOnce({
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

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
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
      expect(mockedListMetadataColumns).toHaveBeenCalled()
      expect(mockedListMetadataDefinition).toHaveBeenCalled()
    })
    expect(mockedListMetadata.mock.calls.length).toBeGreaterThan(metadataCallCount)
    expect(screen.queryByText('Searching assets...')).not.toBeInTheDocument()
  })

  it('切換 tab 時會保留各自的 database 選擇上下文', async () => {
    mockedListMetadata.mockReset()
    mockedExecuteQuery.mockResolvedValue({
      columns: ['id'],
      raw_columns: ['id'],
      sensitive_column_indexes: [],
      rows: [['1']],
      row_count: 1,
      duration_ms: 12,
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

      return {
        db_type: 'mysql',
        level: 'table',
        database: params.database,
        items: [],
      }
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(await screen.findByText('analytics'))
    fireEvent.click(screen.getByText('Run'))
    await waitFor(() => {
      expect(mockedExecuteQuery).toHaveBeenLastCalledWith(expect.objectContaining({
        db_connection_id: 1,
        sql: 'SELECT 1;',
        database: 'analytics',
        schema: undefined,
        redis_db_index: undefined,
        query_execution_id: expect.any(String),
      }), expect.any(AbortSignal))
    })

    fireEvent.click(screen.getByText('New Tab'))
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(await screen.findByText('maestro'))
    fireEvent.click(screen.getByText('Run'))
    await waitFor(() => {
      expect(mockedExecuteQuery).toHaveBeenLastCalledWith(expect.objectContaining({
        db_connection_id: 1,
        sql: 'SELECT 1;',
        database: 'maestro',
        schema: undefined,
        redis_db_index: undefined,
        query_execution_id: expect.any(String),
      }), expect.any(AbortSignal))
    })

    fireEvent.click(screen.getByText('Query 1'))
    fireEvent.click(screen.getByText('Run'))
    await waitFor(() => {
      expect(mockedExecuteQuery).toHaveBeenLastCalledWith(expect.objectContaining({
        db_connection_id: 1,
        sql: 'SELECT 1;',
        database: 'analytics',
        schema: undefined,
        redis_db_index: undefined,
        query_execution_id: expect.any(String),
      }), expect.any(AbortSignal))
    })
  })

  it('切換 tab 後回到原分頁，資產樹展開狀態仍會保留', async () => {
    mockedListMetadata.mockReset()
    mockedListMetadata.mockImplementation(async (_connectionId, params) => {
      if (!params?.database) {
        return {
          db_type: 'mysql',
          level: 'database',
          items: [
            { kind: 'database', name: 'dev_edgex_ops_intelligence' },
          ],
        }
      }

      return {
        db_type: 'mysql',
        level: 'table',
        database: 'dev_edgex_ops_intelligence',
        items: [
          {
            kind: 'table',
            database: 'dev_edgex_ops_intelligence',
            schema: 'dev_edgex_ops_intelligence',
            name: 't_activity_delivery_attempt',
          },
          {
            kind: 'table',
            database: 'dev_edgex_ops_intelligence',
            schema: 'dev_edgex_ops_intelligence',
            name: 't_activity_delivery_outbox',
          },
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

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))
    fireEvent.click(await screen.findByText('dev_edgex_ops_intelligence'))
    fireEvent.click(screen.getByText('dev_edgex_ops_intelligence'))
    fireEvent.click(await screen.findByText('t_activity_delivery_attempt'))

    expect(screen.getByText('t_activity_delivery_outbox')).toBeInTheDocument()

    fireEvent.click(screen.getByText('New Tab'))
    fireEvent.click(screen.getByText('Query 1'))

    expect(screen.getByText('t_activity_delivery_outbox')).toBeInTheDocument()
  })

  it('metadata error 只屬於當前 tab，不會污染新分頁', async () => {
    mockedListMetadata.mockReset()
    mockedListMetadata.mockRejectedValueOnce(new ApiError(
      500,
      'query metadata failed: pg_hba.conf rejects connection for host "10.183.27.22"',
      { error: 'query metadata failed: pg_hba.conf rejects connection for host "10.183.27.22"' },
    ))

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('button', { name: 'Run' })).toBeInTheDocument()

    fireEvent.click(screen.getByText('New Tab'))
    fireEvent.click(screen.getByRole('button', { name: 'Asset Selector' }))
    fireEvent.click(screen.getByText('Primary MySQL'))

    expect(await screen.findByText('Metadata is temporarily unavailable. Please try again later.')).toBeInTheDocument()

    fireEvent.click(screen.getByText('New Tab'))

    expect(screen.queryByText('Metadata is temporarily unavailable. Please try again later.')).not.toBeInTheDocument()

    fireEvent.click(screen.getByText('Query 2'))

    expect(screen.getByText('Metadata is temporarily unavailable. Please try again later.')).toBeInTheDocument()
  })
})
