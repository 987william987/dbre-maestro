import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AuditLogsPage } from '@/modules/audit/pages/AuditLogsPage'
import { ToastProvider } from '@/shared/ui/ToastContext'

vi.mock('@/modules/audit/api', () => ({
  listAuditLogs: vi.fn(),
  exportAuditLogs: vi.fn(),
}))

vi.mock('@/shared/auth/AuthContext', () => ({
  useAuth: () => ({
    user: { permissions: ['audit_logs.write'] },
  }),
}))

import { exportAuditLogs, listAuditLogs } from '@/modules/audit/api'

const mockedListAuditLogs = vi.mocked(listAuditLogs)
const mockedExportAuditLogs = vi.mocked(exportAuditLogs)

describe('AuditLogsPage', () => {
  beforeEach(() => {
    mockedListAuditLogs.mockReset()
  })

  it('renders audit logs with english labels and details', async () => {
    mockedListAuditLogs.mockResolvedValue({
      logs: [
        {
          id: 1,
          actor_name: '',
          actor_id: null,
          action_type: 'setting_change',
          resource_type: null,
          resource_id: 18,
          ip_address: null,
          details: { changed: 'db_metadata_inventory_enabled' },
          created_at: '2026-06-13T00:17:55Z',
        },
      ],
      total: 1,
      limit: 20,
      offset: 0,
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <AuditLogsPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByPlaceholderText('Actor')).toBeInTheDocument()
    expect(screen.getByText('Unspecified Resource · 18')).toBeInTheDocument()
    expect(screen.getByText('System Event')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'View' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'View' }))

    await waitFor(() => {
      expect(screen.getByText('Raw Details')).toBeInTheDocument()
      expect(screen.getByText('Summary')).toBeInTheDocument()
      expect(screen.getByText('Timestamp')).toBeInTheDocument()
      expect(screen.getByText('Source IP')).toBeInTheDocument()
    })
  })

  it('maps visible resource labels to resource_type filters', async () => {
    mockedListAuditLogs.mockResolvedValue({
      logs: [],
      total: 0,
      limit: 20,
      offset: 0,
    })

    render(
      <MemoryRouter>
        <ToastProvider>
          <AuditLogsPage />
        </ToastProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByPlaceholderText('Resource')).toBeInTheDocument()

    fireEvent.change(screen.getByPlaceholderText('Resource'), { target: { value: 'DB Connection' } })

    await waitFor(() => expect(mockedListAuditLogs).toHaveBeenLastCalledWith(expect.objectContaining({
      resourceType: 'db_connection',
      resourceName: '',
    })))
  })

  it('顯示 audit log 載入失敗', async () => {
    mockedListAuditLogs.mockRejectedValue(new Error('offline'))
    render(<MemoryRouter><ToastProvider><AuditLogsPage /></ToastProvider></MemoryRouter>)
    expect(await screen.findByText('Failed to load audit logs.')).toBeInTheDocument()
  })

  it('export 會下載後端回傳的 CSV 檔名', async () => {
    mockedListAuditLogs.mockResolvedValue({ logs: [], total: 0, limit: 20, offset: 0 })
    mockedExportAuditLogs.mockResolvedValue(new Response('id,action', { headers: { 'content-disposition': 'attachment; filename="audit.csv"' } }))
    const createObjectURL = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:audit')
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined)
    render(<MemoryRouter><ToastProvider><AuditLogsPage /></ToastProvider></MemoryRouter>)
    fireEvent.click(await screen.findByRole('button', { name: 'Export' }))
    await waitFor(() => expect(mockedExportAuditLogs).toHaveBeenCalled())
    expect(createObjectURL).toHaveBeenCalled()
    expect(click).toHaveBeenCalled()
  })
})
