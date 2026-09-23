import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ScheduledSQLReportsPage } from './ScheduledSQLReportsPage'
import * as reportsApi from '@/modules/scheduled-sql-reports/api'
import { useAuth } from '@/shared/auth/AuthContext'

vi.mock('@/modules/scheduled-sql-reports/api', () => ({
  listScheduledSQLReports: vi.fn(),
  getScheduledSQLReport: vi.fn(),
  listScheduledReportConnections: vi.fn(),
  listScheduledReportRecipients: vi.fn(),
  listScheduledReportMetadata: vi.fn(),
  createScheduledSQLReport: vi.fn(),
  updateScheduledSQLReport: vi.fn(),
  deleteScheduledSQLReport: vi.fn(),
}))
vi.mock('@/shared/auth/AuthContext', () => ({ useAuth: vi.fn() }))

const mockedUseAuth = vi.mocked(useAuth)
const mockedListReports = vi.mocked(reportsApi.listScheduledSQLReports)
const mockedListConnections = vi.mocked(reportsApi.listScheduledReportConnections)
const mockedListRecipients = vi.mocked(reportsApi.listScheduledReportRecipients)
const mockedListMetadata = vi.mocked(reportsApi.listScheduledReportMetadata)
const mockedCreateReport = vi.mocked(reportsApi.createScheduledSQLReport)

const savedReport: reportsApi.ScheduledSQLReport = {
  id: 1, name: 'Daily billing', description: '', db_connection_id: 3, database_name: 'billing', schema_name: '',
  sql_content: 'SELECT 1', cron_expression: '0 9 * * *', timezone: 'Asia/Taipei', recipient_user_ids: [],
  is_active: true, created_by: 1, updated_by: 1, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
}

function setPermissions(permissions: string[]) {
  mockedUseAuth.mockReturnValue({ user: { permissions } } as ReturnType<typeof useAuth>)
}

function selectOption(label: string, option: string) {
  fireEvent.click(screen.getByRole('button', { name: label }))
  fireEvent.click(screen.getByRole('option', { name: option }))
}

describe('ScheduledSQLReportsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setPermissions(['scheduled_sql_reports.read', 'scheduled_sql_reports.write'])
    mockedListReports.mockResolvedValue([])
    mockedListConnections.mockResolvedValue([{
      id: 3, name: 'Billing MySQL', db_type: 'mysql', host: 'db.local', port: 3306, database_name: '', username: 'reader',
      encryption_key_version: 1, ssl_mode: 'prefer', extra_params: null, created_by: 1,
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    }])
    mockedListRecipients.mockResolvedValue([])
    mockedListMetadata.mockResolvedValue({ db_type: 'mysql', level: 'database', items: [{ kind: 'database', name: 'billing' }] })
    mockedCreateReport.mockResolvedValue(savedReport)
  })

  it('read-only 使用者可以查看，但不能操作建立表單', async () => {
    setPermissions(['scheduled_sql_reports.read'])
    render(<ScheduledSQLReportsPage />)

    expect(await screen.findByText('Create Report')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Save Report' })).not.toBeInTheDocument()
    expect(screen.getByText('Create Report').closest('form')?.querySelector('fieldset')).toBeDisabled()
  })

  it('初始資料載入失敗時顯示錯誤', async () => {
    mockedListReports.mockRejectedValue(new Error('offline'))
    render(<ScheduledSQLReportsPage />)
    expect(await screen.findByText('Failed to load scheduled SQL reports.')).toBeInTheDocument()
  })

  it('建立報表時送出選定的 connection、database 與 SQL', async () => {
    render(<ScheduledSQLReportsPage />)
    expect(await screen.findByText('Create Report')).toBeInTheDocument()

    const form = screen.getByText('Create Report').closest('form')
    const inputs = form?.querySelectorAll('input')
    const textareas = form?.querySelectorAll('textarea')
    if (!inputs || !textareas) throw new Error('report form controls not found')
    fireEvent.change(inputs[1], { target: { value: 'Daily billing' } })
    selectOption('DB Connection', 'Billing MySQL (mysql)')
    await waitFor(() => expect(mockedListMetadata).toHaveBeenCalledWith(3))
    selectOption('Database', 'billing')
    fireEvent.change(textareas[0], { target: { value: 'SELECT 1' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save Report' }))

    await waitFor(() => expect(mockedCreateReport).toHaveBeenCalledWith(expect.objectContaining({
      name: 'Daily billing', db_connection_id: 3, database_name: 'billing', sql_content: 'SELECT 1',
    })))
    expect(await screen.findByText('Scheduled SQL report saved.')).toBeInTheDocument()
  })

  it('建立報表失敗時保留表單並顯示錯誤', async () => {
    mockedCreateReport.mockRejectedValue(new Error('save failed'))
    render(<ScheduledSQLReportsPage />)
    expect(await screen.findByText('Create Report')).toBeInTheDocument()
    const form = screen.getByText('Create Report').closest('form')
    const inputs = form?.querySelectorAll('input')
    const textareas = form?.querySelectorAll('textarea')
    if (!inputs || !textareas) throw new Error('report form controls not found')
    fireEvent.change(inputs[1], { target: { value: 'Daily billing' } })
    selectOption('DB Connection', 'Billing MySQL (mysql)')
    await waitFor(() => expect(mockedListMetadata).toHaveBeenCalledWith(3))
    selectOption('Database', 'billing')
    fireEvent.change(textareas[0], { target: { value: 'SELECT 1' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save Report' }))
    expect(await screen.findByText('Failed to save scheduled SQL report.')).toBeInTheDocument()
    expect(screen.getByDisplayValue('Daily billing')).toBeInTheDocument()
  })
})
