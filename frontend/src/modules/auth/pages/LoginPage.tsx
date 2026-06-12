import { useEffect, useState } from 'react'
import { Eye, EyeOff, Loader2, ShieldCheck } from 'lucide-react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { useAuth } from '@/shared/auth/AuthContext'
import { getSetupStatus } from '@/shared/setup/api'
import { InlineAlert } from '@/shared/ui/InlineAlert'

export function LoginPage() {
  const { isAuthenticated, login, status } = useAuth()
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
    return <Navigate to={nextPath ?? '/tickets'} replace />
  }

  const handleSubmit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setError('')
    setLoading(true)

    try {
      await login({ username, password })
      const nextPath = (location.state as { from?: { pathname?: string } } | null)?.from?.pathname
      navigate(nextPath ?? '/tickets', { replace: true })
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : '登入失敗，請稍後重試。')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-page px-4 py-8 sm:px-6">
      <div className="mx-auto flex min-h-[calc(100vh-4rem)] max-w-6xl items-center justify-center">
        <div className="grid w-full max-w-5xl overflow-hidden rounded-card border border-border bg-panel shadow-card lg:grid-cols-[1.1fr_0.9fr]">
          <section className="hidden border-r border-border bg-panel-soft p-10 lg:flex lg:flex-col lg:justify-between">
            <div>
              <p className="text-[11px] font-bold uppercase tracking-[0.24em] text-faint">Database Governance</p>
              <h1 className="mt-4 max-w-md font-display text-[34px] font-black leading-tight tracking-tight text-ink">
                DBRE Maestro
              </h1>
              <p className="mt-4 max-w-md text-sm text-muted">
                資料庫治理流程，從登入後的第一個操作開始一致化。
              </p>
            </div>

            <div className="rounded-card border border-border bg-panel p-5 shadow-soft">
              <div className="flex items-start gap-3">
                <div className="flex h-10 w-10 items-center justify-center rounded-control bg-accent-soft text-accent">
                  <ShieldCheck className="h-5 w-5" />
                </div>
                <div>
                  <p className="text-sm font-semibold text-ink">登入後可立即確認角色能力</p>
                  <p className="mt-1 text-xs text-muted">
                    `GET /auth/me` 會載入目前身份、`auth_groups` 與 `permissions`，前端依此決定側欄入口與工單操作按鈕。
                  </p>
                </div>
              </div>
            </div>
          </section>

          <section className="flex flex-col justify-center p-6 sm:p-8 lg:p-10">
            <div className="mx-auto w-full max-w-md">
              <div className="mb-8">
                <p className="text-[11px] font-bold uppercase tracking-[0.24em] text-faint">Sign In</p>
                <h2 className="mt-2 font-display text-3xl font-black tracking-tight text-ink">登入平台</h2>
                <p className="mt-2 text-sm text-muted">
                  使用已完成 setup 的帳號登入。若你剛完成初始設定，請直接使用剛建立的管理員帳號。
                </p>
              </div>

              <form className="flex flex-col gap-5" onSubmit={handleSubmit}>
                <label className="flex flex-col gap-1.5">
                  <span className="text-xs font-semibold text-ink">使用者名稱</span>
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
                  <span className="text-xs font-semibold text-ink">密碼</span>
                  <div className="relative">
                    <input
                      className="h-10 w-full rounded-control border border-border bg-panel px-3 pr-10 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                      value={password}
                      onChange={(event) => setPassword(event.target.value)}
                      placeholder="輸入你的密碼"
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
                    'inline-flex h-10 items-center justify-center gap-2 rounded-control px-4 text-sm font-bold transition-colors',
                    'bg-brand text-white hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50',
                  )}
                >
                  {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
                  {loading ? '登入中…' : '登入'}
                </button>
              </form>

              {setupCompleted === false ? (
                <div className="mt-6 rounded-card border border-border bg-panel-soft px-4 py-3">
                  <p className="text-xs font-semibold uppercase tracking-[0.2em] text-faint">First Time?</p>
                  <p className="mt-1 text-sm text-muted">
                    平台尚未完成初始設定，請先前往
                    <button
                      type="button"
                      onClick={() => navigate('/setup')}
                      className="ml-1 font-semibold text-accent transition hover:text-blue-700"
                    >
                      Setup Wizard
                    </button>
                    。
                  </p>
                </div>
              ) : null}
            </div>
          </section>
        </div>
      </div>
    </div>
  )
}
