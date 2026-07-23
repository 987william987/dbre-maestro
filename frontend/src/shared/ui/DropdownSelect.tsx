import { useEffect, useMemo, useRef, useState } from 'react'
import { Check, ChevronDown } from 'lucide-react'
import { cn } from '@/lib/utils'

export type DropdownOption = {
  value: string
  label: string
}

export type DropdownOptionGroup = {
  label: string
  options: ReadonlyArray<DropdownOption>
}

type DropdownSelectItem = DropdownOption | DropdownOptionGroup

function isOptionGroup(item: DropdownSelectItem): item is DropdownOptionGroup {
  return 'options' in item
}

export function DropdownSelect({
  value,
  onChange,
  options,
  disabled = false,
  ariaLabel,
  placeholder,
  align = 'left',
  size = 'md',
  className,
  triggerClassName,
  menuClassName,
  optionClassName,
}: {
  value: string
  onChange: (value: string) => void
  options: ReadonlyArray<DropdownSelectItem>
  disabled?: boolean
  ariaLabel: string
  placeholder?: string
  align?: 'left' | 'right'
  size?: 'md' | 'sm'
  className?: string
  triggerClassName?: string
  menuClassName?: string
  optionClassName?: string
}) {
  const [open, setOpen] = useState(false)
  const containerRef = useRef<HTMLDivElement | null>(null)

  const selectedOption = useMemo(
    () => {
      for (const item of options) {
        if (isOptionGroup(item)) {
          const selected = item.options.find((option) => option.value === value)
          if (selected) {
            return selected
          }
        } else if (item.value === value) {
          return item
        }
      }
      return null
    },
    [options, value],
  )
  const displayAsPlaceholder = !selectedOption || selectedOption.value === '' || selectedOption.value === 'all'

  useEffect(() => {
    function handlePointerDown(event: MouseEvent) {
      const target = event.target as Node
      if (!containerRef.current?.contains(target)) {
        setOpen(false)
      }
    }

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        setOpen(false)
      }
    }

    document.addEventListener('mousedown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('mousedown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [])

  return (
    <div ref={containerRef} className={cn('relative', className)}>
      <button
        type="button"
        aria-label={ariaLabel}
        aria-haspopup="listbox"
        aria-expanded={open}
        disabled={disabled}
        onClick={() => setOpen((current) => !current)}
        className={cn(
          'flex w-full items-center justify-between rounded-lg border border-border bg-white text-left transition disabled:cursor-not-allowed disabled:opacity-60',
          size === 'md'
            ? 'h-9 px-3 text-[12px] font-medium'
            : 'h-9 px-3 text-[12px] font-medium',
          open ? 'border-slate-300' : 'hover:border-slate-300',
          triggerClassName,
        )}
      >
        <span className={cn('truncate pr-3', displayAsPlaceholder ? 'text-muted' : 'text-ink')}>{selectedOption?.label ?? placeholder ?? 'Select'}</span>
        <ChevronDown className={cn('h-4 w-4 shrink-0 text-faint transition-transform', open && 'rotate-180')} />
      </button>

      {open ? (
        <div
          className={cn(
            'absolute top-[calc(100%+8px)] z-30 w-full overflow-hidden rounded-xl border border-border bg-white p-2 shadow-[0_22px_45px_rgba(15,23,42,0.14)]',
            align === 'right' ? 'right-0' : 'left-0',
            menuClassName,
          )}
        >
          <div role="listbox" aria-label={`${ariaLabel} options`} className="grid max-h-[360px] gap-0.5 overflow-y-auto">
            {options.map((item) => {
              if (isOptionGroup(item)) {
                return (
                  <div key={item.label} className="border-t border-border first:border-t-0">
                    <p className="px-3 pb-0.5 pt-2 text-[12px] font-semibold text-muted first:pt-1">{item.label}</p>
                    <div className="grid gap-0.5 pb-1.5">
                      {item.options.map((option) => {
                        const selected = option.value === value
                        return (
                          <button
                            key={option.value}
                            type="button"
                            role="option"
                            aria-selected={selected}
                            onClick={() => {
                              onChange(option.value)
                              setOpen(false)
                            }}
                            className={cn(
                              'flex items-center justify-between rounded-md px-3 py-1.5 text-left text-[12px] font-medium transition',
                              selected ? 'bg-panel-soft text-ink' : 'text-ink hover:bg-panel-soft/70',
                              optionClassName,
                            )}
                          >
                            <span className="break-words pr-3">{option.label}</span>
                            {selected ? <Check className="h-4 w-4 shrink-0" /> : null}
                          </button>
                        )
                      })}
                    </div>
                  </div>
                )
              }
              const option = item
              const selected = option.value === value
              return (
                <button
                  key={option.value}
                  type="button"
                  role="option"
                  aria-selected={selected}
                  onClick={() => {
                    onChange(option.value)
                    setOpen(false)
                  }}
                  className={cn(
                    'flex items-center justify-between rounded-md px-3 py-1.5 text-left text-[12px] font-medium transition',
                    selected ? 'bg-panel-soft text-ink' : 'text-ink hover:bg-panel-soft/70',
                    optionClassName,
                  )}
                >
                  <span className="break-words pr-3">{option.label}</span>
                  {selected ? <Check className="h-4 w-4 shrink-0" /> : null}
                </button>
              )
            })}
          </div>
        </div>
      ) : null}
    </div>
  )
}
