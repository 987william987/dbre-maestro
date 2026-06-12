import { useState, useCallback, useEffect } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { Eye, EyeOff, CheckCircle2, Circle, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { withApiPath } from '@/shared/api/client'
import { getSetupStatus } from '@/shared/setup/api'

// ────────────────────────────────────────────────────────────────────────────
// Types
// ────────────────────────────────────────────────────────────────────────────

type Step = 0 | 1 | 2 | 3  // welcome | account | notifications | complete

interface AccountForm {
  username: string
  email: string
  password: string
  confirmPassword: string
}

interface PasswordRule {
  label: string
  test: (p: string) => boolean
}

const PASSWORD_RULES: PasswordRule[] = [
  { label: '至少 8 個字元',   test: p => p.length >= 8 },
  { label: '包含大寫字母',     test: p => /[A-Z]/.test(p) },
  { label: '包含小寫字母',     test: p => /[a-z]/.test(p) },
  { label: '包含數字',         test: p => /[0-9]/.test(p) },
]

const STEP_LABELS = ['歡迎', '管理員帳號', '通知設定', '完成'] as const

// ────────────────────────────────────────────────────────────────────────────
// Sub-components
// ────────────────────────────────────────────────────────────────────────────

function StepProgress({ current }: { current: Step }) {
  return (
    <div className="flex items-center gap-2">
      {STEP_LABELS.map((label, i) => (
        <div key={i} className="flex items-center gap-2">
          <div className={cn(
            'flex items-center gap-1.5 text-xs font-semibold transition-colors',
            i < current   && 'text-success',
            i === current && 'text-ink',
            i > current   && 'text-faint',
          )}>
            {i < current
              ? <CheckCircle2 className="w-4 h-4 text-success" strokeWidth={2.5} />
              : <Circle className={cn('w-4 h-4', i === current ? 'text-accent' : 'text-border-strong')} strokeWidth={2.5} />
            }
            <span className="hidden sm:inline">{label}</span>
          </div>
          {i < STEP_LABELS.length - 1 && (
            <div className={cn(
              'w-6 h-px mx-1 transition-colors',
              i < current ? 'bg-success' : 'bg-border',
            )} />
          )}
        </div>
      ))}
    </div>
  )
}

function FieldGroup({ label, error, children }: {
  label: string
  error?: string
  children: React.ReactNode
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <label className="text-xs font-semibold text-ink">{label}</label>
      {children}
      {error && <p className="text-xs text-danger">{error}</p>}
    </div>
  )
}

function Input({ className, ...props }: React.InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={cn(
        'h-10 w-full rounded-control border border-border bg-panel px-3 text-sm text-ink',
        'placeholder:text-faint',
        'focus:outline-none focus:ring-2 focus:ring-accent/30 focus:border-accent',
        'disabled:opacity-50',
        className,
      )}
      {...props}
    />
  )
}

function Button({ variant = 'primary', className, children, ...props }: {
  variant?: 'primary' | 'secondary' | 'ghost'
} & React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      className={cn(
        'inline-flex h-9 items-center justify-center gap-2 rounded-control px-4 text-sm font-bold',
        'transition-colors disabled:opacity-50 disabled:cursor-not-allowed',
        variant === 'primary'   && 'bg-brand text-white hover:bg-zinc-800',
        variant === 'secondary' && 'border border-border bg-panel text-ink hover:bg-page',
        variant === 'ghost'     && 'text-muted hover:bg-page',
        className,
      )}
      {...props}
    >
      {children}
    </button>
  )
}

// ────────────────────────────────────────────────────────────────────────────
// Steps
// ────────────────────────────────────────────────────────────────────────────

