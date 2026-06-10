import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { TicketsPage } from '@/modules/tickets/pages/TicketsPage'

vi.mock('@/modules/tickets/api', () => ({
  listTickets: vi.fn(),
}))

import { listTickets } from '@/modules/tickets/api'

const mockedListTickets = vi.mocked(listTickets)

describe('TicketsPage', () => {
  it('切換狀態篩選時會重新請求對應 status', async () => {
    mockedListTickets
      .mockResolvedValueOnce({ tickets: [], limit: 20, offset: 0 })
      .mockResolvedValueOnce({ tickets: [], limit: 20, offset: 0 })

    render(
      <MemoryRouter>
        <TicketsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(mockedListTickets).toHaveBeenCalledWith(undefined))

    fireEvent.change(screen.getByDisplayValue('全部狀態'), { target: { value: 'pending_review' } })

    await waitFor(() => expect(mockedListTickets).toHaveBeenLastCalledWith('pending_review'))
  })

  it('後端回傳 null tickets 時會退化成空狀態，而不是直接白屏', async () => {
    mockedListTickets.mockResolvedValueOnce({ tickets: null as never, limit: 20, offset: 0 })

    render(
      <MemoryRouter>
        <TicketsPage />
      </MemoryRouter>,
    )

    expect(await screen.findByText('尚無歷史工單')).toBeInTheDocument()
  })
})
