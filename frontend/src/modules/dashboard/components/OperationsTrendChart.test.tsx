import { fireEvent, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { OperationsTrendChart } from '@/modules/dashboard/components/OperationsTrendChart'
import type { DashboardOperationTrend } from '@/modules/dashboard/api'

const { rechartsProps } = vi.hoisted(() => ({
  rechartsProps: {
    grid: vi.fn(),
    xAxis: vi.fn(),
    yAxis: vi.fn(),
    tooltip: vi.fn(),
    line: vi.fn(),
  },
}))

vi.mock('recharts', () => ({
  ResponsiveContainer: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  LineChart: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  CartesianGrid: (props: unknown) => { rechartsProps.grid(props); return null },
  XAxis: (props: unknown) => { rechartsProps.xAxis(props); return null },
  YAxis: (props: unknown) => { rechartsProps.yAxis(props); return null },
  Tooltip: (props: unknown) => { rechartsProps.tooltip(props); return null },
  Line: (props: { name: string }) => { rechartsProps.line(props); return <span data-testid={`line-${props.name}`} /> },
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

  it('uses theme tokens for chart series, axes, grid, and tooltip surfaces', () => {
    render(<OperationsTrendChart trend={trend} />)

    expect(rechartsProps.grid).toHaveBeenCalledWith(expect.objectContaining({ stroke: 'rgb(var(--border))' }))
    expect(rechartsProps.xAxis).toHaveBeenCalledWith(expect.objectContaining({ tick: { fill: 'rgb(var(--text-muted))', fontSize: 11 } }))
    expect(rechartsProps.yAxis).toHaveBeenCalledWith(expect.objectContaining({ tick: { fill: 'rgb(var(--text-muted))', fontSize: 11 } }))
    expect(rechartsProps.tooltip).toHaveBeenCalledWith(expect.objectContaining({
      contentStyle: expect.objectContaining({
        backgroundColor: 'rgb(var(--panel))',
        border: '1px solid rgb(var(--border))',
        color: 'rgb(var(--text))',
      }),
    }))
    expect(rechartsProps.line).toHaveBeenCalledWith(expect.objectContaining({ name: 'ddl', stroke: 'rgb(var(--chart-ddl))' }))
    expect(rechartsProps.line).toHaveBeenCalledWith(expect.objectContaining({ name: 'query', stroke: 'rgb(var(--chart-query))' }))
  })
})
