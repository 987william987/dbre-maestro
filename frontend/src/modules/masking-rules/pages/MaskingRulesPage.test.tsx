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
  listMaskingConnections: vi.fn(),
  listMaskingMetadata: vi.fn(),
  listMaskingMetadataColumns: vi.fn(),
  createMaskingWhitelist: vi.fn(),
  patchMaskingWhitelist: vi.fn(),
  deleteMaskingWhitelist: vi.fn(),
  listRedisSensitiveKeyPrefixes: vi.fn(),
  createRedisSensitiveKeyPrefix: vi.fn(),
  patchRedisSensitiveKeyPrefix: vi.fn(),
  deleteRedisSensitiveKeyPrefix: vi.fn(),
}))

import {
  createMaskingRule,
  createRedisSensitiveKeyPrefix,
  listMaskingConnections,
  listMaskingMetadata,
  listMaskingMetadataColumns,
  createMaskingWhitelist,
  deleteMaskingRule,
  listRedisSensitiveKeyPrefixes,
  listMaskingRules,
  listMaskingWhitelists,
  patchMaskingRule,
  patchMaskingWhitelist,
  patchRedisSensitiveKeyPrefix,
} from '@/modules/masking-rules/api'

const mockedListMaskingRules = vi.mocked(listMaskingRules)
const mockedCreateMaskingRule = vi.mocked(createMaskingRule)
const mockedPatchMaskingRule = vi.mocked(patchMaskingRule)
const mockedPatchMaskingWhitelist = vi.mocked(patchMaskingWhitelist)
const mockedPatchRedisSensitiveKeyPrefix = vi.mocked(patchRedisSensitiveKeyPrefix)
const mockedDeleteMaskingRule = vi.mocked(deleteMaskingRule)
const mockedListRedisSensitiveKeyPrefixes = vi.mocked(listRedisSensitiveKeyPrefixes)
const mockedCreateRedisSensitiveKeyPrefix = vi.mocked(createRedisSensitiveKeyPrefix)
const mockedListMaskingWhitelists = vi.mocked(listMaskingWhitelists)
const mockedListMaskingConnections = vi.mocked(listMaskingConnections)
const mockedListMaskingMetadata = vi.mocked(listMaskingMetadata)
const mockedListMaskingMetadataColumns = vi.mocked(listMaskingMetadataColumns)
const mockedCreateMaskingWhitelist = vi.mocked(createMaskingWhitelist)

function selectOption(label: string, option: string) {
  fireEvent.click(screen.getByRole('button', { name: label }))
  fireEvent.click(screen.getByRole('option', { name: option }))
}

const rule = {
  id: 2,
  column_name: 'phone',
  match_type: 'exact' as const,
  mask_mode: 'partial' as const,
  mask_config: { keep_prefix: 3, keep_suffix: 4, mask_char: '*' },
  enabled: true,
  created_by: 1,
  created_at: '2026-01-01T00:00:00Z',
}

const whitelist = {
  id: 5,
  db_connection_id: 1,
  database_name: 'analytics',
  table_name: 'tickets',
  column_name: 'email',
  enabled: true,
  created_by: 1,
  created_at: '2026-01-01T00:00:00Z',
}

