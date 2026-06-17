type ApiClientConfig = {
  getAccessToken: () => string | null
  refreshAccessToken: () => Promise<string | null>
  handleAuthFailure: () => void
}

type EventStreamMessage = {
  event: string
  data: JsonValue | null
}

type JsonPrimitive = string | number | boolean | null
type JsonValue = JsonPrimitive | JsonValue[] | { [key: string]: JsonValue }

const defaultConfig: ApiClientConfig = {
  getAccessToken: () => null,
  refreshAccessToken: async () => null,
  handleAuthFailure: () => undefined,
}

let config = defaultConfig

export const API_PREFIX = '/api'

export function withApiPath(path: string) {
  if (path.startsWith(API_PREFIX)) {
    return path
  }

  return `${API_PREFIX}${path}`
}

export class ApiError extends Error {
  status: number
  data: JsonValue | null

  constructor(status: number, message: string, data: JsonValue | null) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.data = data
  }
}

export function configureApiClient(nextConfig: Partial<ApiClientConfig>) {
  config = { ...defaultConfig, ...nextConfig }
}

async function parseResponse<T>(response: Response): Promise<T | undefined> {
  if (response.status === 204) {
    return undefined
  }

  const contentType = response.headers.get('content-type') ?? ''
  if (contentType.includes('application/json')) {
    return response.json() as Promise<T>
  }

  return response.text() as Promise<T>
}

async function request<T>(path: string, init: RequestInit = {}, canRetry = true, tokenOverride?: string | null): Promise<T> {
  const headers = new Headers(init.headers)
  const token = tokenOverride ?? config.getAccessToken()
  const apiPath = withApiPath(path)

  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }

  if (init.body && !(init.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  const response = await fetch(apiPath, {
    ...init,
    headers,
    credentials: 'same-origin',
  })

  if (response.status === 401 && canRetry) {
    const refreshedToken = await config.refreshAccessToken()
    if (refreshedToken) {
      return request<T>(apiPath, init, false, refreshedToken)
    }
    config.handleAuthFailure()
  }

  const data = await parseResponse<JsonValue>(response)
  if (!response.ok) {
    const message =
      typeof data === 'object' && data !== null && 'error' in data && typeof data.error === 'string'
        ? data.error
        : `Request failed with status ${response.status}`

    throw new ApiError(response.status, message, data ?? null)
  }

  return data as T
}

export const apiClient = {
  get: <T>(path: string) => request<T>(path),
  download: async (path: string) => {
    const headers = new Headers()
    const token = config.getAccessToken()
    const apiPath = withApiPath(path)

    if (token) {
      headers.set('Authorization', `Bearer ${token}`)
    }

    let response = await fetch(apiPath, {
      method: 'GET',
      headers,
      credentials: 'same-origin',
    })

    if (response.status === 401) {
      const refreshedToken = await config.refreshAccessToken()
      if (refreshedToken) {
        const retryHeaders = new Headers()
        retryHeaders.set('Authorization', `Bearer ${refreshedToken}`)
        response = await fetch(apiPath, {
          method: 'GET',
          headers: retryHeaders,
          credentials: 'same-origin',
        })
      } else {
        config.handleAuthFailure()
      }
    }

    if (!response.ok) {
      const contentType = response.headers.get('content-type') ?? ''
      const data = contentType.includes('application/json')
        ? await response.json() as JsonValue
        : await response.text() as JsonValue
      let message = `Request failed with status ${response.status}`
      if (typeof data === 'object' && data !== null && 'error' in data && typeof data.error === 'string') {
        message = data.error
      } else if (typeof data === 'string' && data.trim()) {
        message = data.trim()
      }
      throw new ApiError(response.status, message, data)
    }

    return response
  },
  post: <T>(path: string, body?: unknown) =>
    request<T>(path, {
      method: 'POST',
      body: body === undefined ? undefined : JSON.stringify(body),
    }),
  patch: <T>(path: string, body?: unknown) =>
    request<T>(path, {
      method: 'PATCH',
      body: body === undefined ? undefined : JSON.stringify(body),
    }),
  delete: <T>(path: string) =>
    request<T>(path, {
      method: 'DELETE',
    }),
}

export function openEventStream(
  path: string,
  callbacks: {
    signal?: AbortSignal
    onOpen?: () => void
    onEvent: (message: EventStreamMessage) => void
    onError?: (error: unknown) => void
  },
) {
  const controller = new AbortController()
  let reconnectTimer: number | null = null
  let closed = false

  const cleanup = () => {
    closed = true
    controller.abort()
    if (reconnectTimer !== null) {
      window.clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
  }

  callbacks.signal?.addEventListener('abort', cleanup, { once: true })

  const scheduleReconnect = () => {
    if (closed || callbacks.signal?.aborted) {
      return
    }
    reconnectTimer = window.setTimeout(() => {
      reconnectTimer = null
      void connect(true)
    }, 1000)
  }

  const emitBlock = (block: string) => {
    const lines = block.split(/\r?\n/)
    let eventName = 'message'
    const dataLines: string[] = []

    for (const line of lines) {
      if (!line || line.startsWith(':')) {
        continue
      }
      if (line.startsWith('event:')) {
        eventName = line.slice(6).trim() || 'message'
        continue
      }
      if (line.startsWith('data:')) {
        dataLines.push(line.slice(5).trimStart())
      }
    }

    let parsed: JsonValue | null = null
    const rawData = dataLines.join('\n').trim()
    if (rawData) {
      try {
        parsed = JSON.parse(rawData) as JsonValue
      } catch {
        parsed = rawData
      }
    }

    callbacks.onEvent({ event: eventName, data: parsed })
  }

  const readStream = async (response: Response) => {
    const reader = response.body?.getReader()
    if (!reader) {
      throw new Error('event stream body is unavailable')
    }

    const decoder = new TextDecoder()
    let buffer = ''
    while (!closed && !controller.signal.aborted) {
      const { done, value } = await reader.read()
      if (done) {
        break
      }
      buffer += decoder.decode(value, { stream: true })

      let separatorIndex = buffer.search(/\r?\n\r?\n/)
      while (separatorIndex >= 0) {
        const block = buffer.slice(0, separatorIndex)
        buffer = buffer.slice(separatorIndex + (buffer[separatorIndex] === '\r' ? 4 : 2))
        if (block.trim()) {
          emitBlock(block)
        }
        separatorIndex = buffer.search(/\r?\n\r?\n/)
      }
    }
  }

  const connect = async (canRetryAuth: boolean, tokenOverride?: string | null): Promise<void> => {
    const headers = new Headers({ Accept: 'text/event-stream' })
    const token = tokenOverride ?? config.getAccessToken()
    if (token) {
      headers.set('Authorization', `Bearer ${token}`)
    }

    try {
      const response = await fetch(withApiPath(path), {
        method: 'GET',
        headers,
        credentials: 'same-origin',
        signal: controller.signal,
      })

      if (response.status === 401 && canRetryAuth) {
        const refreshedToken = await config.refreshAccessToken()
        if (refreshedToken) {
          return connect(false, refreshedToken)
        }
        config.handleAuthFailure()
        return
      }

      if (!response.ok) {
        throw new ApiError(response.status, `Request failed with status ${response.status}`, null)
      }

      callbacks.onOpen?.()
      await readStream(response)
      if (!closed && !controller.signal.aborted) {
        scheduleReconnect()
      }
    } catch (error) {
      if (controller.signal.aborted || closed) {
        return
      }
      callbacks.onError?.(error)
      scheduleReconnect()
    }
  }

  void connect(true)
  return cleanup
}
