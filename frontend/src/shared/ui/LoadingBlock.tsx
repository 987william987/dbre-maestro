type LoadingBlockProps = {
  message: string
  className?: string
}

export function LoadingBlock({ message, className }: LoadingBlockProps) {
  return (
    <div className={`flex items-center justify-center rounded-card border border-border bg-panel ${className ?? 'min-h-[240px]'}`}>
      <p className="text-sm font-semibold text-muted">{message}</p>
    </div>
  )
}