function WelcomeStep({ onNext }: { onNext: () => void }) {
  return (
    <div className="flex flex-col items-center text-center gap-6 py-4">
      <div className="w-16 h-16 rounded-card bg-brand flex items-center justify-center shadow-card">
        <span className="text-white text-2xl font-display font-black">M</span>
      </div>

      <div className="flex flex-col gap-2">
        <h1 className="text-2xl font-display font-black text-ink tracking-tight">
          歡迎使用 DBRE Maestro
        </h1>
        <p className="text-muted text-sm max-w-sm">
          資料庫安全治理平台——讓工單審核、查詢控制與稽核日誌統一管理。
        </p>
      </div>

      <ul className="flex flex-col gap-2 text-left text-sm text-muted w-full max-w-xs">
        {[
          '集中管理 DDL/DML 工單審核流程',
          '查詢結果自動脫敏，保護敏感欄位',
          '完整稽核日誌，所有操作皆有記錄',
        ].map((item, i) => (
          <li key={i} className="flex items-start gap-2">
            <CheckCircle2 className="w-4 h-4 text-success mt-0.5 shrink-0" strokeWidth={2.5} />
            <span>{item}</span>
          </li>
        ))}
      </ul>

      <Button className="w-full max-w-xs h-10 text-base" onClick={onNext}>
        開始設定 →
      </Button>
    </div>
  )
}

function AccountStep({
  onNext,
  onBack,
}: {
  onNext: (username: string) => void
  onBack: () => void
}) {
  const [form, setForm] = useState<AccountForm>({
    username: '',
    email: '',
    password: '',
    confirmPassword: '',
  })
  const [showPassword, setShowPassword] = useState(false)
  const [touched, setTouched] = useState<Partial<Record<keyof AccountForm, boolean>>>({})
  const [apiError, setApiError] = useState('')
  const [loading, setLoading] = useState(false)

  const update = useCallback((field: keyof AccountForm) => (
    (e: React.ChangeEvent<HTMLInputElement>) => {
      setForm(f => ({ ...f, [field]: e.target.value }))
      setApiError('')
    }
  ), [])

  const blur = useCallback((field: keyof AccountForm) => () => {
    setTouched(t => ({ ...t, [field]: true }))
  }, [])

  const errors = {
    username: (() => {
      if (!form.username) return '請輸入使用者名稱'
      if (form.username.length < 3 || form.username.length > 64) return '3–64 個字元'
      if (!/^[a-zA-Z0-9_]+$/.test(form.username)) return '僅允許英文字母、數字和底線'
      return ''
    })(),
    email: (() => {
      if (!form.email) return '請輸入 Email'
      if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(form.email)) return 'Email 格式不正確'
      return ''
    })(),
    password: (() => {
      const failing = PASSWORD_RULES.filter(r => !r.test(form.password))
      if (failing.length > 0) return `密碼需${failing.map(r => r.label).join('、')}`
      return ''
    })(),
    confirmPassword: (() => {
      if (!form.confirmPassword) return '請再次輸入密碼'
      if (form.confirmPassword !== form.password) return '兩次密碼不一致'
      return ''
    })(),
  }

  const hasErrors = Object.values(errors).some(Boolean)

  const handleSubmit = async () => {
    setTouched({ username: true, email: true, password: true, confirmPassword: true })
    if (hasErrors) return

    setLoading(true)
    setApiError('')
    try {
      const res = await fetch(withApiPath('/setup'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          username: form.username,
          email: form.email,
          password: form.password,
        }),
      })
      if (res.status === 409) {
        setApiError('平台已完成設定，請直接登入。')
        return
      }
      if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        setApiError((body as { error?: string }).error ?? '建立帳號失敗，請重試。')
        return
      }
      onNext(form.username)
    } catch {
      setApiError('無法連接伺服器，請確認後端服務已啟動。')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex flex-col gap-5">
      <div>
        <h2 className="text-xl font-display font-black text-ink">設定管理員帳號</h2>
        <p className="text-xs text-muted mt-1">這是平台唯一的 Admin 帳號，用於首次登入後管理其他使用者。</p>
      </div>

      <FieldGroup label="使用者名稱" error={touched.username ? errors.username : ''}>
        <Input
          placeholder="e.g. admin"
          value={form.username}
          onChange={update('username')}
          onBlur={blur('username')}
          autoComplete="username"
        />
      </FieldGroup>

      <FieldGroup label="Email" error={touched.email ? errors.email : ''}>
        <Input
          type="email"
          placeholder="you@company.com"
          value={form.email}
          onChange={update('email')}
          onBlur={blur('email')}
          autoComplete="email"
        />
      </FieldGroup>

      <FieldGroup label="密碼" error={touched.password ? errors.password : ''}>
        <div className="relative">
          <Input
            type={showPassword ? 'text' : 'password'}
            placeholder="至少 8 位，含大小寫與數字"
            value={form.password}
            onChange={update('password')}
            onBlur={blur('password')}
            className="pr-10"
            autoComplete="new-password"
          />
          <button
            type="button"
            className="absolute right-3 top-1/2 -translate-y-1/2 text-faint hover:text-muted"
            onClick={() => setShowPassword(v => !v)}
          >
            {showPassword ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
          </button>
        </div>
        {/* Inline password strength requirements */}
        {form.password && (
          <div className="grid grid-cols-2 gap-1 mt-1">
            {PASSWORD_RULES.map(rule => (
              <span
                key={rule.label}
                className={cn(
                  'flex items-center gap-1 text-[11px]',
                  rule.test(form.password) ? 'text-success' : 'text-faint',
                )}
              >
                <CheckCircle2 className={cn('w-3 h-3', rule.test(form.password) ? 'text-success' : 'text-faint')} strokeWidth={2.5} />
                {rule.label}
              </span>
            ))}
          </div>
        )}
      </FieldGroup>

      <FieldGroup label="確認密碼" error={touched.confirmPassword ? errors.confirmPassword : ''}>
        <Input
          type={showPassword ? 'text' : 'password'}
          placeholder="再次輸入密碼"
          value={form.confirmPassword}
          onChange={update('confirmPassword')}
          onBlur={blur('confirmPassword')}
          autoComplete="new-password"
        />
      </FieldGroup>

      {apiError && (
        <div className="rounded-control border border-danger/30 bg-red-50 px-3 py-2 text-xs text-danger">
          {apiError}
        </div>
      )}

      <div className="flex justify-between pt-1">
        <Button variant="ghost" onClick={onBack}>← 返回</Button>
        <Button onClick={handleSubmit} disabled={loading}>
          {loading
            ? <><Loader2 className="w-4 h-4 animate-spin" /> 建立中...</>
            : '建立帳號 →'
          }
        </Button>
      </div>
    </div>
  )
}

