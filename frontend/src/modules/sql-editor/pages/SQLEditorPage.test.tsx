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
  useAuth: () => ({
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
  }),
}))

vi.mock('@/modules/db-connections/api', () => ({
  listDBConnections: vi.fn(),
}))

vi.mock('@/modules/sql-editor/api', () => ({
  executeQuery: vi.fn(),
  listMetadataTables: vi.fn(),
  listMetadataColumns: vi.fn(),
}))

vi.mock('@/modules/exports/api', () => ({
  createExportRequest: vi.fn(),
}))

import { listDBConnections } from '@/modules/db-connections/api'
import { createExportRequest } from '@/modules/exports/api'
import { executeQuery, listMetadataColumns, listMetadataTables } from '@/modules/sql-editor/api'

const mockedListDBConnections = vi.mocked(listDBConnections)
const mockedExecuteQuery = vi.mocked(executeQuery)
const mockedListMetadataTables = vi.mocked(listMetadataTables)
const mockedListMetadataColumns = vi.mocked(listMetadataColumns)
const mockedCreateExportRequest = vi.mocked(createExportRequest)
const storage = new Map<string, string>()

describe('SQLEditorPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    storage.clear()

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

    mockedListMetadataTables.mockResolvedValue({
      tables: [
        {
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
      download_url: '/exports/download/abc',
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
    })

    fireEvent.click(screen.getByText('Export Result'))

    await waitFor(() => {
      expect(mockedCreateExportRequest).toHaveBeenCalledWith({
        db_connection_id: 1,
        sql_content: 'SELECT 1;',
      })
      expect(openSpy).toHaveBeenCalledWith('/exports/download/abc', '_blank', 'noopener,noreferrer')
    })
  })
})
