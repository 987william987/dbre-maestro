import { fireEvent, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { OperationsTrendChart } from '@/modules/dashboard/components/OperationsTrendChart'
import type { DashboardOperationTrend } from '@/modules/dashboard/api'

vi.mock('recharts', () => ({
  ResponsiveContainer: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  LineChart: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  CartesianGrid: () => null,
  XAxis: () => null,
  YAxis: () => null,
  Tooltip: () => null,
  Line: ({ name }: { name: string }) => <span data-testid={`line-${name}`} />,
}))

const trend: DashboardOperationTrend = {
  timezone: 'UTC',
  start_date: '2026-09-01',
  end_date: '2026-09-02',
  points: [
    { date: '2026-09-01', ddl: 2, dml: 1, redis: 0, sql_export: 1, query_access: 1, sensitive_query_access: 0, query: 8 },
    { date: '2026-09-02', ddl: 1, dml: 0, redis: 1, sql_export: 0, query_access: 0, sensitive_query_access: 1, query: 4 },
  ],
}

describe('OperationsTrendChart', () => {
  it('shows all operation series and switches totals by mode', () => {
    render(<OperationsTrendChart trend={trend} />)

    expect(screen.getByText('20')).toBeInTheDocument()
    expect(screen.getByTestId('line-ddl')).toBeInTheDocument()
    expect(screen.getByTestId('line-query')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Tickets' }))
    expect(screen.getByText('8')).toBeInTheDocument()
    expect(screen.getByTestId('line-ddl')).toBeInTheDocument()
    expect(screen.queryByTestId('line-query')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Queries' }))
    expect(screen.getByText('12')).toBeInTheDocument()
    expect(screen.queryByTestId('line-ddl')).not.toBeInTheDocument()
    expect(screen.getByTestId('line-query')).toBeInTheDocument()
  })

  it('shows an explicit empty state', () => {
    render(<OperationsTrendChart trend={{ ...trend, points: [] }} />)
    expect(screen.getByText('No operations recorded in this period.')).toBeInTheDocument()
  })
})
