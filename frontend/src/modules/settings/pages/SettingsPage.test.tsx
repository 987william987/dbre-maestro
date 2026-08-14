import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SettingsPage } from '@/modules/settings/pages/SettingsPage'
import { ToastProvider } from '@/shared/ui/ToastContext'
import type { PlatformSettings } from '@/shared/types/settings'

vi.mock('@/modules/settings/api', () => ({
  getSettings: vi.fn(),
  listSettingsDBConnections: vi.fn(),
  patchSettings: vi.fn(),
  previewWorkflowRules: vi.fn(),
}))
vi.mock('@/modules/users/api', () => ({
  listUsers: vi.fn(),
}))
vi.mock('@/modules/auth-groups/api', () => ({
  listAuthGroups: vi.fn(),
}))
vi.mock('@/shared/auth/AuthContext', () => ({
  useAuth: () => ({
    user: { permissions: ['settings.read', 'settings.write'] },
  }),
}))

import { getSettings, listSettingsDBConnections, previewWorkflowRules } from '@/modules/settings/api'
import { listAuthGroups } from '@/modules/auth-groups/api'
import { listUsers } from '@/modules/users/api'

const mockedGetSettings = vi.mocked(getSettings)
const mockedListSettingsDBConnections = vi.mocked(listSettingsDBConnections)
const mockedPreviewWorkflowRules = vi.mocked(previewWorkflowRules)
const mockedListUsers = vi.mocked(listUsers)
const mockedListAuthGroups = vi.mocked(listAuthGroups)

function makeSettings(overrides: Partial<PlatformSettings> = {}): PlatformSettings {
  return {
    app_env: '',
    lark_app_id: '',
    lark_app_secret_configured: false,
    lark_interactive_cards_enabled: false,
    lark_card_callback_mode: 'http',
    lark_card_verification_token_configured: false,
    lark_oauth_enabled: false,
    lark_oauth_site: 'lark',
    lark_oauth_redirect_url: '',
    sso_oidc_enabled: false,
    sso_oidc_display_name: 'Authentik',
    sso_oidc_issuer_url: '',
    sso_oidc_client_id: '',
    sso_oidc_client_secret: '',
    sso_oidc_client_secret_configured: false,
    sso_oidc_redirect_url: '',
    sso_oidc_scopes: ['openid', 'profile', 'email', 'dbre'],
    sso_oidc_trust_mfa: false,
    sensitive_export_reviewer_user_ids: [],
    sensitive_query_access_reviewer_user_ids: [],
    require_non_sensitive_export_review: true,
    approval_policies: [],
    workflow_rules: [
      {
        id: 1,
        rule_name: 'Global DDL',
        ticket_type: 'ddl',
        db_connection_id: null,
        export_sensitivity: null,
        approval_enabled: true,
        execution_mode: 'manual',
        approval_auth_groups: ['data_owner'],
        executor_auth_groups: ['dba'],
        priority: 100,
        enabled: true,
      },
    ],
    sql_editor_app_timeout_seconds: 30,
    sql_editor_mysql_max_execution_time_ms: 25000,
    sql_editor_postgres_statement_timeout_ms: 25000,
    sql_export_app_timeout_seconds: 120,
    sql_export_mysql_max_execution_time_ms: 90000,
    sql_export_postgres_statement_timeout_ms: 90000,
    mysql_rollback_enabled: false,
    mysql_rollback_engine: 'hybrid',
    mysql_rollback_my2sql_path: 'my2sql',
    mysql_rollback_generation_timeout_seconds: 30,
    mysql_rollback_max_sql_bytes: 5 * 1024 * 1024,
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
    ...overrides,
  }
}

function mockSettingsDependencies() {
  mockedListSettingsDBConnections.mockResolvedValue({
    connections: [
      { id: 12, name: 'analytics-ro', db_type: 'mysql', host: 'db-a.internal', port: 3306 },
      { id: 18, name: 'warehouse-ro', db_type: 'postgres', host: 'pg-a.internal', port: 5432 },
      { id: 19, name: 'cache-redis', db_type: 'redis', host: 'redis.internal', port: 6379 },
    ],
  })
  mockedListUsers.mockResolvedValue({ users: [] })
  mockedListAuthGroups.mockResolvedValue({
    auth_groups: [
      { name: 'data_owner', label: 'Data Owner', description: '', system_defined: true, user_count: 1 },
      { name: 'dba', label: 'DBA', description: '', system_defined: true, user_count: 1 },
    ],
  })
}

