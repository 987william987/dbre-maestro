import { useEffect, useMemo, useState } from 'react'
import { createPortal } from 'react-dom'
import type { ReactNode } from 'react'
import { Loader2, Pause, Play, Settings2, Square, X } from 'lucide-react'
import { ApiError } from '@/shared/api/client'
import type { OnlineDDLMode, OnlineDDLParameters, OnlineDDLRun, TicketExecution } from '@/shared/types/ticket'
import { controlOnlineDDL, dryRunOnlineDDL, getOnlineDDLRun, tuneOnlineDDL } from '@/modules/tickets/api'

type Props = {
  ticketRef: string
  execution: TicketExecution
  run?: OnlineDDLRun
  modes: Record<OnlineDDLMode, { enabled: boolean }>
  canExecute: boolean
  canStop: boolean
  busy: boolean
  onExecute: (mode: 'native' | OnlineDDLMode, parameters?: OnlineDDLParameters) => Promise<void>
  onRunChange: (run: OnlineDDLRun) => void
}

const ghostDefaults: OnlineDDLParameters = {
  schema_version: 'v1',
  ghost: { max_load_threads_running: 10, critical_load_threads_running: 20, chunk_size: 1000, dml_batch_size: 50, nice_ratio: 0.2, max_lag_millis: 1500, cut_over_lock_timeout_seconds: 3 },
}
const ptoscDefaults: OnlineDDLParameters = {
  schema_version: 'v1',
  ptosc: { chunk_size: 1000, max_load_threads_running: 25, critical_load_threads_running: 50, max_lag_seconds: 1, check_interval_seconds: 1, alter_foreign_keys_method: 'none' },
}
const activeStatuses = new Set(['queued', 'running', 'paused', 'cancel_requested'])