const redisPrefix = {
  id: 9,
  db_connection_id: 6,
  redis_db_index: null,
  key_prefix: 'session:',
  reason: 'login session',
  is_active: true,
  created_by: 1,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
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
    mockedListRedisSensitiveKeyPrefixes.mockResolvedValue({ prefixes: [redisPrefix] })
    mockedListMaskingConnections.mockResolvedValue({
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
        {
          id: 6,
          name: 'cache-redis',
          db_type: 'redis',
          host: 'redis.internal',
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
    mockedListMaskingMetadata.mockResolvedValue({
      db_type: 'mysql',
      level: 'database',
      items: [{ kind: 'database', name: 'analytics' }],
    })
    mockedListMaskingMetadataColumns.mockResolvedValue({
      database: 'analytics',
      schema: 'analytics',
      table: 'tickets',
      columns: [{ name: 'email', data_type: 'varchar', column_type: 'varchar(255)', is_nullable: 'YES', comment: '' }],
    })
  })

  it('creates a Redis sensitive key prefix', async () => {
    mockedListRedisSensitiveKeyPrefixes
      .mockResolvedValueOnce({ prefixes: [] })
      .mockResolvedValueOnce({ prefixes: [redisPrefix] })
    mockedCreateRedisSensitiveKeyPrefix.mockResolvedValue(redisPrefix)

    renderPage()

    await waitFor(() => expect(screen.getByText('Redis Sensitive Key Prefixes')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: 'New Prefix' }))
    selectOption('Redis Connection', 'cache-redis')
    fireEvent.change(screen.getByLabelText('Key Prefix'), { target: { value: 'session:' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create Prefix' }))

    await waitFor(() => {
      expect(mockedCreateRedisSensitiveKeyPrefix).toHaveBeenCalledWith({
        db_connection_id: 6,
        redis_db_index: null,
        key_prefix: 'session:',
        reason: null,
        is_active: true,
      })
    })
  })

  it('creates a global masking rule', async () => {
    mockedListMaskingRules
      .mockResolvedValueOnce({ rules: [] })
      .mockResolvedValueOnce({ rules: [rule] })
    mockedCreateMaskingRule.mockResolvedValue(rule)

    renderPage()

    await waitFor(() => expect(screen.getByRole('button', { name: 'New Rule' })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: 'New Rule' }))
    fireEvent.change(screen.getByLabelText('Column Pattern'), { target: { value: 'email' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create Rule' }))

    await waitFor(() => {
      expect(mockedCreateMaskingRule).toHaveBeenCalledWith({
        column_name: 'email',
        match_type: 'exact',
        mask_mode: 'full',
        mask_config: {},
      })
    })
  })

  it('toggles a global masking rule enabled state', async () => {
    mockedPatchMaskingRule.mockResolvedValue({ ...rule, enabled: false })

    renderPage()

    const toggle = await screen.findByRole('switch', { name: 'phone enabled' })
    fireEvent.click(toggle)

    await waitFor(() => {
      expect(mockedPatchMaskingRule).toHaveBeenCalledWith(2, { enabled: false })
    })
    expect(await screen.findByText('Disabled')).toBeInTheDocument()
  })

  it('toggles a whitelist entry enabled state from the list', async () => {
    mockedPatchMaskingWhitelist.mockResolvedValue({ ...whitelist, enabled: false })

    renderPage()

    const toggle = await screen.findByRole('switch', { name: 'analytics.tickets.email enabled' })
    fireEvent.click(toggle)

    await waitFor(() => {
      expect(mockedPatchMaskingWhitelist).toHaveBeenCalledWith(5, { enabled: false })
    })
    expect(await screen.findByText('Disabled')).toBeInTheDocument()
  })

  it('toggles a Redis sensitive key prefix enabled state from the list', async () => {
    mockedPatchRedisSensitiveKeyPrefix.mockResolvedValue({ ...redisPrefix, is_active: false })

    renderPage()

    const toggle = await screen.findByRole('switch', { name: 'session: enabled' })
    fireEvent.click(toggle)

    await waitFor(() => {
      expect(mockedPatchRedisSensitiveKeyPrefix).toHaveBeenCalledWith(9, {
        db_connection_id: 6,
        redis_db_index: null,
        key_prefix: 'session:',
        reason: 'login session',
        is_active: false,
      })
    })
    expect(await screen.findByText('Disabled')).toBeInTheDocument()
  })

  it('creates a whitelist entry', async () => {
    mockedListMaskingWhitelists
      .mockResolvedValueOnce({ whitelist: [] })
      .mockResolvedValueOnce({ whitelist: [whitelist] })
    mockedListMaskingMetadata
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
    fireEvent.click(screen.getByRole('button', { name: 'New Whitelist' }))
    selectOption('Connection', 'analytics-db')
    await waitFor(() => expect(mockedListMaskingMetadata).toHaveBeenCalledWith(1))
    selectOption('Database', 'analytics')
    await waitFor(() => expect(mockedListMaskingMetadata).toHaveBeenCalledWith(1, { database: 'analytics' }))
    selectOption('Table', 'tickets')
    await waitFor(() => expect(mockedListMaskingMetadataColumns).toHaveBeenCalledWith(1, 'analytics', 'tickets', 'analytics'))
    selectOption('Column', 'email')
    fireEvent.click(screen.getByRole('button', { name: 'Create Whitelist' }))

    await waitFor(() => {
      expect(mockedCreateMaskingWhitelist).toHaveBeenCalledWith({
        db_connection_id: 1,
        database_name: 'analytics',
        schema_name: '',
        table_name: 'tickets',
        column_name: 'email',
      })
    })
  })

  it('creates a PostgreSQL whitelist entry with schema scope', async () => {
    const pgWhitelist = {
      ...whitelist,
      db_connection_id: 7,
      database_name: 'app',
      schema_name: 'public',
      table_name: 'users',
    }
    mockedListMaskingConnections.mockResolvedValueOnce({
      connections: [
        {
          id: 7,
          name: 'analytics-pg',
          db_type: 'postgres',
          host: 'pg.internal',
          port: 5432,
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
    mockedListMaskingWhitelists
      .mockResolvedValueOnce({ whitelist: [] })
      .mockResolvedValueOnce({ whitelist: [pgWhitelist] })
    mockedListMaskingMetadata
      .mockResolvedValueOnce({
        db_type: 'postgres',
        level: 'database',
        items: [{ kind: 'database', name: 'app' }],
      })
      .mockResolvedValueOnce({
        db_type: 'postgres',
        level: 'schema',
        database: 'app',
        items: [{ kind: 'schema', name: 'public', database: 'app', schema: 'public' }],
      })
      .mockResolvedValueOnce({
        db_type: 'postgres',
        level: 'table',
        database: 'app',
        schema: 'public',
        items: [{ kind: 'table', name: 'users', database: 'app', schema: 'public' }],
      })
    mockedListMaskingMetadataColumns.mockResolvedValueOnce({
      database: 'app',
      schema: 'public',
      table: 'users',
      columns: [{ name: 'email', data_type: 'text', column_type: 'text', is_nullable: 'YES', comment: '' }],
    })
    mockedCreateMaskingWhitelist.mockResolvedValue(pgWhitelist)

    renderPage()

    await waitFor(() => expect(screen.getByText('Unmask Whitelist')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: 'New Whitelist' }))
    selectOption('Connection', 'analytics-pg')
    await waitFor(() => expect(mockedListMaskingMetadata).toHaveBeenCalledWith(7))
    selectOption('Database', 'app')
    await waitFor(() => expect(mockedListMaskingMetadata).toHaveBeenCalledWith(7, { database: 'app' }))
    selectOption('Schema', 'public')
    await waitFor(() => expect(mockedListMaskingMetadata).toHaveBeenCalledWith(7, { database: 'app', schema: 'public' }))
    selectOption('Table', 'users')
    await waitFor(() => expect(mockedListMaskingMetadataColumns).toHaveBeenCalledWith(7, 'public', 'users', 'app'))
    selectOption('Column', 'email')
    fireEvent.click(screen.getByRole('button', { name: 'Create Whitelist' }))

    await waitFor(() => {
      expect(mockedCreateMaskingWhitelist).toHaveBeenCalledWith({
        db_connection_id: 7,
        database_name: 'app',
        schema_name: 'public',
        table_name: 'users',
        column_name: 'email',
      })
    })
  })

  it('calls delete rule API after confirmation', async () => {
    mockedListMaskingRules
      .mockResolvedValueOnce({ rules: [rule] })
      .mockResolvedValueOnce({ rules: [] })
    mockedDeleteMaskingRule.mockResolvedValue(undefined)

    renderPage()

    await waitFor(() => expect(screen.getByText('phone')).toBeInTheDocument())
    fireEvent.click(screen.getAllByRole('button', { name: 'Delete' })[0])
    fireEvent.click(screen.getByRole('button', { name: 'Confirm Delete' }))

    await waitFor(() => {
      expect(mockedDeleteMaskingRule).toHaveBeenCalledWith(2)
    })
  })

  it('paginates the global masking rule list', async () => {
    mockedListMaskingRules.mockResolvedValue({
      rules: Array.from({ length: 21 }, (_, index) => ({
        id: index + 1,
        column_name: `column_${String(index + 1).padStart(2, '0')}`,
        match_type: 'exact' as const,
        mask_mode: 'partial' as const,
        mask_config: {},
        enabled: true,
        created_by: 1,
        created_at: '2026-01-01T00:00:00Z',
      })),
    })

    renderPage()

    expect(await screen.findByText('column_01')).toBeInTheDocument()
    expect(screen.queryByText('column_21')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Next' }))

    await waitFor(() => expect(screen.getByText('column_21')).toBeInTheDocument())
    expect(screen.queryByText('column_01')).not.toBeInTheDocument()
  })
})
