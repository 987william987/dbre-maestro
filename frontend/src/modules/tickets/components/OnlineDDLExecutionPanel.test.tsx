import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { OnlineDDLExecutionPanel } from './OnlineDDLExecutionPanel'
import type { OnlineDDLRun, TicketExecution } from '@/shared/types/ticket'

vi.mock('@/modules/tickets/api', () => ({
  dryRunOnlineDDL: vi.fn(),
  getOnlineDDLRun: vi.fn(),
  controlOnlineDDL: vi.fn(),
  tuneOnlineDDL: vi.fn(),
}))

import { controlOnlineDDL, dryRunOnlineDDL } from '@/modules/tickets/api'

const execution: TicketExecution = { id: 7, ticket_id: 3, seq: 1, sql_stmt: 'ALTER TABLE users ADD COLUMN note text', status: 'pending' }
const modes = { 'gh-ost': { enabled: true }, 'pt-osc': { enabled: false } }
const callbacks = { onExecute: vi.fn(async () => undefined), onRunChange: vi.fn() }

function renderPanel(run?: OnlineDDLRun) {
  return render(<OnlineDDLExecutionPanel ticketRef="TK-3" execution={run ? { ...execution, status: run.status === 'planned' ? 'pending' : 'running' } : execution} run={run} modes={modes} canExecute canStop busy={false} {...callbacks} />)
}

function renderNativeOnlyPanel() {
  return render(<OnlineDDLExecutionPanel ticketRef="TK-3" execution={execution} modes={{}} canExecute canStop busy={false} {...callbacks} />)
}

describe('OnlineDDLExecutionPanel', () => {
  beforeEach(() => vi.clearAllMocks())

  it('defaults to Native and explains disabled external modes', () => {
    renderPanel()
    expect(screen.getByRole('combobox', { name: 'Statement 1 execution mode' })).toHaveValue('native')
    expect(screen.getByRole('option', { name: /pt-osc \(disabled\)/i })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Execute' })).toBeInTheDocument()
  })

  it('does not offer MySQL-only tools when the API returns no supported modes', () => {
	renderNativeOnlyPanel()
	const options = screen.getAllByRole('option').map((option) => option.textContent)
	expect(options).toEqual(['Native'])
  })

  it('keeps dry run optional and sends the selected mode only when Execute is clicked', async () => {
    vi.mocked(dryRunOnlineDDL).mockResolvedValue({ success: false, stderr: 'dry run rejected', output_truncated: false, error_code: 'tool_process_failed' })
    renderPanel()
    fireEvent.change(screen.getByRole('combobox', { name: 'Statement 1 execution mode' }), { target: { value: 'gh-ost' } })
    expect(screen.getByRole('dialog', { name: 'Statement 1 · gh-ost' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /dry run/i }))
    await waitFor(() => expect(screen.getByText('Dry run failed')).toBeInTheDocument())
	fireEvent.click(screen.getByRole('button', { name: 'Close' }))
	fireEvent.click(screen.getByRole('button', { name: 'Execute' }))
	await waitFor(() => expect(callbacks.onExecute).toHaveBeenCalledWith('gh-ost', expect.objectContaining({ ghost: expect.any(Object) })))
  })

  it('uses the latest OCC version for pause and exposes persisted progress', async () => {
    const run = { ...makeRun('running'), eta_display: '2+03:59:30' }
    vi.mocked(controlOnlineDDL).mockResolvedValue({ ...run, status: 'paused', version: 5 })
    renderPanel(run)
    fireEvent.click(screen.getByRole('button', { name: 'Manage statement 1 gh-ost' }))
    expect(screen.getByText('42.5%')).toBeInTheDocument()
    expect(screen.getByText('250 ms')).toBeInTheDocument()
    expect(screen.getByText('2+03:59:30')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /pause/i }))
    await waitFor(() => expect(controlOnlineDDL).toHaveBeenCalledWith('TK-3', 7, 'pause', 4))
  })

  it('falls back to legacy seconds for runs created before raw ETA storage', () => {
    const run = { ...makeRun('running'), eta_seconds: 90 }
    renderPanel(run)
    fireEvent.click(screen.getByRole('button', { name: 'Manage statement 1 gh-ost' }))
    expect(screen.getByText('90s')).toBeInTheDocument()
  })

  it('shows None when the tool did not provide a metric', () => {
	const run = { ...makeRun('completed'), progress_percent: null, replication_lag_ms: null }
	renderPanel(run)
	fireEvent.click(screen.getByRole('button', { name: 'Manage statement 1 gh-ost' }))
	expect(screen.getAllByText('None')).toHaveLength(6)
  })
})

function makeRun(status: string): OnlineDDLRun {
  return {
    id: 9, ticket_id: 3, execution_id: 7, executor_id: 2, mode: 'gh-ost', status, phase: 'copy', version: 4,
    initial_parameters: { schema_version: 'v1', ghost: { max_load_threads_running: 10, critical_load_threads_running: 20, chunk_size: 1000, dml_batch_size: 50, nice_ratio: 0.2, max_lag_millis: 1500, cut_over_lock_timeout_seconds: 3 } },
    effective_parameters: { schema_version: 'v1', ghost: { max_load_threads_running: 10, critical_load_threads_running: 20, chunk_size: 1000, dml_batch_size: 50, nice_ratio: 0.2, max_lag_millis: 1500, cut_over_lock_timeout_seconds: 3 } },
    progress_percent: 42.5, replication_lag_ms: 250, created_at: '2026-10-05T09:00:00Z', updated_at: '2026-10-05T09:01:00Z',
  }
}
