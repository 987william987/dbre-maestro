import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { LoginPage } from '@/modules/auth/pages/LoginPage'

vi.mock('@/shared/auth/AuthContext', () => ({
  useAuth: vi.fn(),
}))

import { useAuth } from '@/shared/auth/AuthContext'

const mockedUseAuth = vi.mocked(useAuth)

describe('LoginPage', () => {
  it('送出帳密後會呼叫 login 並導向 /tickets', async () => {
    const login = vi.fn().mockResolvedValue(undefined)

    mockedUseAuth.mockReturnValue({
      status: 'anonymous',
      isAuthenticated: false,
      user: null,
      accessToken: null,
      login,
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })

    render(
      <MemoryRouter initialEntries={['/login']}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/tickets" element={<div>tickets page</div>} />
        </Routes>
      </MemoryRouter>,
    )

    fireEvent.change(screen.getByPlaceholderText('e.g. admin'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByPlaceholderText('輸入你的密碼'), { target: { value: 'Password1' } })
    fireEvent.click(screen.getByRole('button', { name: '登入' }))

    await waitFor(() => expect(login).toHaveBeenCalledWith({ username: 'admin', password: 'Password1' }))
    await waitFor(() => expect(screen.getByText('tickets page')).toBeInTheDocument())
  })

  it('登入失敗時顯示錯誤訊息', async () => {
    const login = vi.fn().mockRejectedValue(new Error('invalid credentials'))

    mockedUseAuth.mockReturnValue({
      status: 'anonymous',
      isAuthenticated: false,
      user: null,
      accessToken: null,
      login,
      logout: vi.fn(),
      clearAuth: vi.fn(),
    })

    render(
      <MemoryRouter>
        <LoginPage />
      </MemoryRouter>,
    )

    fireEvent.change(screen.getByPlaceholderText('e.g. admin'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByPlaceholderText('輸入你的密碼'), { target: { value: 'Password1' } })
    fireEvent.click(screen.getByRole('button', { name: '登入' }))

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('invalid credentials'))
  })
})
