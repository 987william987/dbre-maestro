import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AuthProvider, useAuth } from './AuthContext'

// A second tab (e.g. one opened from a Lark ticket link) can race an
// already-open tab to rotate the shared refresh cookie. The loser gets a
// benign "stale refresh token" 401 and should retry once, picking up the
// winner's now-current cookie, instead of forcing a real logout.

function Probe() {
  const { status, accessToken } = useAuth()
  return (
    <div>
      <span data-testid="status">{status}</span>
      <span data-testid="token">{accessToken ?? ''}</span>
    </div>
  )
}

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('AuthContext refreshAccessToken — 多分頁 refresh token 競態', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('收到 stale refresh token 時會重試一次，成功後正常登入，不會被登出', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/api/auth/refresh')) {
        if (fetchMock.mock.calls.length === 1) {
          return jsonResponse(401, { error: 'stale refresh token' })
        }
        return jsonResponse(200, { access_token: 'fresh-token-from-other-tab' })
      }
      if (path.endsWith('/api/auth/me')) {
        return jsonResponse(200, { id: 1, username: 'alice', auth_groups: [], permissions: [] })
      }
      return jsonResponse(404, { error: 'not found' })
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    )

    // Let the first (failing) refresh attempt resolve, then advance past
    // the retry delay so the second attempt fires.
    await vi.waitFor(() => {
      expect(fetchMock.mock.calls.filter((c) => String(c[0]).endsWith('/api/auth/refresh')).length).toBe(1)
    })
    await vi.advanceTimersByTimeAsync(500)

    await vi.waitFor(() => {
      expect(screen.getByTestId('status').textContent).toBe('authenticated')
    })
    expect(screen.getByTestId('token').textContent).toBe('fresh-token-from-other-tab')

    const refreshCalls = fetchMock.mock.calls.filter((c) => String(c[0]).endsWith('/api/auth/refresh'))
    expect(refreshCalls).toHaveLength(2)
  })

  it('收到其他原因的 401（例如真的偵測到 reuse）不會重試，直接登出', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/api/auth/refresh')) {
        return jsonResponse(401, { error: 'refresh token reuse detected' })
      }
      return jsonResponse(404, { error: 'not found' })
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    )

    await vi.waitFor(() => {
      expect(screen.getByTestId('status').textContent).toBe('anonymous')
    })

    // Give any (incorrect) retry a chance to fire before asserting it didn't.
    await vi.advanceTimersByTimeAsync(1000)

    const refreshCalls = fetchMock.mock.calls.filter((c) => String(c[0]).endsWith('/api/auth/refresh'))
    expect(refreshCalls).toHaveLength(1)
  })
})
