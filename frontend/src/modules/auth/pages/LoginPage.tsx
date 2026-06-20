import { useEffect, useState } from 'react'
import { Eye, EyeOff, Loader2 } from 'lucide-react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { useAuth } from '@/shared/auth/AuthContext'
import { defaultRouteForPermissions } from '@/shared/auth/permissions'
import { getSetupStatus } from '@/shared/setup/api'
import { InlineAlert } from '@/shared/ui/InlineAlert'

export function LoginPage() {
  const { isAuthenticated, login, status, user } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [loading, setLoading] = useState(false)
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

  if (status !== 'loading' && isAuthenticated) {
    const nextPath = (location.state as { from?: { pathname?: string } } | null)?.from?.pathname
    return <Navigate to={nextPath ?? defaultRouteForPermissions(user?.permissions ?? [])} replace />
  }

  const handleSubmit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setError('')
    setLoading(true)

    try {
      await login({ username, password })
      const nextPath = (location.state as { from?: { pathname?: string } } | null)?.from?.pathname
      navigate(nextPath ?? '/', { replace: true })
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : 'Login failed. Please try again later.')
    } finally {
      setLoading(false)
    }
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
              className="h-10 rounded-control border border-border bg-panel px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
              value={username}
              onChange={(event) => setUsername(event.target.value)}
              placeholder="e.g. admin"
              autoComplete="username"
              disabled={loading}
            />
          </label>

          <label className="flex flex-col gap-1.5">
            <span className="text-sm font-semibold text-ink">Password</span>
            <div className="relative">
              <input
                className="h-10 w-full rounded-control border border-border bg-panel px-3 pr-10 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                placeholder="Enter your password"
                type={showPassword ? 'text' : 'password'}
                autoComplete="current-password"
                disabled={loading}
              />
              <button
                type="button"
                className="absolute right-3 top-1/2 -translate-y-1/2 text-faint transition hover:text-muted"
                onClick={() => setShowPassword((value) => !value)}
                disabled={loading}
              >
                {showPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
              </button>
            </div>
          </label>

          {error ? <InlineAlert>{error}</InlineAlert> : null}

          <button
            type="submit"
            disabled={loading || username.trim() === '' || password.trim() === ''}
            className={cn(
              'mt-1 inline-flex h-10 items-center justify-center gap-2 rounded-control px-4 text-sm font-bold transition-colors',
              'bg-brand text-white hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50',
            )}
          >
            {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
            {loading ? 'Signing in…' : 'Sign in'}
          </button>
        </form>

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
