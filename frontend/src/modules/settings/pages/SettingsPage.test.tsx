import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SettingsPage } from '@/modules/settings/pages/SettingsPage'
import { ToastProvider } from '@/shared/ui/ToastContext'

vi.mock('@/modules/settings/api', () => ({
  getSettings: vi.fn(),
  listSettingsDBConnections: vi.fn(),
  patchSettings: vi.fn(),
}))
vi.mock('@/modules/users/api', () => ({
  listUsers: vi.fn(),
}))
vi.mock('@/modules/auth-groups/api', () => ({
  listAuthGroups: vi.fn(),
}))

import { getSettings, listSettingsDBConnections } from '@/modules/settings/api'
import { listAuthGroups } from '@/modules/auth-groups/api'
import { listUsers } from '@/modules/users/api'

const mockedGetSettings = vi.mocked(getSettings)
const mockedListSettingsDBConnections = vi.mocked(listSettingsDBConnections)
const mockedListUsers = vi.mocked(listUsers)
const mockedListAuthGroups = vi.mocked(listAuthGroups)

describe('SettingsPage', () => {
  beforeEach(() => {
    mockedGetSettings.mockReset()
    mockedListSettingsDBConnections.mockReset()
    mockedListUsers.mockReset()
    mockedListAuthGroups.mockReset()
  })

  it('loads the settings page without depending on the users API', async () => {
    mockedGetSettings.mockResolvedValue({
      lark_app_id: '',
      lark_app_secret_configured: false,
      sensitive_export_reviewer_user_ids: [],
      sensitive_query_access_reviewer_user_ids: [],
      require_non_sensitive_export_review: true,
      approval_policies: [
        { workflow_type: 'ddl', reviewer_user_ids: [7], reviewer_auth_groups: ['reviewer'], enabled: true },
        { workflow_type: 'sql_export_sensitive', reviewer_user_ids: [], reviewer_auth_groups: ['reviewer'], enabled: true },
      ],
      sql_editor_app_timeout_seconds: 30,
      sql_editor_mysql_max_execution_time_ms: 25000,
      sql_editor_postgres_statement_timeout_ms: 25000,
      db_metadata_inventory_enabled: true,
      db_metadata_inventory_regions: ['ap-northeast-1'],
      db_metadata_inventory_engines: ['aurora-mysql', 'aurora-postgresql', 'redis'],
      db_metadata_inventory_cron: '0 9 * * *',
      db_metadata_inventory_sync_interval_minutes: 5,
      db_metadata_object_enabled: true,
      db_metadata_object_enabled_connection_ids: [12, 18],
      db_metadata_object_cron: '0 10 * * *',
      db_metadata_object_sync_interval_minutes: 60,
      db_metadata_cron_timezone: 'Asia/Taipei',
    })
    mockedListSettingsDBConnections.mockResolvedValue({
      connections: [
        { id: 12, name: 'analytics-ro', db_type: 'mysql', host: 'db-a.internal', port: 3306 },
        { id: 18, name: 'warehouse-ro', db_type: 'postgres', host: 'pg-a.internal', port: 5432 },
      ],
    })
    mockedListUsers.mockResolvedValue({
      users: [
        { id: 7, username: 'leader', email: 'leader@example.com', lark_recipient: '', auth_groups: ['reviewer'], permissions: ['tickets.review', 'sql_editor.export_review'], db_connection_ids: [], protected: false, is_active: true, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
      ],
    })
    mockedListAuthGroups.mockResolvedValue({
      auth_groups: [
        { name: 'reviewer', label: 'Reviewer', description: '', system_defined: true, user_count: 1 },
      ],
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SettingsPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Platform Settings')).toBeInTheDocument())
    expect(screen.queryByText('Metadata Scope')).not.toBeInTheDocument()
    expect(screen.getByDisplayValue('ap-northeast-1')).toBeInTheDocument()
    expect(screen.getByDisplayValue('0 9 * * *')).toBeInTheDocument()
    expect(screen.getByDisplayValue('Asia/Taipei')).toBeInTheDocument()
    expect(screen.getByText('Approval Routing')).toBeInTheDocument()
    expect(screen.getAllByText('Effective reviewers').length).toBeGreaterThan(0)
    expect(screen.getAllByText('leader').length).toBeGreaterThan(0)
    expect(screen.getByText('analytics-ro')).toBeInTheDocument()
    expect(screen.getByText('warehouse-ro')).toBeInTheDocument()
    expect(screen.getByText('2 selected')).toBeInTheDocument()
    expect(screen.getByRole('switch', { name: 'Require approval for non-sensitive exports' })).toBeChecked()
    expect(screen.getByRole('switch', { name: 'analytics-ro selected for object scan' })).toBeInTheDocument()
    expect(screen.getByRole('switch', { name: 'warehouse-ro selected for object scan' })).toBeInTheDocument()
    expect(screen.queryByText('ID 12')).not.toBeInTheDocument()
    expect(screen.queryByText('db-a.internal:3306')).not.toBeInTheDocument()
  })
})