export function OnlineDDLExecutionPanel(props: Props) {
  const [mode, setMode] = useState<'native' | OnlineDDLMode>(props.run?.mode ?? 'native')
  const [parameters, setParameters] = useState<OnlineDDLParameters>(() => props.run?.effective_parameters ?? ghostDefaults)
  const [modalOpen, setModalOpen] = useState(false)
  const [error, setError] = useState('')
  const [acting, setActing] = useState('')
  const [dryRun, setDryRun] = useState<{ success: boolean; stdout?: string; stderr?: string; output_truncated: boolean; error_code?: string }>()
  const run = props.run

  useEffect(() => {
    if (run) {
      setMode(run.mode)
      setParameters(run.effective_parameters)
    }
  }, [run])

  useEffect(() => {
    if (!run || !activeStatuses.has(run.status)) return
    const controller = new AbortController()
    const timer = window.setInterval(() => {
      getOnlineDDLRun(props.ticketRef, props.execution.id, controller.signal)
        .then(props.onRunChange)
        .catch((pollError: unknown) => {
          if (!(pollError instanceof DOMException && pollError.name === 'AbortError')) setError(errorMessage(pollError))
        })
    }, 2000)
    return () => {
      controller.abort()
      window.clearInterval(timer)
    }
  }, [props.execution.id, props.onRunChange, props.ticketRef, run])

  const editable = props.execution.status === 'pending' && !run
  const parameterRows = useMemo(() => mode === 'gh-ost'
    ? [
        ['max_load_threads_running', 'Max load (Threads_running)', 1, 10000],
        ['critical_load_threads_running', 'Critical load (Threads_running)', 2, 100000],
        ['chunk_size', 'Chunk size', 100, 100000],
        ['dml_batch_size', 'DML batch size', 1, 100],
        ['nice_ratio', 'Nice ratio', 0, 100],
        ['max_lag_millis', 'Max lag (ms)', 100, 60000],
        ['cut_over_lock_timeout_seconds', 'Cut-over lock timeout (s)', 1, 60],
      ] as const
    : [
        ['chunk_size', 'Chunk size', 100, 100000],
        ['max_load_threads_running', 'Max load (Threads_running)', 1, 10000],
        ['critical_load_threads_running', 'Critical load (Threads_running)', 2, 100000],
        ['max_lag_seconds', 'Max lag (s)', 1, 60],
        ['check_interval_seconds', 'Check interval (s)', 1, 60],
      ] as const, [mode])

  function selectMode(next: 'native' | OnlineDDLMode) {
    setMode(next)
    setError('')
    setDryRun(undefined)
    if (next === 'gh-ost') setParameters(ghostDefaults)
    if (next === 'pt-osc') setParameters(ptoscDefaults)
    setModalOpen(next !== 'native')
  }

  function updateNumber(key: string, value: number) {
    setParameters((current) => mode === 'gh-ost'
      ? { ...current, ghost: { ...current.ghost!, [key]: value } }
      : { ...current, ptosc: { ...current.ptosc!, [key]: value } })
  }

  async function act(name: string, action: () => Promise<OnlineDDLRun | void>) {
    setActing(name)
    setError('')
    try {
      const next = await action()
      if (next) props.onRunChange(next)
    } catch (actionError) {
      setError(errorMessage(actionError))
    } finally {
      setActing('')
    }
  }

  const modeDisabled = mode !== 'native' && !props.modes[mode].enabled
  return (
    <div className="flex min-w-[250px] items-center gap-2">
      <select
        aria-label={`Statement ${props.execution.seq} execution mode`}
        value={mode}
        disabled={!editable}
        onChange={(event) => selectMode(event.target.value as 'native' | OnlineDDLMode)}
        className="h-8 min-w-0 flex-1 rounded-md border border-border bg-panel px-2 text-[12px] font-medium text-ink disabled:cursor-not-allowed disabled:opacity-70"
      >
        <option value="native">Native</option>
        <option value="gh-ost" disabled={!props.modes['gh-ost'].enabled}>gh-ost{props.modes['gh-ost'].enabled ? '' : ' (disabled)'}</option>
        <option value="pt-osc" disabled={!props.modes['pt-osc'].enabled}>pt-osc{props.modes['pt-osc'].enabled ? '' : ' (disabled)'}</option>
      </select>
      {mode !== 'native' ? (
        <button type="button" aria-label={`${run ? 'Manage' : 'Configure'} statement ${props.execution.seq} ${mode}`} onClick={() => setModalOpen(true)} className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-border bg-panel text-muted transition hover:bg-panel-soft hover:text-ink">
          <Settings2 className="h-3.5 w-3.5" />
        </button>
      ) : null}
      {props.execution.status === 'pending' && props.canExecute && !run ? (
        <button type="button" disabled={props.busy || Boolean(acting)} onClick={() => void act('execute', () => props.onExecute(mode, mode === 'native' ? undefined : parameters))} className="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-md bg-brand px-2.5 text-[12px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-50">
          {acting === 'execute' || props.busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />} Execute
        </button>
      ) : null}
      {modalOpen && mode !== 'native' ? createPortal(
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/35 p-4" role="presentation">
          <div role="dialog" aria-modal="true" aria-labelledby={`online-ddl-title-${props.execution.id}`} className="max-h-[calc(100vh-2rem)] w-full max-w-4xl overflow-y-auto rounded-card border border-border bg-panel shadow-card">
            <div className="sticky top-0 z-10 flex items-start justify-between gap-4 border-b border-border bg-panel px-5 py-4">
              <div>
                <h3 id={`online-ddl-title-${props.execution.id}`} className="text-[16px] font-semibold text-ink">Statement {props.execution.seq} · {mode}</h3>
                <p className="mt-1 text-[12px] text-muted">{modeDescription(mode)}</p>
              </div>
              <button type="button" aria-label="Close Online DDL settings" onClick={() => setModalOpen(false)} disabled={Boolean(acting)} className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-muted transition hover:bg-panel-soft hover:text-ink disabled:opacity-50"><X className="h-4 w-4" /></button>
            </div>
            <div className="p-5">
              {run ? <div className="mb-4 flex items-center gap-2"><span className="rounded-full border border-border bg-panel-soft px-2 py-1 text-[11px] font-semibold text-muted">{run.mode} · {run.status}</span></div> : null}
              <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {parameterRows.map(([key, label, min, max]) => {
                  const source = mode === 'gh-ost' ? parameters.ghost : parameters.ptosc
                  return <NumberField key={key} label={label} value={Number(source?.[key as keyof typeof source] ?? 0)} min={min} max={max} disabled={!editable} onChange={(value) => updateNumber(key, value)} />
                })}
                {mode === 'pt-osc' ? (
                  <label className="grid gap-1 text-[11px] font-semibold text-muted">Foreign key method
                    <select disabled={!editable} value={parameters.ptosc?.alter_foreign_keys_method ?? 'none'} onChange={(event) => setParameters((current) => ({ ...current, ptosc: { ...current.ptosc!, alter_foreign_keys_method: event.target.value as NonNullable<OnlineDDLParameters['ptosc']>['alter_foreign_keys_method'] } }))} className="h-9 rounded-md border border-border bg-panel px-2 text-[12px] text-ink disabled:opacity-70">
                      <option value="none">None</option><option value="auto">Auto</option><option value="rebuild_constraints">Rebuild constraints</option><option value="drop_swap">Drop swap</option>
                    </select>
                  </label>
                ) : null}
              </div>
              {run && run.status !== 'planned' ? <RunStatus run={run} /> : null}
              {run?.mode === 'gh-ost' && (run.status === 'running' || run.status === 'paused') && props.canExecute ? <RuntimeTuning run={run} busy={Boolean(acting)} onTune={(key, value) => act('tune', () => tuneOnlineDDL(props.ticketRef, props.execution.id, run.version, { [key]: value }))} /> : null}
              {run?.mode === 'pt-osc' && activeStatuses.has(run.status) ? <p className="mt-3 text-[11px] text-muted">pt-osc parameters are read-only after execution starts.</p> : null}
              {error ? <p role="alert" className="mt-4 text-[12px] text-rose-700 dark:text-rose-300">{error}</p> : null}
              {dryRun ? <div className={`mt-4 rounded-md border p-3 text-[12px] ${dryRun.success ? 'border-emerald-300 bg-emerald-50 dark:bg-emerald-950/30' : 'border-rose-300 bg-rose-50 dark:bg-rose-950/30'}`}><p className="font-semibold text-ink">Dry run {dryRun.success ? 'succeeded' : 'failed'}{dryRun.output_truncated ? ' (output truncated)' : ''}</p>{dryRun.error_code ? <p className="mt-1 text-muted">{dryRun.error_code}</p> : null}{dryRun.stdout ? <pre className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap text-[11px] text-ink">{dryRun.stdout}</pre> : null}{dryRun.stderr ? <pre className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap text-[11px] text-ink">{dryRun.stderr}</pre> : null}</div> : null}
            </div>
            <div className="sticky bottom-0 flex flex-wrap justify-end gap-2 border-t border-border bg-panel px-5 py-4">
              {run?.status === 'running' && props.canStop ? <ControlButton label="Pause" icon={<Pause className="h-3.5 w-3.5" />} disabled={Boolean(acting)} onClick={() => void act('pause', () => controlOnlineDDL(props.ticketRef, props.execution.id, 'pause', run.version))} /> : null}
              {run?.status === 'paused' && props.canExecute ? <ControlButton label="Resume" icon={<Play className="h-3.5 w-3.5" />} disabled={Boolean(acting)} onClick={() => void act('resume', () => controlOnlineDDL(props.ticketRef, props.execution.id, 'resume', run.version))} /> : null}
              {run && (run.status === 'running' || run.status === 'paused') && props.canStop ? <ControlButton label="Cancel" icon={<Square className="h-3.5 w-3.5" />} disabled={Boolean(acting)} danger onClick={() => void act('cancel', () => controlOnlineDDL(props.ticketRef, props.execution.id, 'cancel', run.version))} /> : null}
              <button type="button" disabled={Boolean(acting)} onClick={() => setModalOpen(false)} className="h-9 rounded-md border border-border bg-panel px-3 text-[12px] font-semibold text-ink disabled:opacity-50">Close</button>
              {editable ? (
                <button type="button" disabled={!props.canExecute || modeDisabled || Boolean(acting)} onClick={() => void act('dry-run', async () => {
                  setDryRun(await dryRunOnlineDDL(props.ticketRef, props.execution.id, mode, parameters))
                })} className="inline-flex h-9 items-center gap-1.5 rounded-md border border-border bg-panel px-3 text-[12px] font-semibold text-ink disabled:opacity-50">
                  {acting === 'dry-run' ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null} Dry run
                </button>
              ) : null}
            </div>
          </div>
        </div>, document.body) : null}
    </div>
  )
}

