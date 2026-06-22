import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { configureApiClient, withApiPath } from '@/shared/api/client'
import type { AuthStatus, CurrentAuthGroup, CurrentUser } from '@/shared/types/auth'

type LoginParams = {
  username: string
  password: string
}

type MFAVerifyParams = {
  mfaToken: string
  code: string
}

type LoginResult =
  | { status: 'authenticated' }
  | {
      status: 'mfa_required'
      mfaToken: string
      setupRequired: boolean
      otpAuthURL?: string
      mfaSecret?: string
      qrDataURL?: string
    }

type AuthContextValue = {
  status: AuthStatus
  user: CurrentUser | null
  accessToken: string | null
  login: (params: LoginParams) => Promise<LoginResult>
  verifyMFA?: (params: MFAVerifyParams) => Promise<void>
  logout: () => Promise<void>
  clearAuth: () => void
  isAuthenticated: boolean
}

type MeResponse = {
  id: number
  username: string
  protected?: boolean
  is_active?: boolean
  auth_groups: Array<CurrentUser['authGroups'][number] | CurrentAuthGroup> | null
  permissions?: string[] | null
  db_connection_ids?: number[] | null
}

type LoginResponse = {
  access_token?: string
  mfa_required?: boolean
  mfa_setup_required?: boolean
  mfa_token?: string
  otp_auth_url?: string
  mfa_secret?: string
  qr_data_url?: string
}

const AuthContext = createContext<AuthContextValue | null>(null)

function normalizeMe(payload: MeResponse): CurrentUser {
  const authGroupDetails = Array.isArray(payload.auth_groups)
    ? payload.auth_groups.flatMap((group) => {
        if (typeof group === 'string') {
          return []
        }
        if (group && typeof group === 'object' && typeof group.group_key === 'string') {
          return [{
            id: typeof group.id === 'number' ? group.id : 0,
            group_key: group.group_key,
            name: typeof group.name === 'string' ? group.name : group.group_key,
            is_system: group.is_system === true,
            is_protected: group.is_protected === true,
          }]
        }
        return []
      })
    : []

  return {
    id: payload.id,
    username: payload.username,
    authGroups: Array.isArray(payload.auth_groups)
      ? payload.auth_groups.flatMap((group) => {
          if (typeof group === 'string') {
            return [group]
          }
          if (group && typeof group === 'object' && typeof group.group_key === 'string') {
            return [group.group_key]
          }
          return []
        })
      : [],
    authGroupDetails,
    permissions: Array.isArray(payload.permissions) ? payload.permissions.filter((item): item is string => typeof item === 'string') : [],
    dbConnectionIds: Array.isArray(payload.db_connection_ids) ? payload.db_connection_ids.filter((item): item is number => typeof item === 'number') : [],
    protected: payload.protected === true,
    isActive: payload.is_active !== false,
  }
}

async function fetchJSON<T>(path: string, init: RequestInit = {}) {
  const response = await fetch(withApiPath(path), {
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
    setAccessToken(null)
    setUser(null)
    setStatus('anonymous')
  }, [])

  const applyAccessToken = useCallback((token: string | null) => {
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
          : 'Failed to load the current user'
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
    const refreshedToken = await refreshAccessToken()
    if (!refreshedToken) {
      return
    }

    try {
      const currentUser = await fetchMe(refreshedToken)
      setUser(currentUser)
      setStatus('authenticated')
    } catch {
      clearAuth()
    }
  }, [clearAuth, fetchMe, refreshAccessToken])

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

  const completeLogin = useCallback(async (token: string) => {
    applyAccessToken(token)

    try {
      const currentUser = await fetchMe(token)
      setUser(currentUser)
      setStatus('authenticated')
    } catch (error) {
      clearAuth()
      throw error
    }
  }, [applyAccessToken, clearAuth, fetchMe])

  const login = useCallback(async ({ username, password }: LoginParams): Promise<LoginResult> => {
    const { response, data } = await fetchJSON<LoginResponse>('/auth/login', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ username, password }),
    })

    if (!response.ok) {
      const message =
        data && typeof data === 'object' && 'error' in data && typeof data.error === 'string'
          ? data.error
          : 'Sign-in failed'
      throw new Error(message)
    }

    if (data?.access_token) {
      await completeLogin(data.access_token)
      return { status: 'authenticated' }
    }

    if ((data?.mfa_required || data?.mfa_setup_required) && data.mfa_token) {
      return {
        status: 'mfa_required',
        mfaToken: data.mfa_token,
        setupRequired: data.mfa_setup_required === true,
        otpAuthURL: data.otp_auth_url,
        mfaSecret: data.mfa_secret,
        qrDataURL: data.qr_data_url,
      }
    }

    throw new Error('Sign-in failed')
  }, [completeLogin])

  const verifyMFA = useCallback(async ({ mfaToken, code }: MFAVerifyParams) => {
    const { response, data } = await fetchJSON<LoginResponse>('/auth/mfa/verify', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ mfa_token: mfaToken, code }),
    })

    if (!response.ok || !data?.access_token) {
      const message =
        data && typeof data === 'object' && 'error' in data && typeof data.error === 'string'
          ? data.error
          : 'MFA verification failed'
      throw new Error(message)
    }

    await completeLogin(data.access_token)
  }, [completeLogin])

  const logout = useCallback(async () => {
    const token = accessToken

    try {
      await fetch(withApiPath('/auth/logout'), {
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
    verifyMFA,
    logout,
    clearAuth,
    isAuthenticated: status === 'authenticated' && user !== null,
  }), [accessToken, clearAuth, login, logout, status, user, verifyMFA])

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const context = useContext(AuthContext)
  if (!context) {
    throw new Error('useAuth must be used within AuthProvider')
  }
  return context
}
