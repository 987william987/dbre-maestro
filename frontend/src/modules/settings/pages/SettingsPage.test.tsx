import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SettingsPage } from '@/modules/settings/pages/SettingsPage'
import { ToastProvider } from '@/shared/ui/ToastContext'

vi.mock('@/modules/settings/api', () => ({
  getSettings: vi.fn(),
  listSettingsDBConnections: vi.fn(),
}))

import { getSettings, listSettingsDBConnections } from '@/modules/settings/api'

const mockedGetSettings = vi.mocked(getSettings)
const mockedListSettingsDBConnections = vi.mocked(listSettingsDBConnections)

describe('SettingsPage', () => {
  beforeEach(() => {
    mockedGetSettings.mockReset()
    mockedListSettingsDBConnections.mockReset()
  })

  it('loads the settings page without depending on the users API', async () => {
    mockedGetSettings.mockResolvedValue({
      sensitive_export_reviewer_user_ids: [],
      sensitive_query_access_reviewer_user_ids: [],
      db_metadata_inventory_enabled: true,
      db_metadata_inventory_regions: ['ap-northeast-1'],
      db_metadata_inventory_engines: ['aurora-mysql', 'aurora-postgresql', 'redis'],
      db_metadata_inventory_sync_interval_minutes: 5,
      db_metadata_object_enabled: true,
      db_metadata_object_enabled_connection_ids: [12, 18],
      db_metadata_object_sync_interval_minutes: 60,
    })
    mockedListSettingsDBConnections.mockResolvedValue({
      connections: [
        { id: 12, name: 'analytics-ro', db_type: 'mysql', host: 'db-a.internal', port: 3306 },
        { id: 18, name: 'warehouse-ro', db_type: 'postgres', host: 'pg-a.internal', port: 5432 },
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
    expect(screen.getByText('analytics-ro')).toBeInTheDocument()
    expect(screen.getByText('warehouse-ro')).toBeInTheDocument()
    expect(screen.getByText('2 selected')).toBeInTheDocument()
    expect(screen.getByRole('switch', { name: 'analytics-ro selected for object scan' })).toBeInTheDocument()
    expect(screen.getByRole('switch', { name: 'warehouse-ro selected for object scan' })).toBeInTheDocument()
    expect(screen.queryByText('ID 12')).not.toBeInTheDocument()
    expect(screen.queryByText('db-a.internal:3306')).not.toBeInTheDocument()
  })
})
