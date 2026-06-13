import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { NewTicketPage } from '@/modules/tickets/pages/NewTicketPage'

const mockedNavigate = vi.fn()

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return {
    ...actual,
    useNavigate: () => mockedNavigate,
  }
})

vi.mock('@/modules/tickets/api', () => ({
  createTicket: vi.fn(),
  listConnections: vi.fn(),
}))

import { createTicket, listConnections } from '@/modules/tickets/api'

const mockedCreateTicket = vi.mocked(createTicket)
const mockedListConnections = vi.mocked(listConnections)

describe('NewTicketPage', () => {
  beforeEach(() => {
    mockedNavigate.mockReset()
    mockedCreateTicket.mockReset()
    mockedListConnections.mockReset()

    mockedListConnections.mockResolvedValue({
      connections: [
        {
          id: 1,
          name: 'orders-primary',
          db_type: 'mysql',
          host: 'orders-primary.internal',
          port: 3306,
          database_name: 'orders',
          username: 'readonly',
          encryption_key_version: 1,
          ssl_mode: 'prefer',
          extra_params: null,
          created_by: 1,
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
      ],
    })

    mockedCreateTicket.mockResolvedValue({
      id: 99,
      ticket_no: 'T-099',
      title: 'Add index',
      description: null,
      sql_content: 'ALTER TABLE orders ADD INDEX idx_status (status);',
      ticket_type: 'ddl',
      db_connection_id: 1,
      status: 'pending_review',
      submitter_id: 1,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    })
  })

  it('renders English copy and target db labels without host or port', async () => {
    render(
      <MemoryRouter>
        <NewTicketPage />
      </MemoryRouter>,
    )

    expect(await screen.findByText('New Ticket')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('e.g. Add index, backfill order status')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('Add context, affected scope, rollback plan, and execution considerations.')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Target DB' }))
    expect(await screen.findByRole('option', { name: 'Not Selected' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'orders-primary · MYSQL' })).toBeInTheDocument()
    expect(screen.queryByText(/orders-primary\.internal/)).not.toBeInTheDocument()
    expect(screen.queryByText(/3306/)).not.toBeInTheDocument()
  })

  it('keeps submit disabled until target db is selected', async () => {
    render(
      <MemoryRouter>
        <NewTicketPage />
      </MemoryRouter>,
    )

    expect(await screen.findByText('New Ticket')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText(/Title/i), { target: { value: 'Add index' } })
    fireEvent.change(screen.getByLabelText('SQL Content'), { target: { value: 'ALTER TABLE orders ADD INDEX idx_status (status);' } })

    expect(screen.getByRole('button', { name: 'Submit Ticket' })).toBeDisabled()

    fireEvent.click(screen.getByRole('button', { name: 'Target DB' }))
    fireEvent.click(await screen.findByRole('option', { name: 'orders-primary · MYSQL' }))

    expect(screen.getByRole('button', { name: 'Submit Ticket' })).not.toBeDisabled()
  })

  it('submits the selected connection id', async () => {
    render(
      <MemoryRouter>
        <NewTicketPage />
      </MemoryRouter>,
    )

    expect(await screen.findByText('New Ticket')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText(/Title/i), { target: { value: 'Add index' } })
    fireEvent.change(screen.getByLabelText('SQL Content'), { target: { value: 'ALTER TABLE orders ADD INDEX idx_status (status);' } })
    fireEvent.click(screen.getByRole('button', { name: 'Target DB' }))
    fireEvent.click(await screen.findByRole('option', { name: 'orders-primary · MYSQL' }))
    fireEvent.click(screen.getByRole('button', { name: 'Submit Ticket' }))

    await waitFor(() => {
      expect(mockedCreateTicket).toHaveBeenCalledWith({
        title: 'Add index',
        description: null,
        sql_content: 'ALTER TABLE orders ADD INDEX idx_status (status);',
        ticket_type: 'ddl',
        db_connection_id: 1,
      })
    })
    expect(mockedNavigate).toHaveBeenCalledWith('/tickets/99', { replace: true })
  })
})
