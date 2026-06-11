import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiClient, configureApiClient } from '@/shared/api/client'

describe('apiClient', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    configureApiClient({
      getAccessToken: () => null,
      refreshAccessToken: async () => null,
      handleAuthFailure: () => undefined,
    })
  })

  it('在 401 後 refresh 成功會重試原請求', async () => {
    let token: string | null = 'old-token'

    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ error: 'unauthorized' }), { status: 401, headers: { 'Content-Type': 'application/json' } }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } }))

    vi.stubGlobal('fetch', fetchMock)

    configureApiClient({
      getAccessToken: () => token,
      refreshAccessToken: async () => {
        token = 'new-token'
        return token
      },
      handleAuthFailure: vi.fn(),
    })

    const result = await apiClient.get<{ ok: boolean }>('/tickets')

    expect(result).toEqual({ ok: true })
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/tickets')
    expect(fetchMock.mock.calls[1]?.[0]).toBe('/api/tickets')
    expect(fetchMock.mock.calls[0]?.[1]).toMatchObject({
      headers: expect.any(Headers),
    })
    expect((fetchMock.mock.calls[1]?.[1] as RequestInit).headers).toBeInstanceOf(Headers)
    expect(((fetchMock.mock.calls[1]?.[1] as RequestInit).headers as Headers).get('Authorization')).toBe('Bearer new-token')
  })

  it('refresh 成功後即使 state 尚未同步，也會用 refresh 回傳的新 token 重試', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ error: 'unauthorized' }), { status: 401, headers: { 'Content-Type': 'application/json' } }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } }))

    vi.stubGlobal('fetch', fetchMock)

    configureApiClient({
      getAccessToken: () => 'stale-token',
      refreshAccessToken: async () => 'fresh-token',
      handleAuthFailure: vi.fn(),
    })

    const result = await apiClient.post<{ ok: boolean }>('/db-connections', { name: 'cache-redis' })

    expect(result).toEqual({ ok: true })
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/db-connections')
    expect(fetchMock.mock.calls[1]?.[0]).toBe('/api/db-connections')
    expect(((fetchMock.mock.calls[0]?.[1] as RequestInit).headers as Headers).get('Authorization')).toBe('Bearer stale-token')
    expect(((fetchMock.mock.calls[1]?.[1] as RequestInit).headers as Headers).get('Authorization')).toBe('Bearer fresh-token')
  })

  it('refresh 失敗時會呼叫 auth failure handler', async () => {
    const handleAuthFailure = vi.fn()

    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: 'unauthorized' }), {
          status: 401,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )

    configureApiClient({
      getAccessToken: () => 'expired-token',
      refreshAccessToken: async () => null,
      handleAuthFailure,
    })

    await expect(apiClient.get('/tickets')).rejects.toThrow()
    expect(handleAuthFailure).toHaveBeenCalledTimes(1)
  })
})
