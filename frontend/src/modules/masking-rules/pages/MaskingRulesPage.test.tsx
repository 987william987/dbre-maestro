import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ToastProvider } from '@/shared/ui/ToastContext'
import { MaskingRulesPage } from '@/modules/masking-rules/pages/MaskingRulesPage'

vi.mock('@/modules/masking-rules/api', () => ({
  listMaskingRules: vi.fn(),
  createMaskingRule: vi.fn(),
  deleteMaskingRule: vi.fn(),
}))

vi.mock('@/modules/db-connections/api', () => ({
  listDBConnections: vi.fn(),
}))

import { listDBConnections } from '@/modules/db-connections/api'
import { createMaskingRule, deleteMaskingRule, listMaskingRules } from '@/modules/masking-rules/api'

const mockedListMaskingRules = vi.mocked(listMaskingRules)
const mockedCreateMaskingRule = vi.mocked(createMaskingRule)
const mockedDeleteMaskingRule = vi.mocked(deleteMaskingRule)
const mockedListDBConnections = vi.mocked(listDBConnections)

describe('MaskingRulesPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    mockedListDBConnections.mockResolvedValue({ connections: [] })
    mockedListMaskingRules.mockResolvedValue({ rules: [] })
  })

  it('可以建立 masking rule', async () => {
    mockedCreateMaskingRule.mockResolvedValue({
      id: 1,
      db_connection_id: null,
      table_name: 'tickets',
      column_name: 'email',
      mask_mode: 'full',
      created_by: 1,
      created_at: '2026-01-01T00:00:00Z',
    })
    mockedListMaskingRules
      .mockResolvedValueOnce({ rules: [] })
      .mockResolvedValueOnce({
        rules: [
          {
            id: 1,
            db_connection_id: null,
            table_name: 'tickets',
            column_name: 'email',
            mask_mode: 'full',
            created_by: 1,
            created_at: '2026-01-01T00:00:00Z',
          },
        ],
      })

    render(
      <MemoryRouter>
        <ToastProvider>
          <MaskingRulesPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('heading', { name: 'Masking Rules' })).toBeInTheDocument()

    fireEvent.change(screen.getByPlaceholderText('table_name'), { target: { value: 'tickets' } })
    fireEvent.change(screen.getByPlaceholderText('column_name'), { target: { value: 'email' } })
    fireEvent.click(screen.getByText('建立規則'))

    await waitFor(() => {
      expect(mockedCreateMaskingRule).toHaveBeenCalledWith({
        db_connection_id: null,
        table_name: 'tickets',
        column_name: 'email',
        mask_mode: 'full',
      })
    })
  })

  it('可以刪除 masking rule', async () => {
    mockedListMaskingRules.mockResolvedValue({
      rules: [
        {
          id: 2,
          db_connection_id: 1,
          table_name: 'tickets',
          column_name: 'phone',
          mask_mode: 'partial',
          created_by: 1,
          created_at: '2026-01-01T00:00:00Z',
        },
      ],
    })
    mockedDeleteMaskingRule.mockResolvedValue(undefined)

    render(
      <MemoryRouter>
        <ToastProvider>
          <MaskingRulesPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('cell', { name: 'tickets.phone' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))

    await waitFor(() => {
      expect(mockedDeleteMaskingRule).toHaveBeenCalledWith(2)
    })
  })
})
