import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SetupWizard } from './SetupWizard'
import { getSetupStatus } from '@/shared/setup/api'

vi.mock('@/shared/setup/api', () => ({ getSetupStatus: vi.fn() }))
const mockedGetSetupStatus = vi.mocked(getSetupStatus)

function renderWizard() {
  return render(
    <MemoryRouter initialEntries={['/setup']}>
      <Routes>
        <Route path="/setup" element={<SetupWizard />} />
        <Route path="/login" element={<div>Login destination</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('SetupWizard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.unstubAllGlobals()
  })

  it('平台已完成設定時導向登入頁', async () => {
    mockedGetSetupStatus.mockResolvedValue({ setup_completed: true })
    renderWizard()
    expect(await screen.findByText('Login destination')).toBeInTheDocument()
  })

  it('阻擋無效表單，並顯示欄位驗證訊息', async () => {
    mockedGetSetupStatus.mockResolvedValue({ setup_completed: false })
    renderWizard()
    fireEvent.click(await screen.findByRole('button', { name: /Get started/ }))
    fireEvent.click(screen.getByRole('button', { name: /Create account/ }))
    expect(screen.getByText('Username is required')).toBeInTheDocument()
    expect(screen.getByText('Email is required')).toBeInTheDocument()
  })

  it('建立 admin 成功後顯示完成頁面', async () => {
    mockedGetSetupStatus.mockResolvedValue({ setup_completed: false })
    const fetchMock = vi.fn(async () => new Response('{}', { status: 201 }))
    vi.stubGlobal('fetch', fetchMock)
    renderWizard()
    fireEvent.click(await screen.findByRole('button', { name: /Get started/ }))
    fireEvent.change(screen.getByPlaceholderText('e.g. admin'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByPlaceholderText('you@company.com'), { target: { value: 'admin@example.com' } })
    fireEvent.change(screen.getByPlaceholderText('Min. 8 chars with upper, lower & number'), { target: { value: 'Admin1234' } })
    fireEvent.change(screen.getByPlaceholderText('Re-enter your password'), { target: { value: 'Admin1234' } })
    fireEvent.click(screen.getByRole('button', { name: /Create account/ }))

    expect(await screen.findByText('Setup complete!')).toBeInTheDocument()
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/setup', expect.objectContaining({ method: 'POST' })))
  })

  it('建立 admin API 失敗時停留在表單並顯示錯誤', async () => {
    mockedGetSetupStatus.mockResolvedValue({ setup_completed: false })
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: 'Username already exists' }), { status: 400 })))
    renderWizard()
    fireEvent.click(await screen.findByRole('button', { name: /Get started/ }))
    fireEvent.change(screen.getByPlaceholderText('e.g. admin'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByPlaceholderText('you@company.com'), { target: { value: 'admin@example.com' } })
    fireEvent.change(screen.getByPlaceholderText('Min. 8 chars with upper, lower & number'), { target: { value: 'Admin1234' } })
    fireEvent.change(screen.getByPlaceholderText('Re-enter your password'), { target: { value: 'Admin1234' } })
    fireEvent.click(screen.getByRole('button', { name: /Create account/ }))
    expect(await screen.findByText('Username already exists')).toBeInTheDocument()
    expect(screen.queryByText('Setup complete!')).not.toBeInTheDocument()
  })
})
