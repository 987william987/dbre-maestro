import { useEffect, useMemo, useRef, useState } from 'react'
import { Check, ChevronDown, Search, X } from 'lucide-react'
import { cn } from '@/lib/utils'

export function SearchableMultiSelect({
  values,
  options,
  onChange,
  disabled = false,
  ariaLabel,
	emptyLabel = 'All tables',
}: {
  values: string[]
  options: string[]
  onChange: (values: string[]) => void
  disabled?: boolean
  ariaLabel: string
	emptyLabel?: string
}) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const containerRef = useRef<HTMLDivElement | null>(null)
  const searchRef = useRef<HTMLInputElement | null>(null)
  const filtered = useMemo(() => {
    const normalized = query.trim().toLowerCase()
    return normalized ? options.filter((option) => option.toLowerCase().includes(normalized)) : options
  }, [options, query])

  useEffect(() => {
    function closeOnOutsideClick(event: MouseEvent) {
      if (!containerRef.current?.contains(event.target as Node)) setOpen(false)
    }
    function closeOnEscape(event: KeyboardEvent) {
      if (event.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', closeOnOutsideClick)
    document.addEventListener('keydown', closeOnEscape)
    return () => {
      document.removeEventListener('mousedown', closeOnOutsideClick)
      document.removeEventListener('keydown', closeOnEscape)
    }
  }, [])

  useEffect(() => {
    if (!open) setQuery('')
    else window.requestAnimationFrame(() => searchRef.current?.focus())
  }, [open])

  function toggle(value: string) {
    onChange(values.includes(value) ? values.filter((item) => item !== value) : [...values, value])
  }

  return (
    <div ref={containerRef} className="relative min-w-0">
      <button
        type="button"
        aria-label={ariaLabel}
        aria-haspopup="listbox"
        aria-expanded={open}
        disabled={disabled}
        onClick={() => setOpen((current) => !current)}
        className={cn('flex h-10 w-full min-w-0 items-center justify-between rounded-lg border border-border bg-panel px-3 text-left text-[12px] font-medium transition disabled:cursor-not-allowed disabled:opacity-60', open ? 'border-border-strong' : 'hover:border-border-strong')}
      >
        <span className={cn('min-w-0 flex-1 truncate pr-3', values.length ? 'text-ink' : 'text-muted')}>
          {values.length === 0 ? emptyLabel : values.length === 1 ? values[0] : `${values.length} tables selected`}
        </span>
        <ChevronDown className={cn('h-4 w-4 shrink-0 text-faint transition-transform', open && 'rotate-180')} />
      </button>
      {values.length > 0 ? (
        <div className="mt-2 flex flex-wrap gap-1.5" aria-label="Selected tables">
          {values.map((value) => <span key={value} className="inline-flex max-w-full items-center gap-1 rounded-md bg-panel-soft px-2 py-1 text-[11px] text-ink"><span className="truncate">{value}</span><button type="button" aria-label={`Remove table ${value}`} onClick={() => toggle(value)} className="text-muted hover:text-ink"><X className="h-3 w-3" /></button></span>)}
        </div>
      ) : null}
      {open ? (
        <div className="absolute left-0 top-[calc(100%+8px)] z-30 w-full min-w-[260px] overflow-hidden rounded-lg border border-border bg-panel p-2 shadow-card">
          <div className="relative mb-2 flex h-9 items-center rounded-lg border border-border bg-panel-soft focus-within:border-border-strong">
            <Search className="pointer-events-none absolute left-3 h-3.5 w-3.5 text-faint" />
            <input ref={searchRef} value={query} onChange={(event) => setQuery(event.target.value)} aria-label={`${ariaLabel} search`} placeholder="Search tables..." className="h-full w-full bg-transparent pl-8 pr-3 text-[12px] text-ink outline-none" />
          </div>
          <div role="listbox" aria-label={`${ariaLabel} options`} aria-multiselectable="true" className="grid max-h-[280px] gap-0.5 overflow-y-auto">
            {filtered.length === 0 ? <p className="px-3 py-2 text-[12px] text-muted">No results</p> : null}
            {filtered.map((option) => {
              const selected = values.includes(option)
              return <button key={option} type="button" role="option" aria-selected={selected} onClick={() => toggle(option)} className={cn('flex items-center justify-between rounded-md px-3 py-2 text-left text-[12px] font-medium text-ink hover:bg-panel-soft/70', selected && 'bg-panel-soft')}><span className="break-all pr-3">{option}</span>{selected ? <Check className="h-4 w-4 shrink-0" /> : null}</button>
            })}
          </div>
        </div>
      ) : null}
    </div>
  )
}
