import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getDashboard } from '@/modules/dashboard/api'
import { apiClient } from '@/shared/api/client'

vi.mock('@/shared/api/client', () => ({
  apiClient: { get: vi.fn() },
}))

const mockedGet = vi.mocked(apiClient.get)

function dashboardResponse(operationsTrend?: unknown) {
  return {
    personal: {},
    platform: {
      operations_trend: operationsTrend,
    },
  }
}

describe('getDashboard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('preserves the platform operations trend returned by the API', async () => {
    const operationsTrend = {
      timezone: 'UTC',
      start_date: '2026-08-09',
      end_date: '2026-09-07',
      points: [{ date: '2026-09-07', ddl: 1, dml: 2, redis: 3, sql_export: 4, query_access: 5, sensitive_query_access: 6, query: 7 }],
    }
    mockedGet.mockResolvedValue(dashboardResponse(operationsTrend))

    const result = await getDashboard()

    expect(result.platform?.operations_trend).toEqual(operationsTrend)
  })

  it('uses an empty UTC trend when an older API response omits the field', async () => {
    mockedGet.mockResolvedValue(dashboardResponse())

    const result = await getDashboard()

    expect(result.platform?.operations_trend).toEqual({
      timezone: 'UTC',
      start_date: '',
      end_date: '',
      points: [],
    })
  })
})