function RunStatus({ run }: { run: OnlineDDLRun }) {
  return <div className="mt-3 grid gap-2 rounded-md border border-border bg-panel p-3 text-[12px] sm:grid-cols-2 lg:grid-cols-4">
    <Metric label="Phase" value={run.phase ?? 'Waiting'} /><Metric label="Progress" value={run.progress_percent == null ? 'None' : `${run.progress_percent.toFixed(1)}%`} />
    <Metric label="ETA" value={run.eta_seconds == null ? 'None' : `${run.eta_seconds}s`} /><Metric label="Copied rows" value={run.copied_rows?.toLocaleString() ?? 'None'} />
    <Metric label="Replication lag" value={run.replication_lag_ms == null ? 'None' : `${run.replication_lag_ms} ms`} /><Metric label="Threads running" value={run.threads_running?.toString() ?? 'None'} />
    <Metric label="Throttle" value={run.throttle_reason ?? 'None'} /><Metric label="Heartbeat" value={run.heartbeat_at ? new Date(run.heartbeat_at).toLocaleString() : 'Not received'} />
    {run.outcome_confidence ? <Metric label="Outcome" value={run.outcome_confidence} /> : null}{run.error_code ? <Metric label="Error" value={run.error_code} /> : null}
    {run.artifact_summary && Object.keys(run.artifact_summary).length > 0 ? <div className="sm:col-span-2 lg:col-span-4"><span className="font-semibold text-muted">Artifacts</span><pre className="mt-1 overflow-auto whitespace-pre-wrap text-[11px] text-ink">{JSON.stringify(run.artifact_summary, null, 2)}</pre></div> : null}
    <details className="sm:col-span-2 lg:col-span-4"><summary className="cursor-pointer font-semibold text-muted">Initial and effective parameters</summary><div className="mt-2 grid gap-2 lg:grid-cols-2"><pre className="overflow-auto whitespace-pre-wrap rounded border border-border p-2 text-[11px] text-ink">Initial{`\n`}{JSON.stringify(run.initial_parameters, null, 2)}</pre><pre className="overflow-auto whitespace-pre-wrap rounded border border-border p-2 text-[11px] text-ink">Effective{`\n`}{JSON.stringify(run.effective_parameters, null, 2)}</pre></div></details>
  </div>
}

