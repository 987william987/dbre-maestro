import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ToastProvider } from '@/shared/ui/ToastContext'
import { SQLEditorPage } from '@/modules/sql-editor/pages/SQLEditorPage'

vi.mock('@uiw/react-codemirror', () => ({
  default: ({ value, onChange }: { value: string; onChange: (value: string) => void }) => (
    <textarea aria-label="CodeMirror" value={value} onChange={(event) => onChange(event.target.value)} />
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
}))

vi.mock('@/modules/exports/api', () => ({
  createExportRequest: vi.fn(),
}))

import { listDBConnections } from '@/modules/db-connections/api'
import { createExportRequest } from '@/modules/exports/api'
import { executeQuery, listMetadata, listMetadataColumns } from '@/modules/sql-editor/api'
import { useAuth } from '@/shared/auth/AuthContext'

const mockedListDBConnections = vi.mocked(listDBConnections)
const mockedExecuteQuery = vi.mocked(executeQuery)
const mockedListMetadata = vi.mocked(listMetadata)
const mockedListMetadataColumns = vi.mocked(listMetadataColumns)
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
  })

  it('會以 user.id 為 namespace 保存 editor 狀態', async () => {
    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL / Redis 工作台')).toBeInTheDocument()

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
      rows: [[1, 'Test ticket']],
      row_count: 1,
      duration_ms: 18,
    })

    mockedCreateExportRequest.mockResolvedValue({
      id: 10,
      status: 'ready',
      sensitive: false,
      download_url: '/api/exports/download/abc',
      expires_at: '2026-06-10T00:00:00Z',
    })

    const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLEditorPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('SQL / Redis 工作台')).toBeInTheDocument()

    fireEvent.click(screen.getByText('Run Query'))

    expect(await screen.findByText('Test ticket')).toBeInTheDocument()
    expect(mockedExecuteQuery).toHaveBeenCalledWith({
      db_connection_id: 1,
      sql: 'SELECT 1;',
      database: undefined,
      schema: undefined,
      redis_db_index: undefined,
    })

    fireEvent.click(screen.getByText('Export Result'))

    await waitFor(() => {
      expect(mockedCreateExportRequest).toHaveBeenCalledWith({
        db_connection_id: 1,
        sql_content: 'SELECT 1;',
      })
      expect(openSpy).toHaveBeenCalledWith('/api/exports/download/abc', '_blank', 'noopener,noreferrer')
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
    mockedExecuteQuery.mockResolvedValue({
      columns: ['id'],
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

    expect(await screen.findByText('SQL / Redis 工作台')).toBeInTheDocument()
    fireEvent.click(screen.getByText('Run Query'))

    expect(await screen.findByText(/Sensitive override active/i)).toBeInTheDocument()
  })

  it('查詢結果 rows 形狀異常時不應讓頁面崩潰', async () => {
    mockedExecuteQuery.mockResolvedValue({
      columns: ['id'],
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

    expect(await screen.findByText('SQL / Redis 工作台')).toBeInTheDocument()
    fireEvent.click(screen.getByText('Run Query'))

    expect(await screen.findByText('1 rows / 12 ms')).toBeInTheDocument()
  })

  it('MySQL 未指定 database 時會先顯示 database 清單', async () => {
    mockedListMetadata.mockReset()
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

    await waitFor(() => {
      expect(mockedListMetadata).toHaveBeenCalled()
      expect(screen.getAllByText('Shared MySQL').length).toBeGreaterThan(0)
      expect(screen.getByText('analytics')).toBeInTheDocument()
    })
  })

  it('MySQL 即使設定 landing database，也會先顯示 database 清單', async () => {
    mockedListMetadata.mockReset()
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

    await waitFor(() => {
      expect(mockedListMetadata).toHaveBeenCalledWith(5)
      expect(screen.getAllByText('Configured MySQL').length).toBeGreaterThan(0)
      expect(screen.getByText('analytics')).toBeInTheDocument()
      expect(screen.getAllByText('maestro').length).toBeGreaterThan(0)
    })
  })

  it('Postgres 即使設定 landing database，也會先顯示 database 清單', async () => {
    mockedListMetadata.mockReset()
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

    await waitFor(() => {
      expect(mockedListMetadata).toHaveBeenCalledWith(4)
      expect(screen.getAllByText('Shared Postgres').length).toBeGreaterThan(0)
      expect(screen.getByText('analytics')).toBeInTheDocument()
      expect(screen.getAllByText('postgres').length).toBeGreaterThan(0)
    })
  })

  it('Explorer Search 會過濾資產節點', async () => {
    mockedListMetadata.mockReset()
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

    await waitFor(() => {
      expect(screen.getByText('analytics')).toBeInTheDocument()
    })

    fireEvent.change(screen.getByLabelText('Explorer Search'), {
      target: { value: 'maes' },
    })

    await waitFor(() => {
      expect(screen.getByText('maestro')).toBeInTheDocument()
      expect(screen.queryByText('analytics')).not.toBeInTheDocument()
    })
  })
})
