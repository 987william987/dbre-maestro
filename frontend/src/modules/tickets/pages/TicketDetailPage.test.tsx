import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Ticket, TicketDetail } from '@/shared/types/ticket'
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
  downloadTicketExport: vi.fn(),
}))

vi.mock('@/shared/ui/ToastContext', () => ({
  useToast: () => ({
    pushToast: vi.fn(),
  }),
}))

import { useAuth } from '@/shared/auth/AuthContext'
import { downloadTicketExport, getTicket } from '@/modules/tickets/api'

const mockedUseAuth = vi.mocked(useAuth)
const mockedGetTicket = vi.mocked(getTicket)
const mockedDownloadTicketExport = vi.mocked(downloadTicketExport)

const baseTicket: Ticket = {
  id: 12,
  ticket_no: 'T-012',
  title: 'Backfill user flags',
  description: 'Fix historical rows',
  sql_content: 'UPDATE users SET flagged = 1 WHERE id < 10;',
  ticket_type: 'dml',
  db_connection_id: 3,
  db_connection_name: 'analytics-primary',
  status: 'pending_review',
  submitter_id: 1,
  submitter_name: 'alice',
  reviewer_id: null,
  reviewer_name: null,
  executor_id: null,
  executor_name: null,
  review_comment: null,
  rejection_reason: null,
  scheduled_at: null,
  started_at: null,
  completed_at: null,
  revoked_by: null,
  revoked_by_name: null,
  created_at: '2026-06-09T10:00:00Z',
  updated_at: '2026-06-09T10:00:00Z',
}

function buildDetail(ticket: Ticket, overrides?: Partial<TicketDetail>): TicketDetail {
  return {
    ticket,
    executions: [],
    scopes: [],
    export_request: null,
    capabilities: {
      can_review: false,
      can_revoke: false,
      can_request_execution: false,
      can_execute: false,
      can_download_export: false,
    },
    ...overrides,
  }
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
    mockedDownloadTicketExport.mockReset()
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
    mockedGetTicket.mockResolvedValue(buildDetail({ ...baseTicket, status: 'pending_review' }, {
      capabilities: {
        can_review: true,
        can_revoke: false,
        can_request_execution: false,
        can_execute: false,
        can_download_export: false,
      },
    }))

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
    mockedGetTicket.mockResolvedValue(buildDetail({ ...baseTicket, status: 'approved' }, {
      capabilities: {
        can_review: false,
        can_revoke: false,
        can_request_execution: true,
        can_execute: true,
        can_download_export: false,
      },
    }))

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
    mockedGetTicket.mockResolvedValue(buildDetail({ ...baseTicket, status: 'pending_review' }))

    renderPage()

    await waitFor(() => expect(screen.getByText('SQL Content')).toBeInTheDocument())
    expect(screen.queryByText('審核操作')).not.toBeInTheDocument()
    expect(screen.queryByText('執行流程')).not.toBeInTheDocument()
  })

  it('sql export submitter 在 ready export 存在時可看到下載入口', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: { id: 1, username: 'dev', authGroups: ['developer'], authGroupDetails: [], permissions: ['sql_editor.export'], dbConnectionIds: [], protected: false, isActive: true },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedGetTicket.mockResolvedValue(buildDetail({
      ...baseTicket,
      ticket_type: 'sql_export',
      status: 'approved',
      sql_content: 'SELECT * FROM users;',
    }, {
      export_request: {
        status: 'ready',
        expires_at: '2026-06-12T01:00:00Z',
        download_url: '/api/exports/download/token-123',
      },
      capabilities: {
        can_review: false,
        can_revoke: false,
        can_request_execution: false,
        can_execute: false,
        can_download_export: true,
      },
    }))

    renderPage()

    await waitFor(() => expect(screen.getByText('匯出下載')).toBeInTheDocument())
    expect(screen.getByRole('button', { name: 'Download Export' })).toBeInTheDocument()
  })

  it('工單資訊優先顯示人類可讀名稱而不是純 id', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: { id: 1, username: 'dev', authGroups: ['developer'], authGroupDetails: [], permissions: ['tickets.apply'], dbConnectionIds: [], protected: false, isActive: true },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedGetTicket.mockResolvedValue(buildDetail({
      ...baseTicket,
      reviewer_id: 2,
      reviewer_name: 'reviewer.bob',
      executor_id: 3,
      executor_name: 'dba.cindy',
      revoked_by: 4,
      revoked_by_name: 'ops.dan',
      revoked_at: '2026-06-10T10:00:00Z',
    }))

    renderPage()

    expect(await screen.findByText('analytics-primary')).toBeInTheDocument()
    expect(screen.getByText('alice')).toBeInTheDocument()
    expect(screen.getByText('reviewer.bob')).toBeInTheDocument()
    expect(screen.getByText('dba.cindy')).toBeInTheDocument()
    expect(screen.getByText('ops.dan')).toBeInTheDocument()
    expect(screen.queryByText(/^3$/)).not.toBeInTheDocument()
  })
})
