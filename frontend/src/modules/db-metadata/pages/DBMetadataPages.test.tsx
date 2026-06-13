import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DBMetadataInventoryPage } from '@/modules/db-metadata/pages/DBMetadataInventoryPage'
import { DBMetadataObjectsPage } from '@/modules/db-metadata/pages/DBMetadataObjectsPage'

vi.mock('@/modules/db-metadata/api', () => ({
  listInventorySnapshots: vi.fn(),
  listDBObjectSnapshots: vi.fn(),
}))

import { listDBObjectSnapshots, listInventorySnapshots } from '@/modules/db-metadata/api'

const mockedListInventorySnapshots = vi.mocked(listInventorySnapshots)
const mockedListDBObjectSnapshots = vi.mocked(listDBObjectSnapshots)

describe('DBMetadata pages', () => {
  beforeEach(() => {
    mockedListInventorySnapshots.mockReset()
    mockedListDBObjectSnapshots.mockReset()
  })

  it('inventory page shows the empty state', async () => {
    mockedListInventorySnapshots.mockResolvedValue({ items: [], total: 0 })

    render(
      <MemoryRouter>
        <DBMetadataInventoryPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Inventory' })).toBeInTheDocument())
    expect(screen.getByRole('link', { name: 'Inventory' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Objects' })).toBeInTheDocument()
    expect(screen.getByLabelText('Engine')).toBeInTheDocument()
    expect(screen.getByLabelText('Identifier Search')).toBeInTheDocument()
    expect(screen.getByLabelText('Role')).toBeInTheDocument()
    expect(screen.getByText('No inventory snapshots match the current filters.')).toBeInTheDocument()
  })

  it('inventory page supports identifier and column filters', async () => {
    mockedListInventorySnapshots.mockResolvedValue({
      items: [
        {
          id: 1,
          snapshot_at: '2026-06-12T10:00:00Z',
          provider: 'aws',
          engine: 'mysql',
          engine_version: '8.0',
          region: 'ap-northeast-1',
          az: 'ap-northeast-1a',
          role: 'writer',
          instance_class: 'db.r6g.large',
          db_identifier: 'orders-primary',
          instance_endpoint: 'orders-primary.cluster.local',
          cluster_endpoint: null,
          mapping_status: 'mapped',
          mapping_connections: ['orders-prod'],
        },
        {
          id: 2,
          snapshot_at: '2026-06-12T10:05:00Z',
          provider: 'aws',
          engine: 'postgres',
          engine_version: '16',
          region: 'ap-northeast-1',
          az: 'ap-northeast-1c',
          role: 'reader',
          instance_class: 'db.r6g.xlarge',
          db_identifier: 'ledger-replica',
          instance_endpoint: 'ledger-replica.cluster.local',
          cluster_endpoint: null,
          mapping_status: 'unmapped',
          mapping_connections: [],
        },
      ],
      total: 2,
    })

    render(
      <MemoryRouter>
        <DBMetadataInventoryPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('orders-primary')).toBeInTheDocument())
    expect(screen.getByText('ledger-replica')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Identifier Search'), { target: { value: 'ledger' } })

    expect(screen.queryByText('orders-primary')).not.toBeInTheDocument()
    expect(screen.getAllByText('ledger-replica').length).toBeGreaterThan(0)

    fireEvent.click(screen.getByRole('button', { name: 'Visible Columns' }))
    fireEvent.click(screen.getByRole('menuitemcheckbox', { name: 'Last Synced' }))

    expect(screen.queryByRole('columnheader', { name: 'Last Synced' })).not.toBeInTheDocument()
  })

  it('objects page shows the empty state', async () => {
    mockedListDBObjectSnapshots.mockResolvedValue({ items: [], total: 0 })

    render(
      <MemoryRouter>
        <DBMetadataObjectsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Objects' })).toBeInTheDocument())
    expect(screen.getByRole('link', { name: 'Inventory' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Objects' })).toBeInTheDocument()
    expect(screen.getByText('No object snapshots yet.')).toBeInTheDocument()
  })

  it('objects page supports connection and column filters', async () => {
    mockedListDBObjectSnapshots.mockResolvedValue({
      items: [
        {
          id: 1,
          snapshot_at: '2026-06-12T10:00:00Z',
          db_connection_id: 12,
          connection_name: 'analytics-mysql-ro',
          engine: 'mysql',
          cluster_name: 'aurora-cluster-a',
          node_name: 'aurora-node-a',
          database_name: 'analytics',
          schema_name: 'analytics',
          table_name: 'orders',
          data_size_bytes: 1024,
          index_size_bytes: 512,
        },
        {
          id: 2,
          snapshot_at: '2026-06-12T10:05:00Z',
          db_connection_id: 18,
          connection_name: 'analytics-pg-ro',
          engine: 'postgres',
          cluster_name: 'aurora-cluster-b',
          node_name: 'aurora-node-b',
          database_name: 'warehouse',
          schema_name: 'public',
          table_name: 'customers',
          data_size_bytes: 2048,
          index_size_bytes: 256,
        },
      ],
      total: 2,
    })

    render(
      <MemoryRouter>
        <DBMetadataObjectsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('analytics-mysql-ro')).toBeInTheDocument())
    expect(screen.getByText('analytics-pg-ro')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Engine' }))
    fireEvent.click(screen.getByRole('option', { name: 'postgres' }))

    expect(screen.queryByText('analytics-mysql-ro')).not.toBeInTheDocument()
    expect(screen.getAllByText('analytics-pg-ro').length).toBeGreaterThan(0)

    fireEvent.click(screen.getByRole('button', { name: 'Connection' }))
    fireEvent.click(screen.getByRole('option', { name: 'analytics-pg-ro' }))

    expect(screen.getAllByText('analytics-pg-ro').length).toBeGreaterThan(0)

    fireEvent.click(screen.getByRole('button', { name: 'Visible Columns' }))
    fireEvent.click(screen.getByRole('menuitemcheckbox', { name: 'Index Size' }))

    expect(screen.queryByRole('columnheader', { name: 'Index Size' })).not.toBeInTheDocument()
  })
})