function NotificationsStep({
  onNext,
  onBack,
}: {
  onNext: () => void
  onBack: () => void
}) {
  return (
    <div className="flex flex-col gap-5">
      <div>
        <h2 className="text-xl font-display font-black text-ink">通知設定 <span className="text-sm font-sans font-normal text-muted">（選填）</span></h2>
        <p className="text-xs text-muted mt-1">工單狀態變更時，透過 Lark Bot 通知相關人員。</p>
      </div>

      <div className="rounded-card border border-border bg-panel-soft p-4 flex flex-col gap-3">
        <div className="flex items-start gap-3">
          <div className="w-8 h-8 rounded-control bg-brand flex items-center justify-center shrink-0 mt-0.5">
            <svg viewBox="0 0 24 24" className="w-4 h-4 fill-white" xmlns="http://www.w3.org/2000/svg">
              <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1 14H9V8h2v8zm4 0h-2V8h2v8z"/>
            </svg>
          </div>
          <div>
            <p className="text-sm font-semibold text-ink">Lark Webhook</p>
            <p className="text-xs text-muted mt-0.5">
              在 Lark 群組中新增 Incoming Webhook Bot，取得 Webhook URL 後設定至環境變數。
            </p>
          </div>
        </div>

        <div className="rounded-control bg-panel-soft px-3 py-2 font-mono text-xs text-ink">
          LARK_WEBHOOK_URL=https://open.larksuite.com/open-apis/bot/v2/hook/...
        </div>

        <p className="text-[11px] text-muted">
          在 <code className="font-mono bg-page px-1 rounded">.env</code> 檔案中加入此變數，重啟服務後生效。也可於稍後在伺服器設定中調整。
        </p>
      </div>

      <div className="flex justify-between pt-1">
        <Button variant="ghost" onClick={onBack}>← 返回</Button>
        <Button onClick={onNext}>完成設定 →</Button>
      </div>
    </div>
  )
}

