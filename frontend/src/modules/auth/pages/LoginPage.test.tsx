import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { LoginPage } from '@/modules/auth/pages/LoginPage'

vi.mock('@/shared/auth/AuthContext', () => ({
  useAuth: vi.fn(),
}))

import { useAuth } from '@/shared/auth/AuthContext'

const mockedUseAuth = vi.mocked(useAuth)

describe('LoginPage', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ setup_completed: true }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })))
  })

  it('送出帳密後會呼叫 login 並導向首頁路由', async () => {
    const login = vi.fn().mockResolvedValue({ status: 'authenticated' })

    mockedUseAuth.mockReturnValue({
      status: 'anonymous',
      isAuthenticated: false,
      user: null,
      accessToken: null,
      login,
      verifyMFA: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })

    render(
      <MemoryRouter initialEntries={['/login']}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/" element={<div>home page</div>} />
        </Routes>
      </MemoryRouter>,
    )

    fireEvent.change(screen.getByPlaceholderText('e.g. admin'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByPlaceholderText('Enter your password'), { target: { value: 'Password1' } })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))

    await waitFor(() => expect(login).toHaveBeenCalledWith({ username: 'admin', password: 'Password1' }))
    await waitFor(() => expect(screen.getByText('home page')).toBeInTheDocument())
  })

  it('登入失敗時顯示錯誤訊息', async () => {
    const login = vi.fn().mockRejectedValue(new Error('invalid credentials'))

    mockedUseAuth.mockReturnValue({
      status: 'anonymous',
      isAuthenticated: false,
      user: null,
      accessToken: null,
      login,
      verifyMFA: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })

    render(
      <MemoryRouter>
        <LoginPage />
      </MemoryRouter>,
    )

    fireEvent.change(screen.getByPlaceholderText('e.g. admin'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByPlaceholderText('Enter your password'), { target: { value: 'Password1' } })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('invalid credentials'))
  })

  it('setup 已完成時不顯示 Setup Wizard 入口', async () => {
    mockedUseAuth.mockReturnValue({
      status: 'anonymous',
      isAuthenticated: false,
      user: null,
      accessToken: null,
      login: vi.fn(),
      verifyMFA: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })

    render(
      <MemoryRouter>
        <LoginPage />
      </MemoryRouter>,
    )

    await waitFor(() => {
      expect(screen.queryByRole('button', { name: 'Setup Wizard' })).not.toBeInTheDocument()
    })
  })

  it('高權限帳號首次登入時顯示 MFA 設定流程', async () => {
    const login = vi.fn().mockResolvedValue({
      status: 'mfa_required',
      mfaToken: 'mfa-token',
      setupRequired: true,
      mfaSecret: 'JBSWY3DPEHPK3PXP',
      qrDataURL: 'data:image/png;base64,test',
    })

    mockedUseAuth.mockReturnValue({
      status: 'anonymous',
      isAuthenticated: false,
      user: null,
      accessToken: null,
      login,
      verifyMFA: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })

    render(
      <MemoryRouter>
        <LoginPage />
      </MemoryRouter>,
    )

    fireEvent.change(screen.getByPlaceholderText('e.g. admin'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByPlaceholderText('Enter your password'), { target: { value: 'Password1' } })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByText('Set up MFA')).toBeInTheDocument()
    expect(screen.getByText('JBSWY3DPEHPK3PXP')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('000000')).toBeInTheDocument()
  })
})
