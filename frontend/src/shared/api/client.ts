type ApiClientConfig = {
  getAccessToken: () => string | null
  refreshAccessToken: () => Promise<string | null>
  handleAuthFailure: () => void
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
