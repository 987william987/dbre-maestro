import type { HTMLAttributes, TableHTMLAttributes, TdHTMLAttributes, ThHTMLAttributes } from 'react'
import { ArrowDown, ArrowUp } from 'lucide-react'
import { cn } from '@/lib/utils'

export type DataTableSortDirection = 'asc' | 'desc'

export function DataTableSurface({ className, ...props }: HTMLAttributes<HTMLElement>) {
  return (
    <section
      className={cn('overflow-hidden rounded-xl border border-border bg-panel shadow-soft', className)}
      {...props}
    />
  )
}

export function DataTableContent({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('grid gap-3 px-3 pb-3', className)} {...props} />
}

export function DataTableScroll({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('overflow-x-auto', className)} {...props} />
}

export function DataTable({ className, ...props }: TableHTMLAttributes<HTMLTableElement>) {
  return <table translate="no" className={cn('min-w-full border-collapse', className)} {...props} />
}

export function DataTableHead({ className, ...props }: HTMLAttributes<HTMLTableSectionElement>) {
  return (
    <thead
      className={cn('bg-editor-toolbar text-left text-[11px] font-semibold text-faint', className)}
      {...props}
    />
  )
}

export function DataTableHeaderCell({ className, ...props }: ThHTMLAttributes<HTMLTableCellElement>) {
  return <th className={cn('whitespace-nowrap align-middle px-3 py-3 leading-4', className)} {...props} />
}

export function SortableDataTableHeaderCell<TSortKey extends string>({
  label,
  sortKey,
  sortState,
  onSort,
  className,
}: {
  label: string
  sortKey: TSortKey
  sortState: { key: TSortKey; direction: DataTableSortDirection }
  onSort: (key: TSortKey) => void
  className?: string
}) {
  const active = sortState.key === sortKey
  return (
    <DataTableHeaderCell className={className}>
      <button
        type="button"
        aria-label={active ? `${label} ${sortState.direction.toUpperCase()}` : label}
        onClick={() => onSort(sortKey)}
        className={cn('inline-flex items-center gap-1 text-left uppercase tracking-[0.16em] transition hover:text-ink', active ? 'text-ink' : 'text-faint')}
      >
        {label}
        {active ? sortState.direction === 'desc' ? <ArrowDown className="h-3 w-3" aria-hidden="true" /> : <ArrowUp className="h-3 w-3" aria-hidden="true" /> : null}
      </button>
    </DataTableHeaderCell>
  )
}

export function DataTableBody({ className, ...props }: HTMLAttributes<HTMLTableSectionElement>) {
  return <tbody className={className} {...props} />
}

export function DataTableRow({ className, ...props }: HTMLAttributes<HTMLTableRowElement>) {
  return (
    <tr
      className={cn('border-t border-border text-[12px] font-normal text-ink hover:bg-slate-50/70', className)}
      {...props}
    />
  )
}

export function DataTableCell({ className, ...props }: TdHTMLAttributes<HTMLTableCellElement>) {
  return <td className={cn('px-3 py-2 align-middle', className)} {...props} />
}
