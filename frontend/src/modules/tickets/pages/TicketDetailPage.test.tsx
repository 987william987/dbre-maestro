import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Ticket, TicketDetail, TicketStatus, TicketType } from '@/shared/types/ticket'
import { TicketDetailPage } from '@/modules/tickets/pages/TicketDetailPage'

vi.mock('@/shared/auth/AuthContext', () => ({
  useAuth: vi.fn(),
}))

vi.mock('@/modules/tickets/api', () => ({
  getTicket: vi.fn(),
  approveTicket: vi.fn(),
  rejectTicket: vi.fn(),
  withdrawTicket: vi.fn(),
  executeTicket: vi.fn(),
  downloadTicketExport: vi.fn(),
  revokeTicket: vi.fn(),
}))

vi.mock('@/shared/ui/ToastContext', () => ({
  useToast: () => ({
    pushToast: vi.fn(),
  }),
}))

import { useAuth } from '@/shared/auth/AuthContext'
import { downloadTicketExport, getTicket, withdrawTicket } from '@/modules/tickets/api'

const mockedUseAuth = vi.mocked(useAuth)
const mockedGetTicket = vi.mocked(getTicket)
const mockedDownloadTicketExport = vi.mocked(downloadTicketExport)
const mockedWithdrawTicket = vi.mocked(withdrawTicket)

