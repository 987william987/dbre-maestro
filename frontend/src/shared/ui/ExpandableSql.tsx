import { useState } from 'react'
import { ChevronDown } from 'lucide-react'

type ExpandableSqlProps = {
  value: string
  label?: string
}

export function ExpandableSql({ value, label = 'SQL' }: ExpandableSqlProps) {
  const [expanded, setExpanded] = useState(false)
  const shouldCollapse = value.length > 120 || value.includes('\n')

  return (
    <div className="w-[520px] max-w-[52vw] min-w-[260px]">
      <pre className={`w-full max-w-full font-mono text-[12px] leading-6 text-ink ${
        shouldCollapse && !expanded
          ? 'truncate whitespace-nowrap'
          : 'max-h-80 overflow-auto whitespace-pre-wrap break-words'
      }`}>
        {value}
      </pre>
      {shouldCollapse ? (
        <button
          type="button"
          onClick={() => setExpanded((current) => !current)}
          className="mt-2 inline-flex items-center gap-1 text-[11px] font-semibold text-primary"
          aria-expanded={expanded}
          aria-label={`${expanded ? 'Collapse' : 'Show full'} ${label}`}
        >
          {expanded ? 'Collapse SQL' : 'Show full SQL'}
          <ChevronDown className={`h-3.5 w-3.5 transition-transform ${expanded ? 'rotate-180' : ''}`} />
        </button>
      ) : null}
    </div>
  )
}
