import { useMemo, useState } from 'react'
import { Activity } from 'lucide-react'
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import type { DashboardOperationTrend, DashboardOperationTrendPoint } from '@/modules/dashboard/api'

type TrendMode = 'all' | 'tickets' | 'queries'
type OperationKey = Exclude<keyof DashboardOperationTrendPoint, 'date'>

const SERIES: Array<{ key: OperationKey; label: string; color: string; group: Exclude<TrendMode, 'all'> }> = [
  { key: 'ddl', label: 'DDL', color: '#18181b', group: 'tickets' },
  { key: 'dml', label: 'DML', color: '#2563eb', group: 'tickets' },
  { key: 'redis', label: 'Redis', color: '#d97706', group: 'tickets' },
  { key: 'sql_export', label: 'SQL Export', color: '#7c3aed', group: 'tickets' },
  { key: 'query_access', label: 'Query Access', color: '#0891b2', group: 'tickets' },
  { key: 'sensitive_query_access', label: 'Sensitive Access', color: '#dc2626', group: 'tickets' },
  { key: 'query', label: 'Queries', color: '#16a34a', group: 'queries' },
]

const MODES: Array<{ key: TrendMode; label: string }> = [
  { key: 'all', label: 'All' },
  { key: 'tickets', label: 'Tickets' },
  { key: 'queries', label: 'Queries' },
]

function formatCount(value: number) {
  return new Intl.NumberFormat('en-US').format(value)
}

function formatAxisDate(value: string) {
  const [, month, day] = value.split('-')
  return `${month}/${day}`
}

function formatRangeDate(value: string) {
  if (!value) {
    return '—'
  }
  return new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' }).format(new Date(`${value}T00:00:00Z`))
}

export function OperationsTrendChart({ trend }: { trend: DashboardOperationTrend }) {
  const [mode, setMode] = useState<TrendMode>('all')
  const visibleSeries = useMemo(
    () => SERIES.filter((series) => mode === 'all' || series.group === mode),
    [mode],
  )
  const total = useMemo(
    () => trend.points.reduce(
      (sum, point) => sum + visibleSeries.reduce((pointTotal, series) => pointTotal + point[series.key], 0),
      0,
    ),
    [trend.points, visibleSeries],
  )
  const hasData = total > 0

  return (
    <section className="overflow-hidden rounded-xl border border-border bg-panel shadow-soft">
      <div className="flex flex-col gap-3 border-b border-border px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-w-0 items-center gap-3">
          <span className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-border bg-panel-soft text-muted">
            <Activity className="h-4 w-4" />
          </span>
          <div className="min-w-0">
            <h3 className="text-[13px] font-semibold text-ink">Operations Trend</h3>
            <p className="text-[11px] text-muted">Daily activity · {trend.timezone || 'UTC'}</p>
          </div>
        </div>
        <div className="inline-flex h-8 w-fit items-center rounded-lg border border-border bg-panel-soft p-0.5" aria-label="Operation trend mode">
          {MODES.map((item) => (
            <button
              key={item.key}
              type="button"
              aria-pressed={mode === item.key}
              onClick={() => setMode(item.key)}
              className={`h-7 rounded-md px-3 text-[11px] font-semibold transition ${mode === item.key ? 'bg-white text-ink shadow-sm' : 'text-muted hover:text-ink'}`}
            >
              {item.label}
            </button>
          ))}
        </div>
      </div>

      <div className="grid gap-5 p-4 lg:grid-cols-[180px_minmax(0,1fr)]">
        <div className="flex flex-col justify-between gap-5">
          <div>
            <p className="text-[11px] font-medium uppercase text-muted">30-day total</p>
            <p className="mt-1 text-[30px] font-semibold tracking-normal text-ink">{formatCount(total)}</p>
            <p className="mt-1 text-[11px] text-muted">{formatRangeDate(trend.start_date)} – {formatRangeDate(trend.end_date)}</p>
          </div>
          <div className="flex flex-wrap gap-x-3 gap-y-2 lg:grid">
            {visibleSeries.map((series) => (
              <div key={series.key} className="flex items-center gap-2 text-[11px] text-muted">
                <span className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: series.color }} />
                <span>{series.label}</span>
              </div>
            ))}
          </div>
        </div>

        <div className="relative h-[300px] min-w-0" aria-label="Daily platform operation counts">
          <ResponsiveContainer width="100%" height="100%" minWidth={1} minHeight={1}>
            <LineChart data={trend.points} margin={{ top: 8, right: 8, bottom: 0, left: -18 }}>
              <CartesianGrid vertical={false} stroke="#e4e4e7" strokeDasharray="3 3" />
              <XAxis
                dataKey="date"
                axisLine={false}
                tickLine={false}
                minTickGap={28}
                tick={{ fill: '#71717a', fontSize: 11 }}
                tickFormatter={formatAxisDate}
              />
              <YAxis allowDecimals={false} axisLine={false} tickLine={false} width={42} tick={{ fill: '#71717a', fontSize: 11 }} />
              <Tooltip
                labelFormatter={(label) => String(label)}
                formatter={(value, name) => [formatCount(Number(value ?? 0)), SERIES.find((series) => series.key === name)?.label ?? String(name)]}
                contentStyle={{ border: '1px solid #e4e4e7', borderRadius: 8, boxShadow: '0 8px 24px rgba(24,24,27,0.08)', fontSize: 12 }}
              />
              {visibleSeries.map((series) => (
                <Line
                  key={series.key}
                  type="monotone"
                  dataKey={series.key}
                  name={series.key}
                  stroke={series.color}
                  strokeWidth={2}
                  dot={false}
                  activeDot={{ r: 4, strokeWidth: 0 }}
                  isAnimationActive={false}
                />
              ))}
            </LineChart>
          </ResponsiveContainer>
          {!hasData ? (
            <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
              <span className="rounded-md border border-border bg-white/90 px-3 py-2 text-[11px] text-muted">No operations recorded in this period.</span>
            </div>
          ) : null}
        </div>
      </div>
    </section>
  )
}
