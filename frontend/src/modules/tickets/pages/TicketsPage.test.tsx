import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { TicketsPage } from '@/modules/tickets/pages/TicketsPage'

vi.mock('@/shared/auth/AuthContext', () => ({
  useAuth: vi.fn(),
}))

vi.mock('@/modules/tickets/api', () => ({
  listTickets: vi.fn(),
}))

import { useAuth } from '@/shared/auth/AuthContext'
import { listTickets } from '@/modules/tickets/api'

const mockedUseAuth = vi.mocked(useAuth)
const mockedListTickets = vi.mocked(listTickets)

describe('TicketsPage', () => {
  it('切換狀態篩選時會重新請求對應 status', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: {
        id: 1,
        username: 'dev',
        authGroups: ['developer'],
        authGroupDetails: [],
        permissions: ['tickets.apply'],
        dbConnectionIds: [],
        protected: false,
        isActive: true,
      },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })

    mockedListTickets
      .mockResolvedValueOnce({ tickets: [], limit: 20, offset: 0 })
      .mockResolvedValueOnce({ tickets: [], limit: 20, offset: 0 })

    render(
      <MemoryRouter>
        <TicketsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(mockedListTickets).toHaveBeenCalledWith(undefined, 20, 0))

    fireEvent.change(screen.getByDisplayValue('All'), { target: { value: 'pending_review' } })

    await waitFor(() => expect(mockedListTickets).toHaveBeenLastCalledWith('pending_review', 20, 0))
  })

  it('後端回傳 null tickets 時會退化成空狀態，而不是直接白屏', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: {
        id: 1,
        username: 'dev',
        authGroups: ['developer'],
        authGroupDetails: [],
        permissions: ['tickets.apply'],
        dbConnectionIds: [],
        protected: false,
        isActive: true,
      },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedListTickets.mockResolvedValueOnce({ tickets: null as never, limit: 20, offset: 0 })

    render(
      <MemoryRouter>
        <TicketsPage />
      </MemoryRouter>,
    )

    expect(await screen.findByText('尚無歷史工單')).toBeInTheDocument()
  })
})
