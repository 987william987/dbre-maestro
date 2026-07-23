import { useState } from 'react'
import { ChevronDown } from 'lucide-react'

type ExpandableSqlProps = {
  value: string
  label?: string
  expanded?: boolean
  onExpandedChange?: (expanded: boolean) => void
  showToggle?: boolean
  expandedMaxHeight?: boolean
}

type ExpandableTextProps = {
  value?: string | null
  empty?: string
}

export function isExpandableSql(value: string) {
  return value.length > 120 || value.includes('\n')
}

export function ExpandableSql({ value, label = 'SQL', expanded: controlledExpanded, onExpandedChange, showToggle = true, expandedMaxHeight = true }: ExpandableSqlProps) {
  const [uncontrolledExpanded, setUncontrolledExpanded] = useState(false)
  const expanded = controlledExpanded ?? uncontrolledExpanded
  const shouldCollapse = value.length > 120 || value.includes('\n')
  const toggleExpanded = () => {
    const next = !expanded
    if (onExpandedChange) {
      onExpandedChange(next)
      return
    }
    setUncontrolledExpanded(next)
  }

  return (
    <div className="w-full min-w-0 max-w-full">
      <pre className={`m-0 block w-full min-w-0 max-w-full font-mono text-[12px] leading-6 text-ink ${
        shouldCollapse && !expanded
          ? 'truncate whitespace-nowrap'
          : `${expandedMaxHeight ? 'max-h-80 overflow-auto' : 'overflow-visible'} whitespace-pre-wrap break-all`
      }`}>
        {value}
      </pre>
      {shouldCollapse && showToggle ? (
        <button
          type="button"
          onClick={toggleExpanded}
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

export function ExpandableText({ value, empty = '—' }: ExpandableTextProps) {
  const [expanded, setExpanded] = useState(false)
  const text = value?.trim() || ''
  const shouldCollapse = text.length > 80 || text.includes('\n')

  if (!text) {
    return <span>{empty}</span>
  }

  return (
    <div className="w-full min-w-0 max-w-full">
      <p className={`text-[13px] leading-6 text-muted ${
        shouldCollapse && !expanded
          ? 'truncate whitespace-nowrap'
          : 'max-h-80 overflow-auto whitespace-pre-wrap break-words'
      }`}>
        {text}
      </p>
      {shouldCollapse ? (
        <button
          type="button"
          onClick={() => setExpanded((current) => !current)}
          className="mt-2 inline-flex items-center gap-1 text-[11px] font-semibold text-primary"
          aria-expanded={expanded}
          aria-label={`${expanded ? 'Collapse' : 'Show full'} message`}
        >
          {expanded ? 'Collapse message' : 'Show full message'}
          <ChevronDown className={`h-3.5 w-3.5 transition-transform ${expanded ? 'rotate-180' : ''}`} />
        </button>
      ) : null}
    </div>
  )
}
