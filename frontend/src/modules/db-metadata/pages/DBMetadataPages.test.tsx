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
    expect(screen.getByLabelText('Inventory endpoint search')).toBeInTheDocument()
    expect(screen.getByLabelText('Version Search')).toBeInTheDocument()
    expect(screen.getByLabelText('Size Search')).toBeInTheDocument()
    expect(screen.getByLabelText('Tags Search')).toBeInTheDocument()
    expect(screen.getByLabelText('Role')).toBeInTheDocument()
    expect(screen.getByLabelText('Mapping')).toBeInTheDocument()
    expect(screen.getByText('No inventory snapshots match the current filters.')).toBeInTheDocument()
  })

  it('inventory page supports inventory filters and column filters', async () => {
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
          cluster_reader_endpoint: 'orders-reader.cluster.local',
          cluster_endpoint: null,
          tags: { env: 'prod' },
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
          cluster_reader_endpoint: 'ledger-reader.cluster.local',
          cluster_endpoint: null,
          tags: { env: 'stage' },
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

    fireEvent.change(screen.getByLabelText('Inventory endpoint search'), { target: { value: 'ledger-reader' } })

    expect(screen.queryByText('orders-primary')).not.toBeInTheDocument()
    expect(screen.getAllByText('ledger-replica').length).toBeGreaterThan(0)

    fireEvent.change(screen.getByLabelText('Version Search'), { target: { value: '16' } })
    fireEvent.change(screen.getByLabelText('Size Search'), { target: { value: 'xlarge' } })
    fireEvent.change(screen.getByLabelText('Tags Search'), { target: { value: 'stage' } })

    expect(screen.getAllByText('ledger-replica').length).toBeGreaterThan(0)

    fireEvent.click(screen.getByRole('button', { name: 'Visible Columns' }))
    fireEvent.click(screen.getByRole('menuitemcheckbox', { name: 'Last Synced' }))

    expect(screen.queryByRole('columnheader', { name: 'Last Synced' })).not.toBeInTheDocument()
  })

  it('objects page shows the empty state', async () => {
    mockedListDBObjectSnapshots.mockResolvedValue({ items: [], total: 0, connection_options: [] })

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
          row_count: 120,
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
          row_count: 240,
          data_size_bytes: 2048,
          index_size_bytes: 256,
        },
      ],
      total: 2,
      connection_options: [
        { id: 12, name: 'analytics-mysql-ro', db_type: 'mysql' },
        { id: 18, name: 'analytics-pg-ro', db_type: 'postgres' },
      ],
    })

    render(
      <MemoryRouter>
        <DBMetadataObjectsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('analytics-mysql-ro')).toBeInTheDocument())
    expect(screen.getByText('analytics-pg-ro')).toBeInTheDocument()
    expect(screen.getByText('customers').compareDocumentPosition(screen.getByText('orders')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Data Size' }))
    expect(screen.getByText('customers').compareDocumentPosition(screen.getByText('orders')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Data Size DESC/i }))
    expect(screen.getByText('orders').compareDocumentPosition(screen.getByText('customers')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

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