const baseTicket: Ticket = {
  id: 12,
  ticket_no: 'T-012',
  title: 'Backfill user flags',
  description: 'Fix historical rows',
  sql_content: 'UPDATE users SET flagged = 1 WHERE id < 10;',
  ticket_type: 'dml',
  db_connection_id: 3,
  db_connection_name: 'analytics-primary',
  database_name: 'analytics_app',
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
    execution_rollbacks: [],
    review_results: [],
    activity_logs: [],
    scopes: [],
    query_access_items: [],
    export_request: null,
    workflow_participants: {
      reviewers: [],
      executors: [],
    },
    capabilities: {
      can_review: false,
      can_reject: false,
      can_withdraw: false,
      can_revoke: false,
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
        <Route path="/tickets/new" element={<div>new ticket page</div>} />
        <Route path="/tickets/:id" element={<TicketDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('TicketDetailPage role visibility', () => {
  beforeEach(() => {
    mockedGetTicket.mockReset()
    mockedDownloadTicketExport.mockReset()
    mockedWithdrawTicket.mockReset()
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
      workflow_participants: {
        reviewers: ['reviewer.bob'],
        executors: ['dba.cindy'],
      },
      capabilities: {
        can_review: true,
        can_reject: true,
        can_withdraw: false,
        can_revoke: false,
        can_execute: false,
        can_download_export: false,
      },
    }))

    renderPage()

    await waitFor(() => expect(screen.getByText('Approve')).toBeInTheDocument())
    expect(screen.getByText('Approval Flow')).toBeInTheDocument()
    expect(screen.getByText('reviewer.bob')).toBeInTheDocument()
    expect(screen.getByText('dba.cindy')).toBeInTheDocument()
    expect(screen.queryByText('Waiting for review')).not.toBeInTheDocument()
    expect(screen.getByText('Reject')).toBeInTheDocument()
    expect(screen.queryByText('Request Execution')).not.toBeInTheDocument()
  })

  it('dba 在 approved 狀態不再顯示 request execution', async () => {
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
      workflow_participants: {
        reviewers: ['reviewer.bob'],
        executors: ['dba.cindy', 'dba.edgar'],
      },
      capabilities: {
        can_review: false,
        can_reject: true,
        can_withdraw: false,
        can_revoke: false,
        can_execute: true,
        can_download_export: false,
      },
    }))

    renderPage()

    await waitFor(() => expect(screen.getByText('dba.cindy, dba.edgar')).toBeInTheDocument())
    expect(screen.queryByText('Review completed')).not.toBeInTheDocument()
    expect(screen.getByText('dba.cindy, dba.edgar')).toBeInTheDocument()
    expect(screen.queryByText('Waiting for DBA execution')).not.toBeInTheDocument()
    expect(screen.queryByText('Request Execution')).not.toBeInTheDocument()
    expect(screen.queryByText('Execute')).not.toBeInTheDocument()
  })

  it('dba 在 pending_execution 狀態不顯示 request execution，但可見 execution reject', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: { id: 3, username: 'dba', authGroups: ['dba'], authGroupDetails: [], permissions: ['tickets.execute'], dbConnectionIds: [], protected: false, isActive: true },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedGetTicket.mockResolvedValue(buildDetail({ ...baseTicket, status: 'pending_execution' }, {
      capabilities: {
        can_review: false,
        can_reject: true,
        can_withdraw: false,
        can_revoke: false,
        can_execute: true,
        can_download_export: false,
      },
    }))

    renderPage()

    await waitFor(() => expect(screen.getByText('Execute')).toBeInTheDocument())
    expect(screen.queryByText('Request Execution')).not.toBeInTheDocument()
    expect(screen.getByText('Reject')).toBeInTheDocument()
  })

  it('免審批但人工執行的 DDL 工單，Review 顯示 System 且 Execution 顯示執行者', async () => {
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
      ticket_type: 'ddl',
      status: 'completed',
      reviewer_id: null,
      reviewer_name: null,
      executor_id: 3,
      executor_name: 'william',
      sql_content: 'ALTER TABLE users ADD COLUMN note VARCHAR(255);',
    }, {
      workflow_participants: {
        reviewers: ['admin_sre_test', 'william', 'kirin'],
        executors: ['william'],
      },
      workflow_resolution: {
        approval_enabled: false,
        execution_mode: 'manual',
      },
    }))

    renderPage()

    await waitFor(() => expect(screen.getByText('Approval Flow')).toBeInTheDocument())
    expect(screen.getAllByText('System').length).toBeGreaterThanOrEqual(2)
    expect(screen.getAllByText('william').length).toBeGreaterThan(0)
    expect(screen.queryByText('admin_sre_test, william, kirin')).not.toBeInTheDocument()
  })

  it('免審批且自動執行的 DML 工單，Review 和 Execution 都顯示 System', async () => {
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
      ticket_type: 'dml',
      status: 'failed',
      reviewer_id: null,
      reviewer_name: null,
      executor_id: 0,
      executor_name: null,
      sql_content: 'UPDATE users SET flagged = 1 WHERE id < 10;',
    }, {
      workflow_participants: {
        reviewers: ['admin_sre_test', 'william', 'kirin'],
        executors: [],
      },
      workflow_resolution: {
        approval_enabled: false,
        execution_mode: 'auto_after_approval',
      },
    }))

    renderPage()

    await waitFor(() => expect(screen.getByText('Approval Flow')).toBeInTheDocument())
    expect(screen.getAllByText('System').length).toBeGreaterThanOrEqual(4)
    expect(screen.queryByText(/^0$/)).not.toBeInTheDocument()
    expect(screen.queryByText('admin_sre_test, william, kirin')).not.toBeInTheDocument()
  })

  it('submitter 在 pending_review 狀態可填寫原因並 withdraw', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: { id: 1, username: 'alice', authGroups: ['developer'], authGroupDetails: [], permissions: ['tickets.apply'], dbConnectionIds: [], protected: false, isActive: true },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedGetTicket.mockResolvedValue(buildDetail({ ...baseTicket, status: 'pending_review' }, {
      capabilities: {
        can_review: false,
        can_reject: false,
        can_withdraw: true,
        can_revoke: false,
        can_execute: false,
        can_download_export: false,
      },
    }))
    mockedWithdrawTicket.mockResolvedValue({ ...baseTicket, status: 'withdrawn', rejection_reason: '需求改變，先撤回' })

    renderPage()

    const reasonInput = await screen.findByPlaceholderText('Withdraw reason (optional)')
    fireEvent.change(reasonInput, { target: { value: '需求改變，先撤回' } })
    fireEvent.click(screen.getByText('Withdraw Ticket'))
    fireEvent.click(await screen.findByRole('button', { name: 'Withdraw' }))

    await waitFor(() => expect(mockedWithdrawTicket).toHaveBeenCalledWith('T-012', '需求改變，先撤回'))
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

  it.each([
    ['completed', 'ddl'],
    ['completed', 'dml'],
    ['completed', 'redis_command'],
    ['failed', 'ddl'],
    ['failed', 'dml'],
    ['failed', 'redis_command'],
    ['interrupted', 'ddl'],
    ['interrupted', 'dml'],
    ['interrupted', 'redis_command'],
    ['rejected', 'ddl'],
    ['rejected', 'dml'],
    ['rejected', 'redis_command'],
    ['withdrawn', 'ddl'],
    ['withdrawn', 'dml'],
    ['withdrawn', 'redis_command'],
  ] satisfies Array<[TicketStatus, TicketType]>)('%s %s 工單可一鍵重提到新建工單頁', async (status, ticketType) => {
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
      status,
      ticket_type: ticketType,
      sql_content: ticketType === 'redis_command' ? 'SET user:1 active' : 'UPDATE users SET flagged = 1 WHERE id < 10;',
    }))

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: 'Resubmit' }))
    expect(screen.getByText('new ticket page')).toBeInTheDocument()
  })

  it('非一般工單即使 rejected 也不顯示一鍵重提', async () => {
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
      status: 'rejected',
      ticket_type: 'sql_export',
      sql_content: 'SELECT * FROM users;',
    }))

    renderPage()

    await waitFor(() => expect(screen.getByText('SQL Content')).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: 'Resubmit' })).not.toBeInTheDocument()
  })

  it('工單詳情 SQL 內容會以格式化後的形式顯示', async () => {
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
      sql_content: 'select id,name from users where id < 10 order by id',
    }))

    renderPage()

    const sqlLabel = await screen.findByText('SQL Content')
    const sqlBlock = sqlLabel.parentElement?.querySelector('pre')
    expect(sqlBlock?.textContent).toContain('SELECT\n  id,')
    expect(sqlBlock?.textContent).toContain('FROM\n  users')
    expect(sqlBlock?.textContent).toContain('ORDER BY\n  id')
  })

  it('非 admin 使用者不顯示 workflow resolution debug trace', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: { id: 1, username: 'dev', authGroups: ['developer'], authGroupDetails: [], permissions: ['tickets.apply'], dbConnectionIds: [], protected: false, isActive: true },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedGetTicket.mockResolvedValue(buildDetail(baseTicket, {
      workflow_resolution_trace: {
        workflow_rule_id: 15,
        workflow_rule_name: 'Global DDL',
        approval_enabled: true,
        approval_user_ids: [2],
        executor_user_ids: [3],
        admin_user_ids: [],
        error_code: '',
        error_message: '',
        resolved_at: '2026-06-23T13:24:45Z',
        resolution_trace: {
          missing_approval_groups: ['data_owner'],
          missing_executor_groups: [],
        },
      },
    }))

    renderPage()

    await waitFor(() => expect(screen.getByText('SQL Content')).toBeInTheDocument())
    expect(screen.queryByText('Debug / Resolution Trace')).not.toBeInTheDocument()
    expect(screen.queryByText('Workflow Resolution Trace')).not.toBeInTheDocument()
    expect(screen.queryByText('Global DDL')).not.toBeInTheDocument()
  })

  it('admin 可在底部展開可讀的 workflow resolution debug trace', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: { id: 2, username: 'admin_sre', authGroups: ['admin'], authGroupDetails: [], permissions: ['tickets.review'], dbConnectionIds: [], protected: false, isActive: true },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedGetTicket.mockResolvedValue(buildDetail({ ...baseTicket, status: 'needs_admin_attention' }, {
      workflow_participants: {
        reviewers: ['reviewer.bob'],
        executors: ['dba.cindy'],
      },
      workflow_resolution_trace: {
        workflow_rule_id: 15,
        workflow_rule_name: 'Global DDL',
        approval_enabled: true,
        approval_user_ids: [2],
        executor_user_ids: [3],
        admin_user_ids: [4],
        approval_users: [{ id: 2, username: 'reviewer.bob' }],
        executor_users: [{ id: 3, username: 'dba.cindy' }],
        admin_users: [{ id: 4, username: 'admin.sre' }],
        missing_approval_groups: [{ group_key: 'data_owner', name: 'Data Owner' }],
        missing_executor_groups: [],
        error_code: '',
        error_message: '',
        resolved_at: '2026-06-23T13:24:45Z',
        resolution_trace: {
          missing_approval_groups: ['data_owner'],
          missing_executor_groups: [],
        },
      },
    }))

    renderPage()

    const toggle = await screen.findByRole('button', { name: /Debug \/ Resolution Trace/i })
    expect(screen.queryByText('Global DDL')).not.toBeInTheDocument()

    fireEvent.click(toggle)

    expect(screen.getByText('Global DDL')).toBeInTheDocument()
    expect(screen.getAllByText('reviewer.bob').length).toBeGreaterThan(0)
    expect(screen.getAllByText('dba.cindy').length).toBeGreaterThan(0)
    expect(screen.getByText('admin.sre (#4)')).toBeInTheDocument()
    expect(screen.getByText('Approval: Data Owner (data_owner)')).toBeInTheDocument()
    expect(screen.getByText('Raw resolution trace')).toBeInTheDocument()
    expect(screen.getByText(/2 \(reviewer\.bob\)/)).toBeInTheDocument()
    expect(screen.getByText(/3 \(analytics-primary\)/)).toBeInTheDocument()
    expect(screen.queryByText('Workflow Resolution Trace')).not.toBeInTheDocument()
  })

  it('admin 查看非 needs_admin_attention 工單時不顯示 workflow resolution debug trace', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: { id: 2, username: 'admin_sre', authGroups: ['admin'], authGroupDetails: [], permissions: ['tickets.review'], dbConnectionIds: [], protected: false, isActive: true },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedGetTicket.mockResolvedValue(buildDetail(baseTicket, {
      workflow_resolution_trace: {
        workflow_rule_id: 15,
        workflow_rule_name: 'Global DDL',
        approval_enabled: true,
        approval_user_ids: [2],
        executor_user_ids: [3],
        admin_user_ids: [4],
        error_code: '',
        error_message: '',
        resolved_at: '2026-06-23T13:24:45Z',
        resolution_trace: {
          missing_approval_groups: ['data_owner'],
          missing_executor_groups: [],
        },
      },
    }))

    renderPage()

    await waitFor(() => expect(screen.getByText('SQL Content')).toBeInTheDocument())
    expect(screen.queryByText('Debug / Resolution Trace')).not.toBeInTheDocument()
    expect(screen.queryByText('Global DDL')).not.toBeInTheDocument()
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
        id: 123,
        status: 'ready',
        expires_at: '2026-06-12T01:00:00Z',
        downloaded_at: null,
        download_url: '/api/exports/123/download',
      },
      capabilities: {
        can_review: false,
        can_reject: false,
        can_withdraw: false,
        can_revoke: false,
        can_execute: false,
        can_download_export: true,
      },
    }))

    renderPage()

    await waitFor(() => expect(screen.getByRole('button', { name: 'Download Export' })).toBeInTheDocument())
    expect(screen.queryByText('Export Download')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Download Export' })).toBeInTheDocument()
  })

  it('敏感 SQL export scope 只顯示命中的敏感欄位', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: { id: 2, username: 'reviewer', authGroups: ['reviewer'], authGroupDetails: [], permissions: ['tickets.review'], dbConnectionIds: [], protected: false, isActive: true },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedGetTicket.mockResolvedValue(buildDetail({
      ...baseTicket,
      ticket_type: 'sql_export',
      sql_content: 'SELECT * FROM users;',
    }, {
      scopes: [
        {
          id: 1,
          ticket_id: 12,
          connection_id: 3,
          database_name: 'nacos',
          schema_name: null,
          table_name: 'accounts',
          column_name: 'phone_number',
          is_sensitive: true,
          source_kind: 'QUERY_COLUMN',
          created_at: '2026-06-09T10:00:00Z',
        },
      ],
    }))

    renderPage()

    await waitFor(() => expect(screen.getByText('Scopes')).toBeInTheDocument())
    expect(screen.getByText('phone_number')).toBeInTheDocument()
    expect(screen.getByText('Sensitive column')).toBeInTheDocument()
    expect(screen.queryByText('nacos')).not.toBeInTheDocument()
    expect(screen.queryByText('accounts')).not.toBeInTheDocument()
    expect(screen.queryByText('QUERY_COLUMN')).not.toBeInTheDocument()
  })

  it('敏感查詢查看 scope 只顯示命中的敏感欄位', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: { id: 2, username: 'reviewer', authGroups: ['reviewer'], authGroupDetails: [], permissions: ['sql_editor.sensitive_review'], dbConnectionIds: [], protected: false, isActive: true },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedGetTicket.mockResolvedValue(buildDetail({
      ...baseTicket,
      ticket_type: 'sensitive_query_access',
      sql_content: 'SELECT * FROM users;',
    }, {
      scopes: [
        {
          id: 1,
          ticket_id: 12,
          connection_id: 3,
          database_name: 'nacos',
          schema_name: null,
          table_name: 'users',
          column_name: 'username',
          is_sensitive: true,
          source_kind: 'QUERY_COLUMN',
          created_at: '2026-06-09T10:00:00Z',
        },
      ],
    }))

    renderPage()

    await waitFor(() => expect(screen.getByText('Scopes')).toBeInTheDocument())
    expect(screen.getByText('username')).toBeInTheDocument()
    expect(screen.getByText('Sensitive column')).toBeInTheDocument()
    expect(screen.queryByText('nacos')).not.toBeInTheDocument()
    expect(screen.queryByText('users')).not.toBeInTheDocument()
    expect(screen.queryByText('QUERY_COLUMN')).not.toBeInTheDocument()
  })

  it('sensitive access 在 stopped 狀態時，approval flow 不應顯示等待審批完成', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: { id: 2, username: 'admin', authGroups: ['admin'], authGroupDetails: [], permissions: ['sql_editor.sensitive_review'], dbConnectionIds: [], protected: false, isActive: true },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedGetTicket.mockResolvedValue(buildDetail({
      ...baseTicket,
      ticket_type: 'sensitive_query_access',
      status: 'stopped',
      sql_content: 'SELECT * FROM users;',
    }, {
      capabilities: {
        can_review: false,
        can_reject: false,
        can_withdraw: false,
        can_revoke: false,
        can_execute: false,
        can_download_export: false,
      },
    }))

    renderPage()

    expect(await screen.findByText('Approval Flow')).toBeInTheDocument()
    expect(screen.getByText('Approval outcome')).toBeInTheDocument()
    expect(screen.queryByText('Sensitive access was revoked and the ticket is closed')).not.toBeInTheDocument()
    expect(screen.queryByText('Waiting for approval to complete the request')).not.toBeInTheDocument()
  })

  it('approved sensitive access 的 revoke 按鈕保持一般 action button 尺寸', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: { id: 2, username: 'admin', authGroups: ['admin'], authGroupDetails: [], permissions: ['sql_editor.sensitive_review'], dbConnectionIds: [], protected: false, isActive: true },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedGetTicket.mockResolvedValue(buildDetail({
      ...baseTicket,
      ticket_type: 'sensitive_query_access',
      status: 'approved',
      sql_content: 'SELECT * FROM users;',
    }, {
      capabilities: {
        can_review: false,
        can_reject: true,
        can_withdraw: false,
        can_revoke: true,
        can_execute: false,
        can_download_export: false,
      },
    }))

    renderPage()

    const revokeButton = await screen.findByRole('button', { name: 'Revoke Access' })
    expect(revokeButton).toHaveClass('h-9')
    expect(revokeButton).toHaveClass('w-auto')
    expect(revokeButton).toHaveClass('self-start')
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
    expect(screen.getByText('analytics_app')).toBeInTheDocument()
    expect(screen.getAllByText('alice').length).toBeGreaterThan(0)
    expect(screen.getAllByText('reviewer.bob').length).toBeGreaterThan(0)
    expect(screen.getAllByText('dba.cindy').length).toBeGreaterThan(0)
    expect(screen.queryByText('ops.dan')).not.toBeInTheDocument()
    expect(screen.queryByText(/^3$/)).not.toBeInTheDocument()
  })

  it('顯示逐句 SQL review 結果', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: { id: 1, username: 'dev', authGroups: ['developer'], authGroupDetails: [], permissions: ['tickets.apply'], dbConnectionIds: [], protected: false, isActive: true },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedGetTicket.mockResolvedValue(buildDetail(baseTicket, {
      review_results: [
        {
          id: 1,
          ticket_id: 12,
          seq: 1,
          sql_stmt: 'UPDATE users SET flagged = 1 WHERE id < 10',
          phase: 'validation',
          scan_rows: 10,
          status: 'pass',
          message: null,
        },
      ],
    }))

    renderPage()

    expect(await screen.findByText('Statement Results')).toBeInTheDocument()
    expect(screen.getByText('UPDATE users SET flagged = 1 WHERE id < 10')).toBeInTheDocument()
    expect(screen.getByText('10')).toBeInTheDocument()
    expect(screen.getByText('pass')).toBeInTheDocument()
  })

  it('DDL 工單詳情顯示表行數與大小且隱藏 Scan Rows', async () => {
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
      ticket_type: 'ddl',
      sql_content: 'ALTER TABLE orders ADD COLUMN note VARCHAR(255);',
    }, {
      review_results: [
        {
          id: 1,
          ticket_id: 12,
          seq: 1,
          sql_stmt: 'ALTER TABLE orders ADD COLUMN note VARCHAR(255);',
          phase: 'validation',
          tables: [{ table_name: 'orders', row_count: 123456, data_size_bytes: 2147483648 }],
          scan_rows: 0,
          status: 'pass',
          message: null,
        },
      ],
    }))

    renderPage()

    expect(await screen.findByText('Statement Results')).toBeInTheDocument()
    expect(screen.getByText('Table Rows')).toBeInTheDocument()
    expect(screen.getByText('Table Size')).toBeInTheDocument()
    expect(screen.getByText('123,456')).toBeInTheDocument()
    expect(screen.getByText('2.00 GB')).toBeInTheDocument()
    expect(screen.queryByText('Scan Rows')).not.toBeInTheDocument()
    expect(screen.queryByText('Rows Affected')).not.toBeInTheDocument()
  })

  it('可一次展開與收合所有長 SQL，且逐列 SQL 仍可獨立控制', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'authenticated',
      isAuthenticated: true,
      user: { id: 1, username: 'dev', authGroups: ['developer'], authGroupDetails: [], permissions: ['tickets.apply'], dbConnectionIds: [], protected: false, isActive: true },
      accessToken: 'token',
      login: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })
    mockedGetTicket.mockResolvedValue(buildDetail(baseTicket, {
      review_results: [
        {
          id: 1,
          ticket_id: 12,
          seq: 1,
          sql_stmt: 'CREATE TABLE IF NOT EXISTS very_long_table_one (id BIGINT AUTO_INCREMENT PRIMARY KEY, user_id BIGINT NOT NULL, encrypted_secret VARCHAR(4096) NOT NULL, created_at BIGINT NOT NULL, updated_at BIGINT NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;',
          phase: 'validation',
          scan_rows: 0,
          status: 'pass',
          message: null,
        },
        {
          id: 2,
          ticket_id: 12,
          seq: 2,
          sql_stmt: 'CREATE TABLE IF NOT EXISTS very_long_table_two (id BIGINT AUTO_INCREMENT PRIMARY KEY, action VARCHAR(64) NOT NULL, operator_id BIGINT DEFAULT NULL, request_payload VARCHAR(4096) NOT NULL, created_at BIGINT NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;',
          phase: 'validation',
          scan_rows: 0,
          status: 'pass',
          message: null,
        },
      ],
    }))

    renderPage()

    const showAll = await screen.findByRole('button', { name: /Show all SQL/i })
    expect(screen.getAllByRole('button', { name: /Show full SQL/i })).toHaveLength(2)

    fireEvent.click(showAll)

    expect(screen.getByRole('button', { name: /Collapse all SQL/i })).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: /Collapse SQL/i })).toHaveLength(2)

    fireEvent.click(screen.getAllByRole('button', { name: /Collapse SQL/i })[0])

    expect(screen.getByRole('button', { name: /Show all SQL/i })).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: /Show full SQL/i })).toHaveLength(1)
    expect(screen.getAllByRole('button', { name: /Collapse SQL/i })).toHaveLength(1)
  })

  it('顯示關鍵工單資訊與逐句 SQL 執行結果', async () => {
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
      status: 'completed',
      reviewer_id: 2,
      reviewer_name: 'reviewer.bob',
      executor_id: 3,
      executor_name: 'dba.cindy',
    }, {
      executions: [
        {
          id: 21,
          ticket_id: 12,
          seq: 1,
          sql_stmt: 'UPDATE users SET flagged = 1 WHERE id < 10;',
          status: 'completed',
          rows_affected: 9,
          error_msg: null,
          started_at: '2026-06-09T10:00:00.000Z',
          completed_at: '2026-06-09T10:00:01.250Z',
        },
      ],
    }))

    renderPage()

    expect(await screen.findByText('Overview')).toBeInTheDocument()
    expect(screen.getByText('analytics-primary')).toBeInTheDocument()
    expect(screen.getByText('analytics_app')).toBeInTheDocument()
    expect(screen.getAllByText('dba.cindy').length).toBeGreaterThan(0)
    expect(screen.getByText('Statement Results')).toBeInTheDocument()
    expect(screen.getAllByText('Completed').length).toBeGreaterThan(0)
    expect(screen.getByText('1.250s')).toBeInTheDocument()
  })

  it('rejected 工單不把未執行 statement 顯示成 Pending Execution', async () => {
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
      status: 'rejected',
      rejection_reason: 'reject at execution stage',
    }, {
      executions: [
        {
          id: 21,
          ticket_id: 12,
          seq: 1,
          sql_stmt: 'CREATE TABLE test_i (id int);',
          status: 'pending',
          rows_affected: 0,
          error_msg: null,
          started_at: null,
          completed_at: null,
        },
      ],
    }))

    renderPage()

    expect(await screen.findByText('Statement Results')).toBeInTheDocument()
    expect(screen.queryByText('Pending Execution')).not.toBeInTheDocument()
  })

  it('執行階段 reject 的 Approval Flow 會顯示 review completed 且 execution failed', async () => {
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
      ticket_type: 'ddl',
      status: 'rejected',
      reviewer_id: 2,
      reviewer_name: 'reviewer.bob',
      rejection_reason: 'reject at execution stage',
    }, {
      workflow_participants: {
        reviewers: ['reviewer.bob'],
        executors: ['dba.cindy'],
      },
      activity_logs: [
        {
          id: 1,
          actor_id: 2,
          actor_name: 'reviewer.bob',
          action_type: 'ticket_approve',
          resource_type: 'ticket',
          resource_id: 12,
          details: null,
          ip_address: null,
          created_at: '2026-06-09T10:01:00Z',
        },
        {
          id: 2,
          actor_id: 3,
          actor_name: 'dba.cindy',
          action_type: 'ticket_reject',
          resource_type: 'ticket',
          resource_id: 12,
          details: { reason: 'reject at execution stage' },
          ip_address: null,
          created_at: '2026-06-09T10:02:00Z',
        },
      ],
    }))

    renderPage()

    expect(await screen.findByLabelText('Review: completed')).toBeInTheDocument()
    expect(screen.getByLabelText('Execution: failed')).toBeInTheDocument()
  })
})
