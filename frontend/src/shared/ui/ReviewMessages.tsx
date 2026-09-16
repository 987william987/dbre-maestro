import { useState } from 'react'

type ReviewMessagesProps = {
  value?: string | null
  empty?: string
}

function reviewMessageTone(message: string) {
  if (message.toLowerCase().startsWith('[error]')) {
    return 'text-danger'
  }
  if (message.toLowerCase().startsWith('[warn]')) {
    return 'text-amber-700'
  }
  return 'text-muted'
}

export function ReviewMessages({ value, empty = '—' }: ReviewMessagesProps) {
  const [expanded, setExpanded] = useState(false)
  const lines = (value ?? '')
    .split(/\n|\s\|\s/)
    .map((item) => item.trim())
    .filter(Boolean)
  if (lines.length === 0) {
    return <span>{empty}</span>
  }

  const shouldCollapse = lines.length > 1 || lines.some((line) => line.length > 80)
  return (
    <div className="w-full min-w-0 max-w-full">
      <div className={`text-[13px] leading-6 ${shouldCollapse && !expanded ? 'truncate whitespace-nowrap' : 'max-h-80 overflow-auto whitespace-pre-wrap break-words'}`}>
        {lines.map((line, index) => (
          <span key={`${line}-${index}`} className={`block ${reviewMessageTone(line)}`}>{line}</span>
        ))}
      </div>
      {shouldCollapse ? (
        <button
          type="button"
          onClick={() => setExpanded((current) => !current)}
          className="mt-2 inline-flex items-center gap-1 text-[11px] font-semibold text-primary"
          aria-expanded={expanded}
          aria-label={`${expanded ? 'Collapse' : 'Show full'} review messages`}
        >
          {expanded ? 'Collapse message' : 'Show full message'}
        </button>
      ) : null}
    </div>
  )
}
