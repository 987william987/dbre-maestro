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
  { label: 'At least 8 characters', test: p => p.length >= 8 },
  { label: 'Uppercase letter',       test: p => /[A-Z]/.test(p) },
  { label: 'Lowercase letter',       test: p => /[a-z]/.test(p) },
  { label: 'Number',                 test: p => /[0-9]/.test(p) },
]

const STEP_LABELS = ['Welcome', 'Admin account', 'Notifications', 'Done'] as const

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
          Welcome to DBRE Maestro
        </h1>
        <p className="text-muted text-sm max-w-sm">
          Database security governance — unified ticket review, query control, and audit logging.
        </p>
      </div>

      <ul className="flex flex-col gap-2 text-left text-sm text-muted w-full max-w-xs">
        {[
          'Centralized DDL/DML ticket review workflow',
          'Automatic query masking for sensitive columns',
          'Full audit log — every action is recorded',
        ].map((item, i) => (
          <li key={i} className="flex items-start gap-2">
            <CheckCircle2 className="w-4 h-4 text-success mt-0.5 shrink-0" strokeWidth={2.5} />
            <span>{item}</span>
          </li>
        ))}
      </ul>

      <Button className="w-full max-w-xs h-10 text-base" onClick={onNext}>
        Get started →
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
      if (!form.username) return 'Username is required'
      if (form.username.length < 3 || form.username.length > 64) return 'Must be 3–64 characters'
      if (!/^[a-zA-Z0-9_]+$/.test(form.username)) return 'Only letters, numbers, and underscores'
      return ''
    })(),
    email: (() => {
      if (!form.email) return 'Email is required'
      if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(form.email)) return 'Invalid email address'
      return ''
    })(),
    password: (() => {
      const failing = PASSWORD_RULES.filter(r => !r.test(form.password))
      if (failing.length > 0) return `Password must include: ${failing.map(r => r.label).join(', ')}`
      return ''
    })(),
    confirmPassword: (() => {
      if (!form.confirmPassword) return 'Please confirm your password'
      if (form.confirmPassword !== form.password) return 'Passwords do not match'
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
        setApiError('The platform is already set up. Please log in directly.')
        return
      }
      if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        setApiError((body as { error?: string }).error ?? 'Failed to create account. Please try again.')
        return
      }
      onNext(form.username)
    } catch {
      setApiError('Cannot reach the server. Please ensure the backend service is running.')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex flex-col gap-5">
      <div>
        <h2 className="text-xl font-display font-black text-ink">Set up admin account</h2>
        <p className="text-xs text-muted mt-1">This is the platform's sole Admin account, used to manage other users after first login.</p>
      </div>

      <FieldGroup label="Username" error={touched.username ? errors.username : ''}>
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

      <FieldGroup label="Password" error={touched.password ? errors.password : ''}>
        <div className="relative">
          <Input
            type={showPassword ? 'text' : 'password'}
            placeholder="Min. 8 chars with upper, lower & number"
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

      <FieldGroup label="Confirm password" error={touched.confirmPassword ? errors.confirmPassword : ''}>
        <Input
          type={showPassword ? 'text' : 'password'}
          placeholder="Re-enter your password"
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
        <Button variant="ghost" onClick={onBack}>← Back</Button>
        <Button onClick={handleSubmit} disabled={loading}>
          {loading
            ? <><Loader2 className="w-4 h-4 animate-spin" /> Creating…</>
            : 'Create account →'
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
        <h2 className="text-xl font-display font-black text-ink">Notifications <span className="text-sm font-sans font-normal text-muted">(optional)</span></h2>
        <p className="text-xs text-muted mt-1">Notify team members via Lark Bot when ticket status changes.</p>
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
              Add an Incoming Webhook Bot to your Lark group, then set the Webhook URL as an environment variable.
            </p>
          </div>
        </div>

        <div className="rounded-control bg-panel-soft px-3 py-2 font-mono text-xs text-ink">
          LARK_WEBHOOK_URL=https://open.larksuite.com/open-apis/bot/v2/hook/...
        </div>

        <p className="text-[11px] text-muted">
          Add this variable to your <code className="font-mono bg-page px-1 rounded">.env</code> file and restart the service. You can also configure it later in server settings.
        </p>
      </div>

      <div className="flex justify-between pt-1">
        <Button variant="ghost" onClick={onBack}>← Back</Button>
        <Button onClick={onNext}>Finish setup →</Button>
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
        <h2 className="text-2xl font-display font-black text-ink">Setup complete!</h2>
        <p className="text-muted text-sm max-w-xs">
          Admin account <code className="font-mono bg-page px-1.5 py-0.5 rounded-pill text-ink text-xs">{username}</code> has been created.
          You can now sign in and start using DBRE Maestro.
        </p>
      </div>

      <div className="flex flex-col gap-2 text-left text-xs text-muted bg-panel-soft border border-border rounded-card px-4 py-3 w-full max-w-xs">
        <p className="font-semibold text-ink text-[11px] uppercase tracking-wide">First steps after login</p>
        <ul className="flex flex-col gap-1">
          {[
            'Invite DBAs and ticket reviewers',
            'Add target databases in the Database Assets page',
            'Configure masking rules for sensitive columns',
          ].map((tip, i) => (
            <li key={i} className="flex items-start gap-1.5">
              <span className="text-faint mt-0.5">›</span> {tip}
            </li>
          ))}
        </ul>
      </div>

      <Button className="w-full max-w-xs h-10 text-base" onClick={() => navigate('/login')}>
        Go to login →
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
      <div className="flex min-h-screen flex-col items-center justify-center bg-page px-4 py-12">
        <div className="mb-6 text-center">
          <h1 className="font-display text-2xl font-black tracking-tight text-ink">DBRE Maestro</h1>
        </div>
        <div className="w-full max-w-md rounded-card border border-border bg-panel px-8 py-6 shadow-card">
          <p className="text-sm font-semibold text-ink">Checking platform status…</p>
          <p className="mt-1 text-xs text-muted">Please wait while the system verifies whether the Setup Wizard is still available.</p>
        </div>
      </div>
    )
  }

  const next = () => setStep(s => (s < 3 ? (s + 1) as Step : s))
  const back = () => setStep(s => (s > 0 ? (s - 1) as Step : s))

  return (
    <div className="flex min-h-screen flex-col items-center justify-center bg-page px-4 py-12">
      <div className="mb-6 text-center">
        <h1 className="font-display text-2xl font-black tracking-tight text-ink">DBRE Maestro</h1>
      </div>

      <div className="w-full max-w-md rounded-card border border-border bg-panel px-8 py-8 shadow-card flex flex-col gap-6">
        {/* Step progress */}
        <div className="flex justify-end">
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
    </div>
  )
}
