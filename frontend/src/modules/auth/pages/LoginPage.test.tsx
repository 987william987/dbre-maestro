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
    vi.unstubAllGlobals()
    mockedUseAuth.mockReset()
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

  it('setup 未完成時保留登入控制但全部禁用', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ setup_completed: false }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })))
    const login = vi.fn()

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

    await waitFor(() => expect(screen.getByRole('button', { name: 'Setup Wizard' })).toBeInTheDocument())

    expect(screen.getByPlaceholderText('e.g. admin')).toBeDisabled()
    expect(screen.getByPlaceholderText('Enter your password')).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Sign in' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Sign in with Lark' })).toBeDisabled()

    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(login).not.toHaveBeenCalled()
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

  it('OIDC SSO 啟用時顯示登入按鈕並導向 provider start URL', async () => {
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/auth/sso/providers') {
        return new Response(JSON.stringify({ providers: [{ display_name: 'Authentik', start_url: '/api/auth/sso/start' }] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      return new Response(JSON.stringify({ setup_completed: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }))
    const originalLocation = window.location
    const assign = vi.fn()
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { ...originalLocation, assign },
    })

    mockedUseAuth.mockReturnValue({
      status: 'anonymous',
      isAuthenticated: false,
      user: null,
      accessToken: null,
      login: vi.fn(),
      consumeSSOLogin: vi.fn(),
      verifyMFA: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })

    render(
      <MemoryRouter initialEntries={[{ pathname: '/login', state: { from: { pathname: '/tickets' } } }]}>
        <LoginPage />
      </MemoryRouter>,
    )

    const ssoButton = await screen.findByRole('button', { name: 'Sign in with Authentik' })
    fireEvent.click(ssoButton)

    expect(assign).toHaveBeenCalledWith('/api/auth/sso/start?returnTo=%2Ftickets')
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: originalLocation,
    })
  })

  it('OIDC SSO callback ticket consume 成功後導向回傳路由', async () => {
    const consumeSSOLogin = vi.fn().mockResolvedValue({ status: 'authenticated', returnTo: '/tickets' })

    mockedUseAuth.mockReturnValue({
      status: 'anonymous',
      isAuthenticated: false,
      user: null,
      accessToken: null,
      login: vi.fn(),
      consumeSSOLogin,
      verifyMFA: vi.fn(),
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })

    render(
      <MemoryRouter initialEntries={['/login?sso_ticket=ticket-123']}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/tickets" element={<div>tickets page</div>} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(consumeSSOLogin).toHaveBeenCalledWith('ticket-123'))
    await waitFor(() => expect(screen.getByText('tickets page')).toBeInTheDocument())
  })
})
