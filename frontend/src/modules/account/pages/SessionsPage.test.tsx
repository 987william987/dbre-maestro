import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { listAccountSessions, revokeAccountSession, revokeAllAccountSessions } from '@/modules/account/api'
import { SessionsPage } from './SessionsPage'
import { ToastProvider } from '@/shared/ui/ToastContext'

vi.mock('@/modules/account/api', () => ({
  listAccountSessions: vi.fn(),
  revokeAccountSession: vi.fn(),
  revokeAllAccountSessions: vi.fn(),
}))
const mockedList = vi.mocked(listAccountSessions)
const mockedRevoke = vi.mocked(revokeAccountSession)
const mockedRevokeAll = vi.mocked(revokeAllAccountSessions)

function renderPage() {
  return render(<ToastProvider><SessionsPage /></ToastProvider>)
}

describe('SessionsPage', () => {
  beforeEach(() => vi.clearAllMocks())

  it('顯示目前 session，且只能撤銷其他有效 session', async () => {
    mockedList.mockResolvedValue({ sessions: [
      { id: 1, user_id: 1, created_at: '2026-01-01T00:00:00Z', expires_at: '2099-01-01T00:00:00Z', is_current: true },
      { id: 2, user_id: 1, created_at: '2026-01-02T00:00:00Z', expires_at: '2099-01-01T00:00:00Z', is_current: false },
    ], current_session_id: 1 })
    mockedRevoke.mockResolvedValue(undefined)
    renderPage()

    expect(await screen.findByText('Session #1')).toBeInTheDocument()
    const revokeButtons = screen.getAllByRole('button', { name: 'Revoke' })
    expect(revokeButtons[0]).toBeDisabled()
    fireEvent.click(revokeButtons[1])
    await waitFor(() => expect(mockedRevoke).toHaveBeenCalledWith(2))
  })

  it('載入失敗時顯示錯誤狀態', async () => {
    mockedList.mockRejectedValue(new Error('offline'))
    renderPage()
    expect(await screen.findByText('Failed to load sessions.')).toBeInTheDocument()
  })

  it('全部撤銷失敗時保留頁面並顯示錯誤', async () => {
    mockedList.mockResolvedValue({ sessions: [
      { id: 1, user_id: 1, created_at: '2026-01-01T00:00:00Z', expires_at: '2099-01-01T00:00:00Z', is_current: true },
    ], current_session_id: 1 })
    mockedRevokeAll.mockRejectedValue(new Error('offline'))
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: 'Revoke All' }))
    expect(await screen.findByText('Failed to revoke sessions.')).toBeInTheDocument()
    expect(mockedRevokeAll).toHaveBeenCalledOnce()
  })
})
