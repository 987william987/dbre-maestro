import type { HTMLAttributes, TableHTMLAttributes, TdHTMLAttributes, ThHTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

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
  return <table className={cn('min-w-full border-collapse', className)} {...props} />
}

export function DataTableHead({ className, ...props }: HTMLAttributes<HTMLTableSectionElement>) {
  return (
    <thead
      className={cn('bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint', className)}
      {...props}
    />
  )
}

export function DataTableHeaderCell({ className, ...props }: ThHTMLAttributes<HTMLTableCellElement>) {
  return <th className={cn('whitespace-nowrap align-middle px-3 py-3', className)} {...props} />
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
