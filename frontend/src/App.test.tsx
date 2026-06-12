import { render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AppProviders } from '@/app/providers/AppProviders'
import App from '@/App'

const storage = new Map<string, string>()

function jsonResponse(body: unknown, init: ResponseInit = {}) {
  return new Response(JSON.stringify(body), {
    status: init.status ?? 200,
    headers: {
      'Content-Type': 'application/json',
      ...(init.headers ?? {}),
    },
  })
}

describe('App routing', () => {
  beforeEach(() => {
    storage.clear()
    window.history.replaceState({}, '', '/')
    vi.restoreAllMocks()

    Object.defineProperty(window, 'localStorage', {
      value: {
        getItem: (key: string) => storage.get(key) ?? null,
        setItem: (key: string, value: string) => {
          storage.set(key, value)
        },
        removeItem: (key: string) => {
          storage.delete(key)
        },
        clear: () => {
          storage.clear()
        },
      },
      configurable: true,
    })
  })

  it('以已登入狀態進入 /tickets 時，即使後端回傳 null 陣列也不會白屏', async () => {
    window.localStorage.setItem('dbre_maestro.access_token', 'test-token')
    window.history.replaceState({}, '', '/tickets')

    const fetchMock = vi.fn<typeof fetch>(async (input, init) => {
      const url = typeof input === 'string'
        ? input
        : input instanceof URL
          ? input.toString()
          : input.url

      if (url === '/api/auth/me') {
        expect(init?.headers).toMatchObject({
          Authorization: 'Bearer test-token',
        })

        return jsonResponse({
          id: 1,
          username: 'admin',
          auth_groups: null,
        })
      }

      if (url === '/api/tickets?limit=20&offset=0') {
        return jsonResponse({
          tickets: null,
          limit: 20,
          offset: 0,
        })
      }

      if (url.startsWith('/api/notifications')) {
        return jsonResponse({
          notifications: [],
          total: 0,
          unread: 0,
          limit: 8,
          offset: 0,
        })
      }

      throw new Error(`unexpected fetch: ${url}`)
    })

    vi.stubGlobal('fetch', fetchMock)

    render(
      <AppProviders>
        <App />
      </AppProviders>,
    )

    expect(await screen.findByText('Ticket Workspace')).toBeInTheDocument()
    expect(await screen.findByText('尚無歷史工單')).toBeInTheDocument()

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/tickets?limit=20&offset=0',
        expect.objectContaining({
          credentials: 'same-origin',
          headers: expect.any(Headers),
        }),
      )
    })
  })
})
