import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { configureApiClient } from '@/shared/api/client'
import type { AuthStatus, CurrentUser } from '@/shared/types/auth'

const ACCESS_TOKEN_KEY = 'dbre_maestro.access_token'

type LoginParams = {
  username: string
  password: string
}

type AuthContextValue = {
  status: AuthStatus
  user: CurrentUser | null
  accessToken: string | null
  login: (params: LoginParams) => Promise<void>
  logout: () => Promise<void>
  clearAuth: () => void
  isAuthenticated: boolean
}

type MeResponse = {
  id: number
  username: string
  auth_groups: CurrentUser['authGroups']
}

type LoginResponse = {
  access_token: string
}

const AuthContext = createContext<AuthContextValue | null>(null)

function readStoredToken() {
  return window.localStorage.getItem(ACCESS_TOKEN_KEY)
}

function writeStoredToken(token: string | null) {
  if (token) {
    window.localStorage.setItem(ACCESS_TOKEN_KEY, token)
  } else {
    window.localStorage.removeItem(ACCESS_TOKEN_KEY)
  }
}

function normalizeMe(payload: MeResponse): CurrentUser {
  return {
    id: payload.id,
    username: payload.username,
    authGroups: payload.auth_groups,
  }
}

async function fetchJSON<T>(path: string, init: RequestInit = {}) {
  const response = await fetch(path, {
    ...init,
    credentials: 'same-origin',
  })

  const contentType = response.headers.get('content-type') ?? ''
  const data = contentType.includes('application/json')
    ? await response.json()
    : null

  return { response, data: data as T }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>('loading')
  const [user, setUser] = useState<CurrentUser | null>(null)
  const [accessToken, setAccessToken] = useState<string | null>(null)
  const refreshPromiseRef = useRef<Promise<string | null> | null>(null)

  const clearAuth = useCallback(() => {
    writeStoredToken(null)
    setAccessToken(null)
    setUser(null)
    setStatus('anonymous')
  }, [])

  const applyAccessToken = useCallback((token: string | null) => {
    writeStoredToken(token)
    setAccessToken(token)
  }, [])

  const fetchMe = useCallback(async (token: string) => {
    const { response, data } = await fetchJSON<MeResponse>('/auth/me', {
      headers: {
        Authorization: `Bearer ${token}`,
      },
    })

    if (!response.ok) {
      const errorMessage =
        data && typeof data === 'object' && 'error' in data && typeof data.error === 'string'
          ? data.error
          : '讀取目前使用者失敗'
      throw new Error(errorMessage)
    }

    return normalizeMe(data)
  }, [])

  const refreshAccessToken = useCallback(async () => {
    if (refreshPromiseRef.current) {
      return refreshPromiseRef.current
    }

    refreshPromiseRef.current = (async () => {
      const { response, data } = await fetchJSON<LoginResponse>('/auth/refresh', {
        method: 'POST',
      })

      if (!response.ok || !data?.access_token) {
        clearAuth()
        return null
      }

      applyAccessToken(data.access_token)
      return data.access_token
    })()

    try {
      return await refreshPromiseRef.current
    } finally {
      refreshPromiseRef.current = null
    }
  }, [applyAccessToken, clearAuth])

  const bootstrap = useCallback(async () => {
    const storedToken = readStoredToken()

    if (!storedToken) {
      setStatus('anonymous')
      return
    }

    applyAccessToken(storedToken)

    try {
      const currentUser = await fetchMe(storedToken)
      setUser(currentUser)
      setStatus('authenticated')
      return
    } catch {
      const refreshedToken = await refreshAccessToken()
      if (!refreshedToken) {
        clearAuth()
        return
      }

      try {
        const currentUser = await fetchMe(refreshedToken)
        setUser(currentUser)
        setStatus('authenticated')
      } catch {
        clearAuth()
      }
    }
  }, [applyAccessToken, clearAuth, fetchMe, refreshAccessToken])

  useEffect(() => {
    void bootstrap()
  }, [bootstrap])

  useEffect(() => {
    configureApiClient({
      getAccessToken: () => accessToken,
      refreshAccessToken,
      handleAuthFailure: clearAuth,
    })
  }, [accessToken, clearAuth, refreshAccessToken])

  const login = useCallback(async ({ username, password }: LoginParams) => {
    const { response, data } = await fetchJSON<LoginResponse>('/auth/login', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ username, password }),
    })

    if (!response.ok || !data?.access_token) {
      const message =
        data && typeof data === 'object' && 'error' in data && typeof data.error === 'string'
          ? data.error
          : '登入失敗'
      throw new Error(message)
    }

    applyAccessToken(data.access_token)

    try {
      const currentUser = await fetchMe(data.access_token)
      setUser(currentUser)
      setStatus('authenticated')
    } catch (error) {
      clearAuth()
      throw error
    }
  }, [applyAccessToken, clearAuth, fetchMe])

  const logout = useCallback(async () => {
    const token = accessToken

    try {
      await fetch('/auth/logout', {
        method: 'POST',
        credentials: 'same-origin',
        headers: token
          ? {
              Authorization: `Bearer ${token}`,
            }
          : undefined,
      })
    } finally {
      clearAuth()
    }
  }, [accessToken, clearAuth])

  const value = useMemo<AuthContextValue>(() => ({
    status,
    user,
    accessToken,
    login,
    logout,
    clearAuth,
    isAuthenticated: status === 'authenticated' && user !== null,
  }), [accessToken, clearAuth, login, logout, status, user])

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const context = useContext(AuthContext)
  if (!context) {
    throw new Error('useAuth must be used within AuthProvider')
  }
  return context
}
