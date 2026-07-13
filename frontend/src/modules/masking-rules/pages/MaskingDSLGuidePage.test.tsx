import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import { MaskingDSLGuidePage } from '@/modules/masking-rules/pages/MaskingDSLGuidePage'

describe('MaskingDSLGuidePage', () => {
  it('renders the DSL guide with practical examples', () => {
    render(
      <MemoryRouter>
        <MaskingDSLGuidePage />
      </MemoryRouter>,
    )

    expect(screen.queryByRole('link', { name: 'Back To Rules' })).not.toBeInTheDocument()
    expect(screen.getByText('Basic Structure')).toBeInTheDocument()
    expect(screen.getByText('Mask Modes')).toBeInTheDocument()
    expect(screen.getByText('How Masking Rules Work With Unmask Whitelist')).toBeInTheDocument()
    expect(screen.getByText('Recommended Workflow')).toBeInTheDocument()
    expect(screen.getByText('完整 Rule Payload 範例')).toBeInTheDocument()
    expect(screen.getByText('Step 3. 最終效果')).toBeInTheDocument()
    expect(screen.getByText('如果 `analytics.crm_contacts.email` 是誤傷，不應遮罩，就在 `Unmask Whitelist` 新增一條精準豁免：')).toBeInTheDocument()
  })
})
