import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SettingsPage } from '@/modules/settings/pages/SettingsPage'

vi.mock('@/modules/settings/api', () => ({
  getSettings: vi.fn(),
}))

import { getSettings } from '@/modules/settings/api'

const mockedGetSettings = vi.mocked(getSettings)

describe('SettingsPage', () => {
  beforeEach(() => {
    mockedGetSettings.mockReset()
  })

  it('不依賴 users API 也能載入設定頁', async () => {
    mockedGetSettings.mockResolvedValue({
      sensitive_export_reviewer_user_ids: [],
      sensitive_query_access_reviewer_user_ids: [],
    })

    render(
      <MemoryRouter>
        <SettingsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('平台設定')).toBeInTheDocument())
    expect(screen.getByText('Reviewer 改由 RBAC 管理')).toBeInTheDocument()
    expect(screen.getByText(/不需要 `users.read` 或 `users.write`/)).toBeInTheDocument()
  })
})
