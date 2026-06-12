type PaginationProps = {
  offset: number
  pageSize: number
  count: number
  total?: number
  onChange: (nextOffset: number) => void
}

export function Pagination({ offset, pageSize, count, total, onChange }: PaginationProps) {
  const from = count === 0 ? 0 : offset + 1
  const to = offset + count
  const hasPrev = offset > 0
  const hasNext = total != null ? offset + pageSize < total : count === pageSize

  if (!hasPrev && !hasNext && offset === 0) {
    return null
  }

  return (
    <div className="flex items-center justify-between px-1">
      <p className="text-[12px] text-muted">
        {total != null ? `Showing ${from}–${Math.min(to, total)} of ${total}` : `Showing ${from}–${to}`}
      </p>
      <div className="flex gap-2">
        <button
          type="button"
          disabled={!hasPrev}
          onClick={() => onChange(Math.max(0, offset - pageSize))}
          className="inline-flex h-9 items-center justify-center rounded-md border border-border bg-panel px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:opacity-50"
        >
          Previous
        </button>
        <button
          type="button"
          disabled={!hasNext}
          onClick={() => onChange(offset + pageSize)}
          className="inline-flex h-9 items-center justify-center rounded-md border border-border bg-panel px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:opacity-50"
        >
          Next
        </button>
      </div>
    </div>
  )
}
