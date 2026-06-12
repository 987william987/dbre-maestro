import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ToastProvider } from '@/shared/ui/ToastContext'
import { SQLReviewRulesPage } from '@/modules/sql-review-rules/pages/SQLReviewRulesPage'

vi.mock('@/modules/sql-review-rules/api', () => ({
  listSQLReviewRules: vi.fn(),
  patchSQLReviewRule: vi.fn(),
}))

import { listSQLReviewRules, patchSQLReviewRule } from '@/modules/sql-review-rules/api'

const mockedListSQLReviewRules = vi.mocked(listSQLReviewRules)
const mockedPatchSQLReviewRule = vi.mocked(patchSQLReviewRule)

describe('SQLReviewRulesPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    mockedListSQLReviewRules.mockResolvedValue({
      rules: [
        {
          id: 1,
          rule_name: 'high_row_count',
          enabled: true,
          threshold: 10000,
          description: 'Reject queries where estimated row count exceeds threshold',
          updated_by: 1,
          updated_at: '2026-01-01T00:00:00Z',
        },
      ],
    })
  })

  it('可以更新 sql review rule', async () => {
    mockedPatchSQLReviewRule.mockResolvedValue({
      id: 1,
      rule_name: 'high_row_count',
      enabled: false,
      threshold: 5000,
      description: 'Reject queries where estimated row count exceeds threshold',
      updated_by: 1,
      updated_at: '2026-01-01T00:00:00Z',
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLReviewRulesPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('heading', { name: 'SQL 審核規則' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('checkbox'))
    fireEvent.change(screen.getByPlaceholderText('threshold'), { target: { value: '5000' } })
    fireEvent.click(screen.getByText('儲存'))

    await waitFor(() => {
      expect(mockedPatchSQLReviewRule).toHaveBeenCalledWith('high_row_count', {
        enabled: false,
        threshold: 5000,
      })
    })
  })
})
