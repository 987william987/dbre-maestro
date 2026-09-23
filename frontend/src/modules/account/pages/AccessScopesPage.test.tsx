import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getAccountAccessScopes } from '@/modules/account/api'
import { AccessScopesPage } from './AccessScopesPage'

vi.mock('@/modules/account/api', () => ({ getAccountAccessScopes: vi.fn() }))
const mockedGetAccountAccessScopes = vi.mocked(getAccountAccessScopes)

describe('AccessScopesPage', () => {
  beforeEach(() => vi.clearAllMocks())

  it('可搜尋 scope，並為可續期權限提供 renew 連結', async () => {
    mockedGetAccountAccessScopes.mockResolvedValue({
      db_scopes: [{ id: 1, name: 'Primary MySQL', db_type: 'mysql' }],
      query_access_scopes: [{
        id: 2, connection_id: 1, connection_name: 'Primary MySQL', subject_type: 'user', effect: 'allow',
        database_pattern: 'billing', table_pattern: '*', granted_via: 'ticket', expires_at: null,
        remaining_days: null, source_ticket_no: 'TK-2', expiring_soon: false,
        renew_ticket_path: '/tickets/new?ticket_type=query_access&renew=2',
      }],
      db_scope_count: 1,
      query_scope_count: 1,
    })
    render(<MemoryRouter><AccessScopesPage /></MemoryRouter>)

    expect(await screen.findByRole('link', { name: 'Renew' })).toHaveAttribute('href', '/tickets/new?ticket_type=query_access&renew=2')
    fireEvent.change(screen.getByPlaceholderText('Search scopes...'), { target: { value: 'missing' } })
    expect(screen.getByText('No DB submission scope found.')).toBeInTheDocument()
    expect(screen.getByText('No active query access scope found.')).toBeInTheDocument()
  })

  it('載入失敗時顯示錯誤狀態', async () => {
    mockedGetAccountAccessScopes.mockRejectedValue(new Error('offline'))
    render(<MemoryRouter><AccessScopesPage /></MemoryRouter>)
    expect(await screen.findByText('Failed to load access scopes.')).toBeInTheDocument()
  })
})
