import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { StatusBadge } from '@/shared/ui/StatusBadge'

describe('StatusBadge', () => {
  it('failed / stopped / interrupted 應使用一致的失敗色系', () => {
    const { rerender } = render(<StatusBadge status="failed" />)
    expect(screen.getByText('Failed')).toHaveClass('border-rose-200', 'bg-rose-50', 'text-rose-700')

    rerender(<StatusBadge status="stopped" />)
    expect(screen.getByText('Stopped')).toHaveClass('border-rose-200', 'bg-rose-50', 'text-rose-700')

    rerender(<StatusBadge status="interrupted" />)
    expect(screen.getByText('Interrupted')).toHaveClass('border-rose-200', 'bg-rose-50', 'text-rose-700')
  })
})
