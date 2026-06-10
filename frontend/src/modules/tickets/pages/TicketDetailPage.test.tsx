import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Ticket } from '@/shared/types/ticket'
import { TicketDetailPage } from '@/modules/tickets/pages/TicketDetailPage'

vi.mock('@/shared/auth/AuthContext', () => ({
  useAuth: vi.fn(),
}))

vi.mock('@/modules/tickets/api', () => ({
  getTicket: vi.fn(),
  approveTicket: vi.fn(),
  rejectTicket: vi.fn(),
  requestExecution: vi.fn(),
  executeTicket: vi.fn(),
}))

vi.mock('@/shared/ui/ToastContext', () => ({
  useToast: () => ({
    pushToast: vi.fn(),
  }),
}))

import { useAuth } from '@/shared/auth/AuthContext'
import { getTicket } from '@/modules/tickets/api'

const mockedUseAuth = vi.mocked(useAuth)
const mockedGetTicket = vi.mocked(getTicket)

const baseTicket: Ticket = {
  id: 12,
  ticket_no: 'T-012',
  title: 'Backfill user flags',
  description: 'Fix historical rows',
  sql_content: 'UPDATE users SET flagged = 1 WHERE id < 10;',
  ticket_type: 'dml',
  db_connection_id: 3,
  status: 'pending_review',
  submitter_id: 1,
  reviewer_id: null,
  executor_id: null,
  review_comment: null,
  rejection_reason: null,
  scheduled_at: null,
  started_at: null,
  completed_at: null,
  created_at: '2026-06-09T10:00:00Z',
  updated_at: '2026-06-09T10:00:00Z',
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/tickets/12']}>
      <Routes>
        <Route path="/tickets" element={<div>tickets page</div>} />
        <Route path="/tickets/:id" element={<TicketDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('TicketDetailPage role visibility', () => {
  beforeEach(() => {
    mockedGetTicket.mockReset()
  })

  it('reviewer 在 pending_review 狀態可見 approve / reject', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: { id: 2, username: 'reviewer', authGroups: ['reviewer'], authGroupDetails: [], permissions: ['tickets.review'], dbConnectionIds: [], protected: false, isActive: true },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedGetTicket.mockResolvedValue({ ...baseTicket, status: 'pending_review' })

    renderPage()

    await waitFor(() => expect(screen.getByText('審核通過')).toBeInTheDocument())
    expect(screen.getByText('拒絕工單')).toBeInTheDocument()
    expect(screen.queryByText('Request Execution')).not.toBeInTheDocument()
  })

  it('dba 在 approved 狀態可見 request execution', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: { id: 3, username: 'dba', authGroups: ['dba'], authGroupDetails: [], permissions: ['tickets.execute'], dbConnectionIds: [], protected: false, isActive: true },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedGetTicket.mockResolvedValue({ ...baseTicket, status: 'approved' })

    renderPage()

    await waitFor(() => expect(screen.getByText('Request Execution')).toBeInTheDocument())
    expect(screen.getByText('Execute')).toBeInTheDocument()
  })

  it('developer 不會看到審核或 DBA 操作面板', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: { id: 1, username: 'dev', authGroups: ['developer'], authGroupDetails: [], permissions: ['tickets.apply'], dbConnectionIds: [], protected: false, isActive: true },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedGetTicket.mockResolvedValue({ ...baseTicket, status: 'pending_review' })

    renderPage()

    await waitFor(() => expect(screen.getByText('SQL Content')).toBeInTheDocument())
    expect(screen.queryByText('審核操作')).not.toBeInTheDocument()
    expect(screen.queryByText('執行流程')).not.toBeInTheDocument()
  })
})
