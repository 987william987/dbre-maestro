import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
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

function selectOption(label: string, option: string) {
  fireEvent.click(screen.getByRole('button', { name: label }))
  fireEvent.click(screen.getByRole('option', { name: option }))
}

describe('TicketsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

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
      .mockResolvedValueOnce({ tickets: [], total: 0, limit: 20, offset: 0 })
      .mockResolvedValueOnce({ tickets: [], total: 0, limit: 20, offset: 0 })

    render(
      <MemoryRouter>
        <TicketsPage />
      </MemoryRouter>,
    )

    expect(screen.getByRole('button', { name: 'Status' })).toHaveTextContent('All Status')

    await waitFor(() => expect(mockedListTickets).toHaveBeenCalledWith({
      status: undefined,
      type: undefined,
      ticketNo: undefined,
      title: undefined,
      submitter: undefined,
      from: undefined,
      to: undefined,
      limit: 20,
      offset: 0,
    }))

    selectOption('Status', 'Pending Review')

    await waitFor(() => expect(mockedListTickets).toHaveBeenLastCalledWith({
      status: 'pending_review',
      type: undefined,
      ticketNo: undefined,
      title: undefined,
      submitter: undefined,
      from: undefined,
      to: undefined,
      limit: 20,
      offset: 0,
    }))
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
    mockedListTickets.mockResolvedValueOnce({ tickets: null as never, total: 0, limit: 20, offset: 0 })

    render(
      <MemoryRouter>
        <TicketsPage />
      </MemoryRouter>,
    )

    expect(await screen.findByText('No ticket history yet')).toBeInTheDocument()
  })

  it('moves to the next page when pagination is used', async () => {
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
      .mockResolvedValueOnce({
        tickets: Array.from({ length: 20 }, (_, index) => ({
          id: index + 1,
          ticket_no: `T-${index + 1}`,
          title: `Ticket ${index + 1}`,
          description: null,
          sql_content: `SELECT ${index + 1};`,
          ticket_type: 'ddl',
          status: 'pending_review',
          submitter_id: 1,
          submitter_name: 'alice',
          created_at: '2026-06-12T00:00:00Z',
          updated_at: '2026-06-12T00:00:00Z',
        })),
        total: 21,
        limit: 20,
        offset: 0,
      })
      .mockResolvedValueOnce({
        tickets: [
          {
            id: 21,
            ticket_no: 'T-21',
            title: 'Ticket 21',
            description: null,
            sql_content: 'SELECT 21;',
            ticket_type: 'ddl',
            status: 'pending_review',
            submitter_id: 1,
            submitter_name: 'alice',
            created_at: '2026-06-12T00:00:00Z',
            updated_at: '2026-06-12T00:00:00Z',
          },
        ],
        total: 21,
        limit: 20,
        offset: 20,
      })

    render(
      <MemoryRouter>
        <TicketsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Ticket 1')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: 'Next' }))

    await waitFor(() => expect(mockedListTickets).toHaveBeenLastCalledWith({
      status: undefined,
      type: undefined,
      ticketNo: undefined,
      title: undefined,
      submitter: undefined,
      from: undefined,
      to: undefined,
      limit: 20,
      offset: 20,
    }))
    await waitFor(() => expect(screen.getByText('Ticket 21')).toBeInTheDocument())
  })

  it('列表優先顯示 submitter 名稱而不是 id', async () => {
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

    mockedListTickets.mockResolvedValueOnce({
      tickets: [
        {
          id: 1,
          ticket_no: 'T-001',
          title: 'Readable submitter',
          description: null,
          sql_content: 'SELECT 1;',
          ticket_type: 'ddl',
          db_connection_name: 'aws-jp-shared-mysql-nonprod',
          database_name: 'testnet_user_server',
          status: 'pending_review',
          submitter_id: 99,
          submitter_name: 'alice',
          created_at: '2026-06-12T00:00:00Z',
          updated_at: '2026-06-12T00:00:00Z',
        },
      ],
      total: 1,
      limit: 20,
      offset: 0,
    })

    render(
      <MemoryRouter>
        <TicketsPage />
      </MemoryRouter>,
    )

    expect(await screen.findByText('alice')).toBeInTheDocument()
    expect(screen.getByText('aws-jp-shared-mysql-nonprod')).toBeInTheDocument()
    expect(screen.getByText('testnet_user_server')).toBeInTheDocument()
    expect(screen.queryByText(/^99$/)).not.toBeInTheDocument()
  })

  it('description 欄位預設隱藏，但可以透過欄位 filter 顯示', async () => {
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

    mockedListTickets.mockResolvedValueOnce({
      tickets: [
        {
          id: 1,
          ticket_no: 'T-001',
          title: 'Column filter ticket',
          description: 'Description should be hideable',
          sql_content: 'SELECT 1;',
          ticket_type: 'ddl',
          status: 'pending_review',
          submitter_id: 99,
          submitter_name: 'alice',
          created_at: '2026-06-12T00:00:00Z',
          updated_at: '2026-06-12T00:00:00Z',
        },
      ],
      total: 1,
      limit: 20,
      offset: 0,
    })

    render(
      <MemoryRouter>
        <TicketsPage />
      </MemoryRouter>,
    )

    await screen.findByText('Column filter ticket')
    expect(screen.queryByRole('columnheader', { name: 'Description' })).not.toBeInTheDocument()
    expect(screen.queryByText('Description should be hideable')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Visible Columns' }))
    fireEvent.click(screen.getByRole('menuitemcheckbox', { name: 'Description' }))

    expect(screen.getByRole('columnheader', { name: 'Description' })).toBeInTheDocument()
    expect(screen.getByText('Description should be hideable')).toBeInTheDocument()
  })

  it('submits keyword, type, and status filters with audit-log-style controls', async () => {
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

    mockedListTickets.mockResolvedValue({ tickets: [], total: 0, limit: 20, offset: 0 })

    render(
      <MemoryRouter>
        <TicketsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(mockedListTickets).toHaveBeenCalledTimes(1))

    fireEvent.change(screen.getByPlaceholderText('Ticket no.'), { target: { value: 'TK-2026' } })
    fireEvent.change(screen.getByPlaceholderText('Title'), { target: { value: 'Export' } })
    fireEvent.change(screen.getByPlaceholderText('Submitter'), { target: { value: 'alice' } })
    selectOption('Type', 'SQL Export')
    selectOption('Status', 'Approved')

    await waitFor(() => expect(mockedListTickets).toHaveBeenLastCalledWith({
      status: 'approved',
      type: 'sql_export',
      ticketNo: 'TK-2026',
      title: 'Export',
      submitter: 'alice',
      from: undefined,
      to: undefined,
      limit: 20,
      offset: 0,
    }))
  })
})
