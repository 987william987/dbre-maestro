import { render, screen, waitFor } from '@testing-library/react'
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

  it('inventory page 可顯示空狀態', async () => {
    mockedListInventorySnapshots.mockResolvedValue({ items: [], total: 0 })

    render(
      <MemoryRouter>
        <DBMetadataInventoryPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('DB Metadata / 實例總覽')).toBeInTheDocument())
    expect(screen.getByRole('link', { name: '實例總覽' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '資料庫物件' })).toBeInTheDocument()
    expect(screen.getByLabelText('Engine')).toBeInTheDocument()
    expect(screen.getByLabelText('Identifier 搜尋')).toBeInTheDocument()
    expect(screen.getByLabelText('Role')).toBeInTheDocument()
    expect(screen.getByText('查無符合條件的 inventory snapshot。')).toBeInTheDocument()
  })

  it('objects page 可顯示空狀態', async () => {
    mockedListDBObjectSnapshots.mockResolvedValue({ items: [], total: 0 })

    render(
      <MemoryRouter>
        <DBMetadataObjectsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('DB Metadata / 資料庫物件')).toBeInTheDocument())
    expect(screen.getByRole('link', { name: '實例總覽' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '資料庫物件' })).toBeInTheDocument()
    expect(screen.getByText('尚未有任何 object snapshot。')).toBeInTheDocument()
  })
})
