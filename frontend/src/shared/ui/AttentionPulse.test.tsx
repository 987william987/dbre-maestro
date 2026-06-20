import { render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { AttentionPulse } from '@/shared/ui/AttentionPulse'

describe('AttentionPulse', () => {
  it('activeKey 改變時會啟動 attention 狀態', async () => {
    const { rerender } = render(
      <AttentionPulse>
        <button type="button">Action</button>
      </AttentionPulse>,
    )

    expect(screen.getByRole('button', { name: 'Action' }).parentElement).not.toHaveAttribute('data-attention-active')

    rerender(
      <AttentionPulse activeKey={1}>
        <button type="button">Action</button>
      </AttentionPulse>,
    )

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Action' }).parentElement).toHaveAttribute('data-attention-active', 'true')
    })
  })

  it('disabled 時不會啟動 attention 狀態', async () => {
    render(
      <AttentionPulse activeKey={1} disabled>
        <button type="button">Action</button>
      </AttentionPulse>,
    )

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Action' }).parentElement).not.toHaveAttribute('data-attention-active')
    })
  })
})