function CompleteStep({ username }: { username: string }) {
  const navigate = useNavigate()

  return (
    <div className="flex flex-col items-center text-center gap-6 py-4">
      <div className="w-16 h-16 rounded-full bg-success-soft flex items-center justify-center">
        <CheckCircle2 className="w-9 h-9 text-success" strokeWidth={2} />
      </div>

      <div className="flex flex-col gap-2">
        <h2 className="text-2xl font-display font-black text-ink">設定完成！</h2>
        <p className="text-muted text-sm max-w-xs">
          管理員帳號 <code className="font-mono bg-page px-1.5 py-0.5 rounded-pill text-ink text-xs">{username}</code> 已建立。
          現在可以登入開始使用 DBRE Maestro。
        </p>
      </div>

      <div className="flex flex-col gap-2 text-left text-xs text-muted bg-panel-soft border border-border rounded-card px-4 py-3 w-full max-w-xs">
        <p className="font-semibold text-ink text-[11px] uppercase tracking-wide">登入後的第一步</p>
        <ul className="flex flex-col gap-1">
          {[
            '邀請 DBA / 工單審核者加入',
            '在「資料庫資產」頁面新增目標資料庫',
            '設定敏感欄位脫敏規則',
          ].map((tip, i) => (
            <li key={i} className="flex items-start gap-1.5">
              <span className="text-faint mt-0.5">›</span> {tip}
            </li>
          ))}
        </ul>
      </div>

      <Button className="w-full max-w-xs h-10 text-base" onClick={() => navigate('/login')}>
        前往登入 →
      </Button>
    </div>
  )
}

// ────────────────────────────────────────────────────────────────────────────
// Main Wizard shell
// ────────────────────────────────────────────────────────────────────────────

export function SetupWizard() {
  const [step, setStep] = useState<Step>(0)
  const [adminUsername, setAdminUsername] = useState('')
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
          setSetupCompleted(false)
        }
      })

    return () => {
      active = false
    }
  }, [])

  if (setupCompleted === true) {
    return <Navigate to="/login" replace />
  }

  if (setupCompleted === null) {
    return (
      <div className="min-h-screen bg-page flex items-center justify-center px-4 py-10">
        <div className="rounded-card border border-border bg-panel px-6 py-5 shadow-soft">
          <p className="text-sm font-semibold text-ink">正在檢查平台初始化狀態…</p>
          <p className="mt-1 text-xs text-muted">請稍候，系統正在確認是否仍可進入 Setup Wizard。</p>
        </div>
      </div>
    )
  }

  const next = () => setStep(s => (s < 3 ? (s + 1) as Step : s))
  const back = () => setStep(s => (s > 0 ? (s - 1) as Step : s))

  return (
    <div className="min-h-screen bg-page flex items-center justify-center px-4 py-10">
      <div className="w-full max-w-md">
        {/* Card */}
        <div className="bg-panel rounded-card border border-border shadow-card px-8 py-8 flex flex-col gap-6">
          {/* Header */}
          <div className="flex items-center justify-between">
            <span className="text-xs font-bold text-faint tracking-widest uppercase">DBRE Maestro</span>
            <StepProgress current={step} />
          </div>

          <div className="h-px bg-border -mx-8" />

          {/* Step content */}
          <div className="min-h-[320px]">
            {step === 0 && <WelcomeStep onNext={next} />}
            {step === 1 && (
              <AccountStep
                onNext={(username) => { setAdminUsername(username); next() }}
                onBack={back}
              />
            )}
            {step === 2 && <NotificationsStep onNext={next} onBack={back} />}
            {step === 3 && <CompleteStep username={adminUsername} />}
          </div>
        </div>

        {/* Footer */}
        <p className="text-center text-[11px] text-faint mt-4">
          DBRE Maestro — 內部資料庫安全治理平台
        </p>
      </div>
    </div>
  )
}
