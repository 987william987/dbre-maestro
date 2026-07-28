import { useEffect, useState } from 'react'
import { Eye, EyeOff, Loader2 } from 'lucide-react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { apiClient } from '@/shared/api/client'
import { useAuth } from '@/shared/auth/AuthContext'
import { defaultRouteForPermissions } from '@/shared/auth/permissions'
import larkLogoUrl from '@/assets/lark-share-logo.png'
import { getSetupStatus } from '@/shared/setup/api'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'

export function LoginPage() {
  const { isAuthenticated, login, consumeLarkLogin, consumeSSOLogin, status, user, verifyMFA } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [mfaCode, setMFACode] = useState('')
  const [mfaChallenge, setMFAChallenge] = useState<{
    token: string
    setupRequired: boolean
    otpAuthURL?: string
    mfaSecret?: string
    qrDataURL?: string
  } | null>(null)
  const [showPassword, setShowPassword] = useState(false)
  const [loading, setLoading] = useState(false)
  const [larkLoading, setLarkLoading] = useState(false)
  const [ssoLoading, setSSOLoading] = useState(false)
  const [ssoProvider, setSSOProvider] = useState<{ display_name: string; start_url: string } | null>(null)
  const [error, setError] = useState('')
  const [setupCompleted, setSetupCompleted] = useState<boolean | null>(null)

  useEffect(() => {
    let active = true

    void getSetupStatus()
      .then((result) => {
        if (active) {
          setSetupCompleted(result.setup_completed)
        }
      })
      .catch(() => {
        if (active) {
          setSetupCompleted(null)
        }
      })

    return () => {
      active = false
    }
  }, [])

  useEffect(() => {
    let active = true
    void apiClient.get<{ providers?: Array<{ display_name?: string; start_url?: string }> }>('/auth/sso/providers')
      .then((result) => {
        if (!active) {
          return
        }
        const provider = Array.isArray(result.providers)
          ? result.providers.find((item) => typeof item.start_url === 'string' && item.start_url !== '')
          : null
        setSSOProvider(provider ? {
          display_name: provider.display_name || 'SSO',
          start_url: provider.start_url || '/api/auth/sso/start',
        } : null)
      })
      .catch(() => {
        if (active) {
          setSSOProvider(null)
        }
      })
    return () => {
      active = false
    }
  }, [])

  useEffect(() => {
    const params = new URLSearchParams(location.search)
    const ticket = params.get('lark_ticket')?.trim() ?? ''
    const larkError = params.get('lark_error')?.trim() ?? ''
    const ssoTicket = params.get('sso_ticket')?.trim() ?? ''
    const ssoError = params.get('sso_error')?.trim() ?? ''
    if (ssoError) {
      setError(ssoLoginErrorMessage(ssoError))
      navigate('/login', { replace: true })
      return
    }
    if (larkError) {
      setError(larkLoginErrorMessage(larkError))
      navigate('/login', { replace: true })
      return
    }
    if (ssoTicket) {
      let active = true
      setLoading(true)
      setError('')
      if (!consumeSSOLogin) {
        setError('SSO login is unavailable.')
        setLoading(false)
        navigate('/login', { replace: true })
        return
      }
      void consumeSSOLogin(ssoTicket)
        .then((result) => {
          if (!active) {
            return
          }
          navigate('/login', { replace: true })
          if (result.status === 'mfa_required') {
            setMFAChallenge({
              token: result.mfaToken,
              setupRequired: result.setupRequired,
              otpAuthURL: result.otpAuthURL,
              mfaSecret: result.mfaSecret,
              qrDataURL: result.qrDataURL,
            })
            setMFACode('')
            return
          }
          navigate(result.returnTo ?? '/', { replace: true })
        })
        .catch((consumeError: unknown) => {
          if (active) {
            setError(consumeError instanceof Error ? consumeError.message : 'SSO login failed. Please try again later.')
            navigate('/login', { replace: true })
          }
        })
        .finally(() => {
          if (active) {
            setLoading(false)
          }
        })
      return () => {
        active = false
      }
    }
    if (!ticket) {
      return
    }

    let active = true
    setLoading(true)
    setError('')
    if (!consumeLarkLogin) {
      setError('Lark login is unavailable.')
      setLoading(false)
      navigate('/login', { replace: true })
      return
    }
    void consumeLarkLogin(ticket)
      .then((result) => {
        if (!active) {
          return
        }
        navigate('/login', { replace: true })
        if (result.status === 'mfa_required') {
          setMFAChallenge({
            token: result.mfaToken,
            setupRequired: result.setupRequired,
            otpAuthURL: result.otpAuthURL,
            mfaSecret: result.mfaSecret,
            qrDataURL: result.qrDataURL,
          })
          setMFACode('')
          return
        }
        navigate(result.returnTo ?? '/', { replace: true })
      })
      .catch((consumeError: unknown) => {
        if (active) {
          setError(consumeError instanceof Error ? consumeError.message : 'Lark login failed. Please try again later.')
          navigate('/login', { replace: true })
        }
      })
      .finally(() => {
        if (active) {
          setLoading(false)
        }
      })

    return () => {
      active = false
    }
  }, [consumeLarkLogin, consumeSSOLogin, location.search, location.state, navigate])

  if (status === 'loading') {
    return (
      <div className="flex min-h-screen items-center justify-center bg-page px-6">
        <LoadingBlock message="Loading..." className="min-h-[160px] w-full max-w-sm rounded-card border-border bg-panel" />
      </div>
    )
  }

  const hasLoginTicket = new URLSearchParams(location.search).has('lark_ticket') || new URLSearchParams(location.search).has('sso_ticket')
  if (isAuthenticated && !hasLoginTicket) {
    const nextPath = (location.state as { from?: { pathname?: string } } | null)?.from?.pathname
    return <Navigate to={nextPath ?? defaultRouteForPermissions(user?.permissions ?? [])} replace />
  }

  const setupRequired = setupCompleted === false
  const loginDisabled = setupRequired || loading || (mfaChallenge ? mfaCode.length !== 6 : username.trim() === '' || password.trim() === '')
  const larkDisabled = setupRequired || loading || larkLoading || ssoLoading
  const ssoDisabled = setupRequired || loading || larkLoading || ssoLoading

  const handleSubmit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (setupRequired) {
      return
    }
    setError('')
    setLoading(true)

    try {
      if (mfaChallenge) {
        if (!verifyMFA) {
          throw new Error('MFA verification is unavailable')
        }
        await verifyMFA({ mfaToken: mfaChallenge.token, code: mfaCode })
        const nextPath = (location.state as { from?: { pathname?: string } } | null)?.from?.pathname
        navigate(nextPath ?? '/', { replace: true })
        return
      }
      const result = await login({ username, password })
      if (result.status === 'mfa_required') {
        setMFAChallenge({
          token: result.mfaToken,
          setupRequired: result.setupRequired,
          otpAuthURL: result.otpAuthURL,
          mfaSecret: result.mfaSecret,
          qrDataURL: result.qrDataURL,
        })
        setMFACode('')
        return
      }
      const nextPath = (location.state as { from?: { pathname?: string } } | null)?.from?.pathname
      navigate(nextPath ?? '/', { replace: true })
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : 'Login failed. Please try again later.')
    } finally {
      setLoading(false)
    }
  }

  const handleLarkLogin = async () => {
    if (setupRequired) {
      return
    }
    setError('')
    setLarkLoading(true)
    try {
      const nextPath = (location.state as { from?: { pathname?: string } } | null)?.from?.pathname
      const returnTo = nextPath && nextPath.startsWith('/') ? nextPath : '/'
      const result = await apiClient.get<{ url: string }>(`/auth/lark/login/start?returnTo=${encodeURIComponent(returnTo)}`)
      window.location.assign(result.url)
    } catch (startError) {
      setError(startError instanceof Error ? startError.message : 'Failed to start Lark login.')
      setLarkLoading(false)
    }
  }

  const handleSSOLogin = () => {
    if (setupRequired || !ssoProvider) {
      return
    }
    setError('')
    setSSOLoading(true)
    const nextPath = (location.state as { from?: { pathname?: string } } | null)?.from?.pathname
    const returnTo = nextPath && nextPath.startsWith('/') ? nextPath : '/'
    const separator = ssoProvider.start_url.includes('?') ? '&' : '?'
    window.location.assign(`${ssoProvider.start_url}${separator}returnTo=${encodeURIComponent(returnTo)}`)
  }

  return (
    <div className="flex min-h-screen flex-col items-center justify-center bg-page px-4 py-12">
      <div className="mb-6 text-center">
        <h1 className="font-display text-2xl font-black tracking-tight text-ink">DBRE Maestro</h1>
      </div>

      <div className="w-full max-w-sm rounded-card border border-border bg-panel p-8 shadow-card">
        <div className="mb-6">
          <h2 className="font-display text-2xl font-black tracking-tight text-ink">Sign in</h2>
          <p className="mt-1.5 text-sm text-muted">Enter your username and password to log in</p>
        </div>

        <form className="flex flex-col gap-4" onSubmit={handleSubmit}>
          <label className="flex flex-col gap-1.5">
            <span className="text-sm font-semibold text-ink">Username</span>
            <input
              className="h-10 rounded-control border border-border bg-panel px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20 disabled:cursor-not-allowed disabled:opacity-60"
              value={username}
              onChange={(event) => setUsername(event.target.value)}
              placeholder="e.g. admin"
              autoComplete="username"
              disabled={setupRequired || loading || mfaChallenge !== null}
            />
          </label>

          <label className="flex flex-col gap-1.5">
            <span className="text-sm font-semibold text-ink">Password</span>
            <div className="relative">
              <input
                className="h-10 w-full rounded-control border border-border bg-panel px-3 pr-10 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20 disabled:cursor-not-allowed disabled:opacity-60"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                placeholder="Enter your password"
                type={showPassword ? 'text' : 'password'}
                autoComplete="current-password"
                disabled={setupRequired || loading || mfaChallenge !== null}
              />
              <button
                type="button"
                className="absolute right-3 top-1/2 -translate-y-1/2 text-faint transition hover:text-muted disabled:cursor-not-allowed disabled:opacity-50"
                onClick={() => setShowPassword((value) => !value)}
                disabled={setupRequired || loading || mfaChallenge !== null}
              >
                {showPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
              </button>
            </div>
          </label>

          {mfaChallenge ? (
            <div className="grid gap-4 rounded-card border border-border bg-panel-soft px-4 py-4">
              <div>
                <p className="text-sm font-semibold text-ink">
                  {mfaChallenge.setupRequired ? 'Set up MFA' : 'Enter MFA code'}
                </p>
                <p className="mt-1 text-xs leading-5 text-muted">
                  {mfaChallenge.setupRequired
                    ? 'Scan the QR code with Google Authenticator or enter the setup key manually, then enter the 6-digit code.'
                    : 'Enter the 6-digit code from your authenticator app.'}
                </p>
              </div>
              {mfaChallenge.setupRequired && mfaChallenge.qrDataURL ? (
                <div className="flex justify-center">
                  <img src={mfaChallenge.qrDataURL} alt="MFA setup QR code" className="h-44 w-44 rounded-lg border border-border bg-white p-2" />
                </div>
              ) : null}
              {mfaChallenge.setupRequired && mfaChallenge.mfaSecret ? (
                <div className="rounded-lg border border-border bg-white px-3 py-2">
                  <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">Setup Key</p>
                  <p className="mt-1 break-all font-mono text-[12px] text-ink">{mfaChallenge.mfaSecret}</p>
                </div>
              ) : null}
              <label className="flex flex-col gap-1.5">
                <span className="text-sm font-semibold text-ink">Authenticator Code</span>
                <input
                  className="h-10 rounded-control border border-border bg-panel px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                  value={mfaCode}
                  onChange={(event) => setMFACode(event.target.value.replace(/\D/g, '').slice(0, 6))}
                  placeholder="000000"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  disabled={loading}
                />
              </label>
              <button
                type="button"
                onClick={() => {
                  setMFAChallenge(null)
                  setMFACode('')
                  setError('')
                }}
                className="justify-self-start text-xs font-semibold text-muted underline decoration-border underline-offset-4 transition hover:text-ink"
                disabled={loading}
              >
                Use another account
              </button>
            </div>
          ) : null}

          {error ? <InlineAlert>{error}</InlineAlert> : null}

          <button
            type="submit"
            disabled={loginDisabled}
            className={cn(
              'mt-1 inline-flex h-10 items-center justify-center gap-2 rounded-control px-4 text-sm font-bold transition-colors',
              'bg-brand text-white hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50',
            )}
          >
            {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
            {loading ? 'Signing in…' : mfaChallenge ? 'Verify and Sign in' : 'Sign in'}
          </button>
        </form>

        {!mfaChallenge ? (
          <>
            {ssoProvider ? (
              <button
                type="button"
                disabled={ssoDisabled}
                onClick={handleSSOLogin}
                className={cn(
                  'mt-3 inline-flex h-10 w-full items-center justify-center gap-2 rounded-control border border-border px-4 text-sm font-bold transition-colors',
                  'bg-panel text-ink hover:bg-panel-soft disabled:cursor-not-allowed disabled:opacity-50',
                )}
              >
                {ssoLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
                {ssoLoading ? 'Redirecting…' : `Sign in with ${ssoProvider.display_name}`}
              </button>
            ) : null}
            <button
              type="button"
              disabled={larkDisabled}
              onClick={handleLarkLogin}
              className={cn(
                'mt-3 inline-flex h-10 w-full items-center justify-center gap-2 rounded-control border border-border px-4 text-sm font-bold transition-colors',
                'bg-panel text-ink hover:bg-panel-soft disabled:cursor-not-allowed disabled:opacity-50',
              )}
            >
              {larkLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : <img src={larkLogoUrl} alt="" className="h-5 w-5 rounded-full" />}
              {larkLoading ? 'Redirecting…' : 'Sign in with Lark'}
            </button>
          </>
        ) : null}

        {setupCompleted === false ? (
          <div className="mt-6 rounded-card border border-border bg-panel-soft px-4 py-4">
            <p className="text-xs font-semibold uppercase tracking-[0.18em] text-faint">First time?</p>
            <p className="mt-2 text-sm leading-6 text-muted">
              The platform has not been initialized yet. Please complete the{' '}
              <button
                type="button"
                onClick={() => navigate('/setup')}
                className="font-semibold text-ink underline decoration-border underline-offset-4 transition hover:text-accent hover:decoration-accent"
              >
                Setup Wizard
              </button>{' '}
              first.
            </p>
          </div>
        ) : null}
      </div>
    </div>
  )
}

function larkLoginErrorMessage(code: string) {
  if (code === 'access_denied') {
    return 'Lark authorization was cancelled.'
  }
  if (code === 'user_disabled') {
    return 'The Lark account is linked to a disabled user.'
  }
  if (code === 'setup_required') {
    return 'Please complete the Setup Wizard before signing in.'
  }
  return 'Lark login failed. Please try again later.'
}

function ssoLoginErrorMessage(code: string) {
  if (code === 'access_denied') {
    return 'SSO authorization was cancelled.'
  }
  if (code === 'user_disabled') {
    return 'The SSO account is linked to a disabled user.'
  }
  if (code === 'setup_required') {
    return 'Please complete the Setup Wizard before signing in.'
  }
  return 'SSO login failed. Please try again later.'
}
