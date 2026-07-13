import type { InputHTMLAttributes } from 'react'
import { Search } from 'lucide-react'
import { cn } from '@/lib/utils'

export function SearchInput({
  className,
  wrapperClassName,
  iconClassName,
  ...props
}: InputHTMLAttributes<HTMLInputElement> & {
  wrapperClassName?: string
  iconClassName?: string
}) {
  return (
    <div className={cn('flex h-9 items-center gap-2 rounded-2xl border border-border bg-white px-3 shadow-soft transition focus-within:border-slate-400', wrapperClassName)}>
      <Search className={cn('h-4 w-4 text-faint', iconClassName)} />
      <input
        className={cn('h-full w-full bg-transparent text-[13px] text-ink outline-none placeholder:text-muted', className)}
        {...props}
      />
    </div>
  )
}
