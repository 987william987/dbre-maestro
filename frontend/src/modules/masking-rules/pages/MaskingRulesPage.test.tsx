import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ToastProvider } from '@/shared/ui/ToastContext'
import { MaskingRulesPage } from '@/modules/masking-rules/pages/MaskingRulesPage'

vi.mock('@/shared/auth/AuthContext', () => ({
  useAuth: () => ({
    user: {
      id: 1,
      username: 'admin',
      authGroups: ['admin'],
      authGroupDetails: [],
      permissions: ['masking_rules.write'],
      dbConnectionIds: [],
      protected: false,
      isActive: true,
    },
  }),
}))

vi.mock('@/modules/masking-rules/api', () => ({
  listMaskingRules: vi.fn(),
  createMaskingRule: vi.fn(),
  patchMaskingRule: vi.fn(),
  deleteMaskingRule: vi.fn(),
  listMaskingWhitelists: vi.fn(),
  createMaskingWhitelist: vi.fn(),
  patchMaskingWhitelist: vi.fn(),
  deleteMaskingWhitelist: vi.fn(),
}))

vi.mock('@/modules/db-connections/api', () => ({
  listDBConnections: vi.fn(),
}))

vi.mock('@/modules/sql-editor/api', () => ({
  listMetadata: vi.fn(),
  listMetadataColumns: vi.fn(),
}))

import {
  createMaskingRule,
  createMaskingWhitelist,
  deleteMaskingRule,
  listMaskingRules,
  listMaskingWhitelists,
} from '@/modules/masking-rules/api'
import { listDBConnections } from '@/modules/db-connections/api'
import { listMetadata, listMetadataColumns } from '@/modules/sql-editor/api'

const mockedListMaskingRules = vi.mocked(listMaskingRules)
const mockedCreateMaskingRule = vi.mocked(createMaskingRule)
const mockedDeleteMaskingRule = vi.mocked(deleteMaskingRule)
const mockedListMaskingWhitelists = vi.mocked(listMaskingWhitelists)
const mockedCreateMaskingWhitelist = vi.mocked(createMaskingWhitelist)
const mockedListDBConnections = vi.mocked(listDBConnections)
const mockedListMetadata = vi.mocked(listMetadata)
const mockedListMetadataColumns = vi.mocked(listMetadataColumns)

const rule = {
  id: 2,
  column_name: 'phone',
  mask_mode: 'partial' as const,
  created_by: 1,
  created_at: '2026-01-01T00:00:00Z',
}

const whitelist = {
  id: 5,
  db_connection_id: 1,
  database_name: 'analytics',
  table_name: 'tickets',
  column_name: 'email',
  created_by: 1,
  created_at: '2026-01-01T00:00:00Z',
}

function renderPage() {
  return render(
    <MemoryRouter>
      <ToastProvider>
        <MaskingRulesPage />
      </ToastProvider>
    </MemoryRouter>,
  )
}

describe('MaskingRulesPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    mockedListMaskingRules.mockResolvedValue({ rules: [rule] })
    mockedListMaskingWhitelists.mockResolvedValue({ whitelist: [whitelist] })
    mockedListDBConnections.mockResolvedValue({
      connections: [
        {
          id: 1,
          name: 'analytics-db',
          db_type: 'mysql',
          host: 'db.internal',
          port: 3306,
          database_name: null,
          username: 'readonly',
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
      items: [{ kind: 'database', name: 'analytics' }],
    })
    mockedListMetadataColumns.mockResolvedValue({
      database: 'analytics',
      schema: 'analytics',
      table: 'tickets',
      columns: [{ name: 'email', data_type: 'varchar', column_type: 'varchar(255)', is_nullable: 'YES', comment: '' }],
    })
  })

  it('可建立 global masking rule', async () => {
    mockedListMaskingRules
      .mockResolvedValueOnce({ rules: [] })
      .mockResolvedValueOnce({ rules: [rule] })
    mockedCreateMaskingRule.mockResolvedValue(rule)

    renderPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Masking Rules' })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '新增規則' }))
    fireEvent.change(screen.getByLabelText('Column Name'), { target: { value: 'email' } })
    fireEvent.click(screen.getByRole('button', { name: '建立規則' }))

    await waitFor(() => {
      expect(mockedCreateMaskingRule).toHaveBeenCalledWith({
        column_name: 'email',
        mask_mode: 'full',
      })
    })
  })

  it('可建立 whitelist', async () => {
    mockedListMaskingWhitelists
      .mockResolvedValueOnce({ whitelist: [] })
      .mockResolvedValueOnce({ whitelist: [whitelist] })
    mockedListMetadata
      .mockResolvedValueOnce({
        db_type: 'mysql',
        level: 'database',
        items: [{ kind: 'database', name: 'analytics' }],
      })
      .mockResolvedValueOnce({
        db_type: 'mysql',
        level: 'table',
        database: 'analytics',
        items: [{ kind: 'table', name: 'tickets', database: 'analytics', schema: 'analytics' }],
      })
    mockedCreateMaskingWhitelist.mockResolvedValue(whitelist)

    renderPage()

    await waitFor(() => expect(screen.getByText('Unmask Whitelist')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '新增白名單' }))
    fireEvent.change(screen.getByLabelText('Connection'), { target: { value: '1' } })
    await waitFor(() => expect(mockedListMetadata).toHaveBeenCalledWith(1))
    fireEvent.change(screen.getByLabelText('Database'), { target: { value: 'analytics' } })
    await waitFor(() => expect(mockedListMetadata).toHaveBeenCalledWith(1, { database: 'analytics' }))
    fireEvent.change(screen.getByLabelText('Table'), { target: { value: 'tickets' } })
    await waitFor(() => expect(mockedListMetadataColumns).toHaveBeenCalledWith(1, 'analytics', 'tickets', 'analytics'))
    fireEvent.change(screen.getByLabelText('Column'), { target: { value: 'email' } })
    fireEvent.click(screen.getByRole('button', { name: '建立白名單' }))

    await waitFor(() => {
      expect(mockedCreateMaskingWhitelist).toHaveBeenCalledWith({
        db_connection_id: 1,
        database_name: 'analytics',
        table_name: 'tickets',
        column_name: 'email',
      })
    })
  })

  it('確認刪除後會呼叫 delete rule API', async () => {
    mockedListMaskingRules
      .mockResolvedValueOnce({ rules: [rule] })
      .mockResolvedValueOnce({ rules: [] })
    mockedDeleteMaskingRule.mockResolvedValue(undefined)

    renderPage()

    await waitFor(() => expect(screen.getByText('phone')).toBeInTheDocument())
    fireEvent.click(screen.getAllByRole('button', { name: 'Delete' })[0])
    fireEvent.click(screen.getByRole('button', { name: '確認刪除' }))

    await waitFor(() => {
      expect(mockedDeleteMaskingRule).toHaveBeenCalledWith(2)
    })
  })
})
