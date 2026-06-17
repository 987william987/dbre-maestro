import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ToastProvider } from '@/shared/ui/ToastContext'
import { SQLReviewRulesPage } from '@/modules/sql-review-rules/pages/SQLReviewRulesPage'

vi.mock('@/modules/sql-review-rules/api', () => ({
  listSQLReviewRules: vi.fn(),
  patchSQLReviewRule: vi.fn(),
}))

vi.mock('@/shared/auth/AuthContext', () => ({
  useAuth: () => ({
    user: {
      permissions: ['sql_review.read', 'sql_review.write'],
    },
  }),
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

  it('updates a SQL review rule', async () => {
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

    expect(await screen.findByRole('heading', { name: 'SQL Review Rules' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('switch'))
    fireEvent.change(screen.getByPlaceholderText('Row limit'), { target: { value: '5000' } })
    fireEvent.click(screen.getByText('Save'))

    await waitFor(() => {
      expect(mockedPatchSQLReviewRule).toHaveBeenCalledWith('high_row_count', {
        enabled: false,
        threshold: 5000,
      })
    })
  })

  it('paginates the SQL review rule list', async () => {
    mockedListSQLReviewRules.mockResolvedValue({
      rules: Array.from({ length: 21 }, (_, index) => ({
        id: index + 1,
        rule_name: `rule_${index + 1}`,
        enabled: true,
        threshold: 1000 + index,
        description: `Rule ${index + 1}`,
        updated_by: 1,
        updated_at: '2026-01-01T00:00:00Z',
      })),
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLReviewRulesPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('rule_1')).toBeInTheDocument()
    expect(screen.queryByText('rule_21')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Next' }))

    await waitFor(() => expect(screen.getByText('rule_21')).toBeInTheDocument())
    expect(screen.queryByText('rule_1')).not.toBeInTheDocument()
  })

  it('only allows threshold editing for high_row_count', async () => {
    mockedListSQLReviewRules.mockResolvedValue({
      rules: [
        {
          id: 1,
          rule_name: 'ddl_no_comment',
          enabled: true,
          threshold: null,
          description: 'legacy text',
          updated_by: 1,
          updated_at: '2026-01-01T00:00:00Z',
        },
        {
          id: 2,
          rule_name: 'high_row_count',
          enabled: true,
          threshold: 1000,
          description: 'legacy text',
          updated_by: 1,
          updated_at: '2026-01-01T00:00:00Z',
        },
      ],
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <SQLReviewRulesPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByText('Require CREATE TABLE statements to include a table comment.')).toBeInTheDocument()
    expect(screen.getByDisplayValue('1000')).toBeInTheDocument()
    expect(screen.getByDisplayValue('N/A')).toBeDisabled()
  })
})
