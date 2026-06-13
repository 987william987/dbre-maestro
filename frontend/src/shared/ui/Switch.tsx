import { cn } from '@/lib/utils'

type SwitchProps = {
  checked: boolean
  onChange: (checked: boolean) => void
  disabled?: boolean
  ariaLabel?: string
  className?: string
}

export function Switch({ checked, onChange, disabled = false, ariaLabel, className }: SwitchProps) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={ariaLabel}
      disabled={disabled}
      onClick={() => {
        if (!disabled) {
          onChange(!checked)
        }
      }}
      className={cn(
        'relative inline-flex h-5 w-10 shrink-0 items-center rounded-full border transition-[background-color,border-color,box-shadow] duration-200 ease-out',
        checked
          ? 'border-ink bg-ink shadow-[inset_0_0_0_1px_rgba(24,24,27,0.03)]'
          : 'border-border bg-zinc-200 hover:border-border-strong',
        disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer',
        className,
      )}
    >
      <span
        className={cn(
          'pointer-events-none inline-block h-3.5 w-3.5 rounded-full bg-white shadow-[0_1px_2px_rgba(0,0,0,0.16)] transition-transform duration-200 ease-out',
          checked ? 'translate-x-5' : 'translate-x-1',
        )}
      />
    </button>
  )
}
