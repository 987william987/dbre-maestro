import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DashboardPage } from './DashboardPage'
import { getDashboard, type DashboardResponse } from '@/modules/dashboard/api'

vi.mock('@/modules/dashboard/api', async () => {
  const actual = await vi.importActual<typeof import('@/modules/dashboard/api')>('@/modules/dashboard/api')
  return { ...actual, getDashboard: vi.fn() }
})
vi.mock('@/modules/dashboard/components/OperationsTrendChart', () => ({
  OperationsTrendChart: () => <div>Operations trend</div>,
}))

const mockedGetDashboard = vi.mocked(getDashboard)
const personalDashboard = {
  personal: {
    ticket_summary: { total: 3, completed: 1, failed: 1, active: 1, by_type: [], by_status: [] },
    active_tickets: [],
    recent_tickets: [],
    db_scopes: [{ id: 1, name: 'Primary MySQL', db_type: 'mysql' }],
    query_access_scopes: [],
  },
  platform: null,
} satisfies DashboardResponse

describe('DashboardPage', () => {
  beforeEach(() => vi.clearAllMocks())

  it('顯示個人 dashboard，且沒有平台資料時不渲染平台區塊', async () => {
    mockedGetDashboard.mockResolvedValue(personalDashboard)
    render(<MemoryRouter><DashboardPage /></MemoryRouter>)

    expect(await screen.findByRole('heading', { name: 'Dashboard' })).toBeInTheDocument()
    expect(screen.getByText('Primary MySQL')).toBeInTheDocument()
    expect(screen.queryByText('Platform Operations')).not.toBeInTheDocument()
  })

  it('dashboard 載入失敗時顯示可理解的錯誤', async () => {
    mockedGetDashboard.mockRejectedValue(new Error('offline'))
    render(<MemoryRouter><DashboardPage /></MemoryRouter>)

    expect(await screen.findByText('Failed to load dashboard.')).toBeInTheDocument()
  })

  it('有平台資料時顯示營運區塊', async () => {
    mockedGetDashboard.mockResolvedValue({
      ...personalDashboard,
      platform: {
        ticket_summary: { total: 8, completed: 5, failed: 1, active: 2, by_type: [], by_status: [] },
        queue: { pending_review: 1, pending_execution: 1, executing: 0, needs_admin_attention: 0, failed_today: 0, failed_7d: 1, long_pending: 0 },
        aging: {},
        execution_risk: { recent_failed: 0, manually_stopped: 0, service_shutdown: 0, outcome_unknown: 0, not_sent: 0, db_explicit_error: 0, by_outcome: [], by_interruption: [] },
        access_governance: { expiring_soon: 0, long_lived: 0, never_expires: 0, recently_revoked: 0, sensitive_requests_7d: 0, sql_export_requests_7d: 0, active_rules: 0 },
        db_metadata_health: { db_type_counts: [], enabled_metadata_connection_count: 0, object_snapshot_connection_count: 0, stale_object_connection_count: 0, object_count: 0, object_sync_failed: false },
        notification_health: { lark_failed_7d: 0, interactive_callback_failed_7d: 0, retry_or_failure_7d: 0, missing_lark_recipient_7d: 0, recipient_conflict_7d: 0, by_type: [] },
        top_usage: { submitters: [], db_connections_by_tickets: [], failed_db_connections: [], sql_exports_by_user: [] },
        operations_trend: { timezone: 'Asia/Taipei', start_date: '2026-01-01', end_date: '2026-01-02', points: [] },
        recent_attention: [], long_pending_tickets: [], recent_failed_tickets: [], db_connection_failures: [],
      },
    })
    render(<MemoryRouter><DashboardPage /></MemoryRouter>)
    expect(await screen.findByText('Platform Operations')).toBeInTheDocument()
    expect(screen.getByText('Operations trend')).toBeInTheDocument()
  })
})
