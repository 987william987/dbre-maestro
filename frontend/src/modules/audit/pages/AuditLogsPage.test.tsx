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

import { listAuditLogs } from '@/modules/audit/api'

const mockedListAuditLogs = vi.mocked(listAuditLogs)

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

    expect(await screen.findByRole('heading', { name: 'Audit Logs' })).toBeInTheDocument()
    expect(screen.getByPlaceholderText('Actor')).toBeInTheDocument()
    expect(screen.getByText('Unspecified Resource · 18')).toBeInTheDocument()
    expect(screen.getByText('System Event')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'View' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'View' }))

    await waitFor(() => {
      expect(screen.getByText('Full Details')).toBeInTheDocument()
      expect(screen.getByText('Timestamp')).toBeInTheDocument()
      expect(screen.getByText('Source IP')).toBeInTheDocument()
    })
  })
})
