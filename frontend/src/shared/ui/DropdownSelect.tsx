import { useEffect, useMemo, useRef, useState } from 'react'
import { Check, ChevronDown, Search } from 'lucide-react'
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

function optionMatchesQuery(option: DropdownOption, query: string) {
  const normalizedQuery = query.trim().toLowerCase()
  if (normalizedQuery === '') {
    return true
  }
  return option.label.toLowerCase().includes(normalizedQuery) || option.value.toLowerCase().includes(normalizedQuery)
}

function filterDropdownOptions(options: ReadonlyArray<DropdownSelectItem>, query: string): DropdownSelectItem[] {
  if (query.trim() === '') {
    return [...options]
  }
  return options.flatMap<DropdownSelectItem>((item) => {
    if (!isOptionGroup(item)) {
      return optionMatchesQuery(item, query) ? [item] : []
    }
    const nextOptions = item.options.filter((option) => optionMatchesQuery(option, query))
    return nextOptions.length > 0 ? [{ ...item, options: nextOptions }] : []
  })
}

function countDropdownOptions(options: ReadonlyArray<DropdownSelectItem>) {
  return options.reduce((total, item) => total + (isOptionGroup(item) ? item.options.length : 1), 0)
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
  searchable,
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
  searchable?: boolean
}) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const containerRef = useRef<HTMLDivElement | null>(null)
  const searchInputRef = useRef<HTMLInputElement | null>(null)

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
  const optionCount = useMemo(() => countDropdownOptions(options), [options])
  const showSearch = searchable ?? optionCount > 8
  const filteredOptions = useMemo(() => filterDropdownOptions(options, query), [options, query])

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

  useEffect(() => {
    if (!open) {
      setQuery('')
      return
    }
    if (showSearch) {
      window.requestAnimationFrame(() => searchInputRef.current?.focus())
    }
  }, [open, showSearch])

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
          {showSearch ? (
            <div className="relative mb-2 flex h-9 items-center rounded-lg border border-border bg-panel-soft transition focus-within:border-slate-300 focus-within:bg-white">
              <Search className="pointer-events-none absolute left-3 h-3.5 w-3.5 text-faint" />
              <input
                ref={searchInputRef}
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                aria-label={`${ariaLabel} search`}
                className="h-full w-full rounded-lg border-0 bg-transparent pl-8 pr-3 text-[12px] font-medium text-ink outline-none placeholder:text-muted"
                placeholder="Search..."
              />
            </div>
          ) : null}
          <div role="listbox" aria-label={`${ariaLabel} options`} className="grid max-h-[360px] gap-0.5 overflow-y-auto">
            {filteredOptions.length === 0 ? (
              <p className="px-3 py-2 text-[12px] font-medium text-muted">No results</p>
            ) : null}
            {filteredOptions.map((item) => {
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