function RuntimeTuning({ run, busy, onTune }: { run: OnlineDDLRun; busy: boolean; onTune: (key: string, value: number) => Promise<void> }) {
  const [key, setKey] = useState('max_load_threads_running')
  const current = run.effective_parameters.ghost?.[key as keyof NonNullable<OnlineDDLParameters['ghost']>] ?? 0
  const [value, setValue] = useState(Number(current))
  useEffect(() => setValue(Number(current)), [current, key])
  return <div className="mt-3 flex flex-wrap items-end gap-2 border-t border-border pt-3">
    <label className="grid gap-1 text-[11px] font-semibold text-muted">Runtime parameter<select value={key} onChange={(event) => setKey(event.target.value)} className="h-8 rounded-md border border-border bg-panel px-2 text-[12px] text-ink">{['max_load_threads_running', 'critical_load_threads_running', 'chunk_size', 'dml_batch_size', 'nice_ratio', 'max_lag_millis'].map((item) => <option key={item}>{item}</option>)}</select></label>
    <NumberField label="New value" value={value} min={0} max={100000} disabled={busy} onChange={setValue} />
    <button type="button" disabled={busy} onClick={() => void onTune(key, value)} className="h-8 rounded-md border border-border bg-panel px-3 text-[12px] font-semibold text-ink disabled:opacity-50">Apply</button>
  </div>
}

function NumberField({ label, value, min, max, disabled, onChange }: { label: string; value: number; min: number; max: number; disabled: boolean; onChange: (value: number) => void }) {
  return <label className="grid gap-1 text-[11px] font-semibold text-muted">{label}<input type="number" value={value} min={min} max={max} step={label.includes('ratio') ? 0.1 : 1} disabled={disabled} onChange={(event) => onChange(Number(event.target.value))} className="h-8 rounded-md border border-border bg-panel px-2 text-[12px] text-ink disabled:opacity-70" /></label>
}
function Metric({ label, value }: { label: string; value: string }) { return <div><span className="font-semibold text-muted">{label}</span><p className="mt-0.5 break-words text-ink">{value}</p></div> }
function ControlButton({ label, icon, disabled, danger, onClick }: { label: string; icon: ReactNode; disabled: boolean; danger?: boolean; onClick: () => void }) { return <button type="button" disabled={disabled} onClick={onClick} className={`inline-flex h-8 items-center gap-1.5 rounded-md border px-3 text-[12px] font-semibold disabled:opacity-50 ${danger ? 'border-rose-300 bg-rose-50 text-rose-700 dark:bg-rose-950/40 dark:text-rose-300' : 'border-border bg-panel text-ink'}`}>{icon}{label}</button> }
function modeDescription(mode: 'native' | OnlineDDLMode) { if (mode === 'gh-ost') return 'Best for large, busy tables with ROW/FULL binlog; does not support triggers or foreign keys.'; if (mode === 'pt-osc') return 'Trigger-based online copy with explicit foreign-key handling; runtime parameters stay fixed.'; return 'Runs the approved SQL directly through MySQL. Fastest setup, but progress and pause are unavailable.' }
function errorMessage(error: unknown) { return error instanceof ApiError ? error.message : 'Online DDL action failed. Please try again.' }
