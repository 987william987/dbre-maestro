import { useState, useCallback, useEffect } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { Eye, EyeOff, CheckCircle2, Circle, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { withApiPath } from '@/shared/api/client'
import { getSetupStatus } from '@/shared/setup/api'

// ────────────────────────────────────────────────────────────────────────────
// Types
// ────────────────────────────────────────────────────────────────────────────

type Step = 0 | 1 | 2  // welcome | account | complete

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

const STEP_LABELS = ['Welcome', 'Admin account', 'Done'] as const

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
              ? <CheckCircle2 className="h-4 w-4 text-success" strokeWidth={2.5} />
              : <Circle className={cn('h-4 w-4', i === current ? 'text-accent' : 'text-border-strong')} strokeWidth={2.5} />
            }
            <span className="hidden sm:inline">{label}</span>
          </div>
          {i < STEP_LABELS.length - 1 && (
            <div className={cn(
              'mx-1 h-px w-6 transition-colors',
              i < current ? 'bg-success' : 'bg-border',
            )} />
          )}
        </div>
      ))}
    </div>
  )
}

function StepShell({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <div className={cn('flex w-full flex-col', className)}>
      {children}
    </div>
  )
}

function ActionRow({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex flex-col-reverse gap-3 pt-2 sm:flex-row sm:items-center sm:justify-between">
      {children}
    </div>
  )
}

function PasswordVisibilityButton({
  show,
  onClick,
}: {
  show: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      className="absolute right-3 top-1/2 -translate-y-1/2 text-faint hover:text-muted"
      onClick={onClick}
      aria-label={show ? 'Hide password' : 'Show password'}
    >
      {show ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
    </button>
  )
}

function PasswordRules({ password }: { password: string }) {
  if (!password) {
    return null
  }

  return (
    <div className="mt-1 grid gap-1 sm:grid-cols-2">
      {PASSWORD_RULES.map(rule => {
        const passed = rule.test(password)
        return (
          <span
            key={rule.label}
            className={cn(
              'flex min-w-0 items-center gap-1 text-[11px]',
              passed ? 'text-success' : 'text-faint',
            )}
          >
            <CheckCircle2 className={cn('h-3 w-3 shrink-0', passed ? 'text-success' : 'text-faint')} strokeWidth={2.5} />
            <span className="min-w-0 truncate">{rule.label}</span>
          </span>
        )
      })}
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
    <StepShell className="items-center gap-6 py-4 text-center">
      <div className="flex h-16 w-16 items-center justify-center rounded-card bg-brand shadow-card">
        <span className="font-display text-2xl font-black text-white">M</span>
      </div>

      <div className="flex flex-col gap-2">
        <h1 className="font-display text-2xl font-black tracking-tight text-ink">
          Welcome to DBRE Maestro
        </h1>
        <p className="max-w-sm text-sm text-muted">
          Database security governance — unified ticket review, query control, and audit logging.
        </p>
      </div>

      <ul className="flex w-full max-w-xs flex-col gap-2 text-left text-sm text-muted">
        {[
          'Centralized DDL/DML ticket review workflow',
          'Automatic query masking for sensitive columns',
          'Full audit log — every action is recorded',
        ].map((item, i) => (
          <li key={i} className="flex items-start gap-2">
            <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-success" strokeWidth={2.5} />
            <span>{item}</span>
          </li>
        ))}
      </ul>

      <Button className="h-10 w-full max-w-xs text-base" onClick={onNext}>
        Get started →
      </Button>
    </StepShell>
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
    <StepShell className="gap-5">
      <div>
        <h2 className="font-display text-xl font-black text-ink">Set up admin account</h2>
        <p className="mt-1 text-xs text-muted">Create the first Admin account. It can manage users, permissions, database assets, and platform settings.</p>
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
          <PasswordVisibilityButton show={showPassword} onClick={() => setShowPassword(v => !v)} />
        </div>
        <PasswordRules password={form.password} />
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

      <ActionRow>
        <Button variant="ghost" onClick={onBack}>← Back</Button>
        <Button onClick={handleSubmit} disabled={loading}>
          {loading
            ? <><Loader2 className="h-4 w-4 animate-spin" /> Creating…</>
            : 'Create account →'
          }
        </Button>
      </ActionRow>
    </StepShell>
  )
}

function CompleteStep({ username }: { username: string }) {
  const navigate = useNavigate()

  return (
    <StepShell className="items-center gap-6 py-4 text-center">
      <div className="flex h-16 w-16 items-center justify-center rounded-full bg-success-soft">
        <CheckCircle2 className="h-9 w-9 text-success" strokeWidth={2} />
      </div>

      <div className="flex flex-col gap-2">
        <h2 className="font-display text-2xl font-black text-ink">Setup complete!</h2>
        <p className="max-w-xs text-sm text-muted">
          Admin account <code className="font-mono bg-page px-1.5 py-0.5 rounded-pill text-ink text-xs">{username}</code> has been created.
          You can now sign in and start using DBRE Maestro.
        </p>
      </div>

      <div className="flex w-full max-w-xs flex-col gap-2 rounded-card border border-border bg-panel-soft px-4 py-3 text-left text-xs text-muted">
        <p className="text-[11px] font-semibold uppercase tracking-wide text-ink">First steps after login</p>
        <ul className="flex flex-col gap-1">
          {[
            'Invite DBAs and ticket reviewers',
            'Add target databases in the Database Assets page',
            'Configure masking rules for sensitive columns',
          ].map((tip, i) => (
            <li key={i} className="flex items-start gap-1.5">
              <span className="mt-0.5 shrink-0 text-faint">›</span>
              <span>{tip}</span>
            </li>
          ))}
        </ul>
      </div>

      <Button className="h-10 w-full max-w-xs text-base" onClick={() => navigate('/login')}>
        Go to login →
      </Button>
    </StepShell>
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

  const next = () => setStep(s => (s < 2 ? (s + 1) as Step : s))
  const back = () => setStep(s => (s > 0 ? (s - 1) as Step : s))

  return (
    <div className="flex min-h-screen flex-col items-center justify-center bg-page px-4 py-12">
      <div className="mb-6 text-center">
        <h1 className="font-display text-2xl font-black tracking-tight text-ink">DBRE Maestro</h1>
      </div>

      <div className="flex w-full max-w-md flex-col gap-6 rounded-card border border-border bg-panel px-8 py-8 shadow-card">
        <div className="flex justify-end">
          <StepProgress current={step} />
        </div>

        <div className="-mx-8 h-px bg-border" />

        <div className="min-h-[320px]">
          {step === 0 && <WelcomeStep onNext={next} />}
          {step === 1 && (
            <AccountStep
              onNext={(username) => { setAdminUsername(username); setStep(2) }}
              onBack={back}
            />
          )}
          {step === 2 && <CompleteStep username={adminUsername} />}
        </div>
      </div>
    </div>
  )
}