function renderSettingsPage() {
  render(
    <MemoryRouter>
      <ToastProvider>
        <SettingsPage />
      </ToastProvider>
    </MemoryRouter>,
  )
}

describe('SettingsPage', () => {
  beforeEach(() => {
    mockedGetSettings.mockReset()
    mockedListSettingsDBConnections.mockReset()
    mockedPreviewWorkflowRules.mockReset()
    mockedListUsers.mockReset()
    mockedListAuthGroups.mockReset()
    mockedPreviewWorkflowRules.mockResolvedValue({ previews: [] })
  })

  it('loads the settings page without depending on the users API', async () => {
    mockedGetSettings.mockResolvedValue({
      ...makeSettings(),
      lark_app_id: '',
      lark_app_secret_configured: false,
      lark_interactive_cards_enabled: false,
      lark_card_callback_mode: 'http',
      lark_card_verification_token_configured: false,
      lark_oauth_enabled: false,
      lark_oauth_site: 'lark',
      lark_oauth_redirect_url: '',
      sensitive_export_reviewer_user_ids: [],
      sensitive_query_access_reviewer_user_ids: [],
      require_non_sensitive_export_review: true,
      approval_policies: [
        { workflow_type: 'ddl', reviewer_user_ids: [7], reviewer_auth_groups: ['reviewer'], enabled: true },
        { workflow_type: 'sql_export_sensitive', reviewer_user_ids: [], reviewer_auth_groups: ['reviewer'], enabled: true },
      ],
      workflow_rules: [
        {
          id: 1,
          rule_name: 'Global DDL',
          ticket_type: 'ddl',
          db_connection_id: null,
          export_sensitivity: null,
          approval_enabled: true,
          execution_mode: 'manual',
          approval_auth_groups: ['data_owner'],
          executor_auth_groups: ['dba'],
          priority: 100,
          enabled: true,
        },
      ],
      sql_editor_app_timeout_seconds: 30,
      sql_editor_mysql_max_execution_time_ms: 25000,
      sql_editor_postgres_statement_timeout_ms: 25000,
      sql_export_app_timeout_seconds: 120,
      sql_export_mysql_max_execution_time_ms: 90000,
      sql_export_postgres_statement_timeout_ms: 90000,
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
        { name: 'data_owner', label: 'Data Owner', description: '', system_defined: true, user_count: 1 },
        { name: 'dba', label: 'DBA', description: '', system_defined: true, user_count: 1 },
      ],
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SettingsPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Workflow Rules')).toBeInTheDocument())
    expect(screen.queryByText('Metadata Scope')).not.toBeInTheDocument()
    expect(screen.getByDisplayValue('ap-northeast-1')).toBeInTheDocument()
    expect(screen.getByDisplayValue('0 9 * * *')).toBeInTheDocument()
    expect(screen.getByDisplayValue('Asia/Taipei')).toBeInTheDocument()
    expect(screen.getByDisplayValue('Global DDL')).toBeInTheDocument()
    expect(screen.getByText('SQL Export Timeout')).toBeInTheDocument()
    expect(screen.getByDisplayValue('120')).toBeInTheDocument()
    expect(screen.getAllByDisplayValue('90000')).toHaveLength(2)
    expect(screen.getAllByText('Data Owner').length).toBeGreaterThan(0)
    expect(screen.getAllByText('DBA').length).toBeGreaterThan(0)
    expect(screen.getAllByText('analytics-ro').length).toBeGreaterThan(0)
    expect(screen.getAllByText('warehouse-ro').length).toBeGreaterThan(0)
    expect(screen.getByText('2 selected')).toBeInTheDocument()
    expect(screen.queryByText('Export Approval')).not.toBeInTheDocument()
    expect(screen.getByRole('switch', { name: 'analytics-ro selected for object scan' })).toBeInTheDocument()
    expect(screen.getByRole('switch', { name: 'warehouse-ro selected for object scan' })).toBeInTheDocument()
    expect(screen.queryByText('ID 12')).not.toBeInTheDocument()
    expect(screen.queryByText('db-a.internal:3306')).not.toBeInTheDocument()
    expect(mockedListUsers).not.toHaveBeenCalled()
  })

  it('locks production workflow execution switches to approval required and manual execution', async () => {
    mockedGetSettings.mockResolvedValue(makeSettings({
      app_env: 'production',
      workflow_rules: [
        {
          id: 1,
          rule_name: 'Global DDL',
          ticket_type: 'ddl',
          db_connection_id: null,
          export_sensitivity: null,
          approval_enabled: false,
          execution_mode: 'auto_after_approval',
          approval_auth_groups: ['data_owner'],
          executor_auth_groups: ['dba'],
          priority: 100,
          enabled: true,
        },
      ],
    }))
    mockSettingsDependencies()

    renderSettingsPage()

    const approval = await screen.findByRole('switch', { name: 'Global DDL approval enabled' })
    const autoExecute = screen.getByRole('switch', { name: 'Global DDL auto execute after approval' })
    expect(approval).toBeDisabled()
    expect(approval).toHaveAttribute('aria-checked', 'true')
    expect(autoExecute).toBeDisabled()
    expect(autoExecute).toHaveAttribute('aria-checked', 'false')
  })

  it('allows non-production redis workflow rules to combine no approval with auto execution', async () => {
    mockedGetSettings.mockResolvedValue(makeSettings({
      app_env: 'staging',
      workflow_rules: [
        {
          id: 1,
          rule_name: 'Global Redis Command',
          ticket_type: 'redis_command',
          db_connection_id: null,
          export_sensitivity: null,
          approval_enabled: false,
          execution_mode: 'auto_after_approval',
          approval_auth_groups: [],
          executor_auth_groups: [],
          priority: 100,
          enabled: true,
        },
      ],
    }))
    mockSettingsDependencies()

    renderSettingsPage()

    const approval = await screen.findByRole('switch', { name: 'Global Redis Command approval enabled' })
    const autoExecute = screen.getByRole('switch', { name: 'Global Redis Command auto execute after approval' })
    expect(approval).not.toBeDisabled()
    expect(approval).toHaveAttribute('aria-checked', 'false')
    expect(autoExecute).not.toBeDisabled()
    expect(autoExecute).toHaveAttribute('aria-checked', 'true')
  })

  it('filters workflow DB connection options by ticket type', async () => {
    mockedGetSettings.mockResolvedValue(makeSettings({
      workflow_rules: [
        {
          id: 1,
          rule_name: 'Global DDL',
          ticket_type: 'ddl',
          db_connection_id: null,
          export_sensitivity: null,
          approval_enabled: true,
          execution_mode: 'manual',
          approval_auth_groups: ['data_owner'],
          executor_auth_groups: ['dba'],
          priority: 100,
          enabled: true,
        },
        {
          id: 2,
          rule_name: 'Global Redis Command',
          ticket_type: 'redis_command',
          db_connection_id: null,
          export_sensitivity: null,
          approval_enabled: true,
          execution_mode: 'manual',
          approval_auth_groups: ['data_owner'],
          executor_auth_groups: ['dba'],
          priority: 100,
          enabled: true,
        },
      ],
    }))
    mockSettingsDependencies()

    renderSettingsPage()

    const ddlConnection = await screen.findByRole('button', { name: 'Workflow rule 1 DB connection' })
    fireEvent.click(ddlConnection)
    let options = within(screen.getByRole('listbox', { name: 'Workflow rule 1 DB connection options' }))
    expect(options.getByRole('option', { name: 'analytics-ro' })).toBeInTheDocument()
    expect(options.getByRole('option', { name: 'warehouse-ro' })).toBeInTheDocument()
    expect(options.queryByRole('option', { name: 'cache-redis' })).not.toBeInTheDocument()

    fireEvent.click(ddlConnection)
    const redisConnection = screen.getByRole('button', { name: 'Workflow rule 2 DB connection' })
    fireEvent.click(redisConnection)
    options = within(screen.getByRole('listbox', { name: 'Workflow rule 2 DB connection options' }))
    expect(options.getByRole('option', { name: 'cache-redis' })).toBeInTheDocument()
    expect(options.queryByRole('option', { name: 'analytics-ro' })).not.toBeInTheDocument()
    expect(options.queryByRole('option', { name: 'warehouse-ro' })).not.toBeInTheDocument()
  })
})
