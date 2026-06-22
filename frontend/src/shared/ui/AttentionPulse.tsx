import { useEffect, useState, type ReactElement } from 'react'
import { cn } from '@/lib/utils'

type AttentionPulseProps = {
  activeKey?: string | number | null
  disabled?: boolean
  className?: string
  children: ReactElement
}

const ATTENTION_PULSE_MS = 2400

export function AttentionPulse({ activeKey, disabled = false, className, children }: AttentionPulseProps) {
  const [animating, setAnimating] = useState(false)

  useEffect(() => {
    if (activeKey === undefined || activeKey === null || disabled) {
      setAnimating(false)
      return undefined
    }

    setAnimating(false)
    const frameID = window.requestAnimationFrame(() => {
      setAnimating(true)
    })
    const timeoutID = window.setTimeout(() => {
      setAnimating(false)
    }, ATTENTION_PULSE_MS)

    return () => {
      window.cancelAnimationFrame(frameID)
      window.clearTimeout(timeoutID)
    }
  }, [activeKey, disabled])

  return (
    <span
      data-attention-active={animating ? 'true' : undefined}
      className={cn('inline-flex rounded-md', animating && 'attention-pulse', className)}
    >
      {children}
    </span>
  )
}
