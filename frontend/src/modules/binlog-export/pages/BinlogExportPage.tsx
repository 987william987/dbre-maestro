import { useCallback, useEffect, useMemo, useState, type FormEvent, type ReactNode } from 'react'
import { Ban, Database, Download, Eye, FileClock, Loader2, RefreshCw, RotateCcw, Send, X } from 'lucide-react'
import {
  cancelBinlogExport,
  createBinlogExport,
  downloadBinlogArtifact,
  listBinlogDatabases,
  listBinlogExportConnections,
  listBinlogExportJobs,
  listBinlogs,
	previewBinlogArtifact,
	probeBinlogTimestamps,
  listBinlogTables,
  retryBinlogExport,
  type BinlogExportJob,
  type BinlogExportStatus,
  type BinlogFile,
  type CreateBinlogExportPayload,
} from '@/modules/binlog-export/api'
import { ApiError } from '@/shared/api/client'
import { useAuth } from '@/shared/auth/AuthContext'
import { formatDateTime } from '@/shared/lib/format'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { SearchableMultiSelect } from '@/modules/binlog-export/components/SearchableMultiSelect'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { Pagination } from '@/shared/ui/Pagination'

type Draft = {
  connectionID: string
  rangeMode: 'time' | 'position'
  timezone: string
  startTime: string
  endTime: string
  startFile: string
  startPos: string
  endFile: string
  endPos: string
  database: string
  tables: string[]
  dmlTypes: string[]
  acknowledgeUnfiltered: boolean
}

const now = new Date()
const EMPTY_DRAFT: Draft = {
  connectionID: '', rangeMode: 'time', timezone: 'UTC',
  startTime: toUTCInput(new Date(now.getTime() - 5 * 60_000)), endTime: toUTCInput(now),
  startFile: '', startPos: '4', endFile: '', endPos: '', database: '', tables: [],
  dmlTypes: ['insert', 'update', 'delete'], acknowledgeUnfiltered: false,
}

const inputClass = 'h-10 w-full rounded-lg border border-border bg-panel px-3 text-[13px] text-ink outline-none transition focus:border-border-strong disabled:cursor-not-allowed disabled:opacity-60'
const activeStatuses: BinlogExportStatus[] = ['queued', 'running', 'cancel_requested']
const JOB_PAGE_SIZE = 20

export function BinlogExportPage() {
  const { user } = useAuth()
  const canExecute = user?.permissions.includes('binlog_exports.execute') ?? false
  const [draft, setDraft] = useState<Draft>(EMPTY_DRAFT)
  const [connections, setConnections] = useState<Array<{ id: number; name: string }>>([])
  const [binlogs, setBinlogs] = useState<BinlogFile[]>([])
  const [databases, setDatabases] = useState<string[]>([])
  const [tables, setTables] = useState<string[]>([])
  const [jobs, setJobs] = useState<BinlogExportJob[]>([])
  const [jobOffset, setJobOffset] = useState(0)
  const [jobTotal, setJobTotal] = useState(0)
  const [loading, setLoading] = useState(true)
	const [loadingContext, setLoadingContext] = useState(false)
	const [probingTimes, setProbingTimes] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [actionID, setActionID] = useState<number | null>(null)
  const [error, setError] = useState('')
	const [notice, setNotice] = useState('')
	const [preview, setPreview] = useState<{ title: string; sql: string; truncated: boolean } | null>(null)

  const connectionOptions = useMemo(() => [{ value: '', label: 'Select connection' }, ...connections.map((item) => ({ value: String(item.id), label: item.name }))], [connections])
  const databaseOptions = useMemo(() => [{ value: '', label: 'All databases' }, ...databases.map((name) => ({ value: name, label: name }))], [databases])
  const binlogOptions = useMemo(() => [{ value: '', label: loadingContext ? 'Loading binlogs...' : 'Select binlog file' }, ...binlogs.map((item) => ({ value: item.name, label: item.active ? `${item.name} (active)` : item.name }))], [binlogs, loadingContext])
  const timezoneOptions = useMemo(() => getTimezoneNames().map((timezone) => ({ value: timezone, label: timezoneLabel(timezone) })), [])
  const unfiltered = draft.database === '' && draft.tables.length === 0

  const refreshJobs = useCallback(async (silent = false, offset = 0) => {
    try {
      const response = await listBinlogExportJobs(JOB_PAGE_SIZE, offset)
      setJobs(response.items)
      setJobTotal(response.total)
    } catch (loadError) {
      if (!silent) setError(apiMessage(loadError, 'Failed to load binlog export jobs.'))
    }
  }, [])

  useEffect(() => {
    let active = true
    async function load() {
      setLoading(true)
      try {
        const [nextConnections, nextJobs] = await Promise.all([listBinlogExportConnections(), listBinlogExportJobs(JOB_PAGE_SIZE, 0)])
        if (!active) return
        setConnections(nextConnections)
        setJobs(nextJobs.items)
        setJobTotal(nextJobs.total)
      } catch (loadError) {
        if (active) setError(apiMessage(loadError, 'Failed to load MySQL Binlog Export.'))
      } finally {
        if (active) setLoading(false)
      }
    }
    void load()
    return () => { active = false }
  }, [])

  useEffect(() => {
    if (!jobs.some((job) => activeStatuses.includes(job.status))) return
    const timer = window.setInterval(() => void refreshJobs(true, jobOffset), 2000)
    return () => window.clearInterval(timer)
  }, [jobOffset, jobs, refreshJobs])

  useEffect(() => {
    if (draft.connectionID === '') {
      setBinlogs([]); setDatabases([]); setTables([])
      return
    }
    let active = true
    setLoadingContext(true)
    Promise.all([listBinlogs(Number(draft.connectionID)), listBinlogDatabases(Number(draft.connectionID))])
      .then(([binlogResponse, nextDatabases]) => {
        if (!active) return
        setBinlogs(binlogResponse.items)
        setDatabases(nextDatabases)
        const latest = binlogResponse.items[binlogResponse.items.length - 1]
        setDraft((current) => ({ ...current, startFile: latest?.name ?? '', endFile: latest?.name ?? '', endPos: latest ? String(latest.size_bytes) : '' }))
      })
      .catch((loadError) => { if (active) setError(apiMessage(loadError, 'Failed to load connection metadata.')) })
      .finally(() => { if (active) setLoadingContext(false) })
    return () => { active = false }
  }, [draft.connectionID])

  useEffect(() => {
    if (draft.connectionID === '' || draft.database === '') {
      setTables([])
      setDraft((current) => current.tables.length ? { ...current, tables: [] } : current)
      return
    }
    let active = true
    listBinlogTables(Number(draft.connectionID), draft.database)
      .then((items) => { if (active) setTables(items) })
      .catch((loadError) => { if (active) setError(apiMessage(loadError, 'Failed to load tables.')) })
    return () => { active = false }
  }, [draft.connectionID, draft.database])

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setError(''); setNotice('')
    let payload: CreateBinlogExportPayload
    try {
      payload = buildPayload(draft)
    } catch (validationError) {
      setError(validationError instanceof Error ? validationError.message : 'Invalid export range.')
      return
    }
    setSubmitting(true)
    try {
      const created = await createBinlogExport(payload)
      setNotice(`Binlog export #${created.id} queued.`)
      setDraft((current) => ({ ...current, acknowledgeUnfiltered: false }))
      setJobOffset(0)
      await refreshJobs(true, 0)
    } catch (submitError) {
      setError(apiMessage(submitError, 'Failed to create binlog export.'))
    } finally {
      setSubmitting(false)
    }
  }

  async function runAction(job: BinlogExportJob, action: 'cancel' | 'retry') {
    setActionID(job.id); setError(''); setNotice('')
    try {
      if (action === 'cancel') await cancelBinlogExport(job.id)
      else await retryBinlogExport(job.id)
      setNotice(action === 'cancel' ? `Cancellation requested for #${job.id}.` : `Retry queued for #${job.id}.`)
      const nextOffset = action === 'retry' ? 0 : jobOffset
      if (action === 'retry') setJobOffset(0)
      await refreshJobs(true, nextOffset)
    } catch (actionError) {
      setError(apiMessage(actionError, `Failed to ${action} binlog export.`))
    } finally {
      setActionID(null)
    }
  }

	async function download(job: BinlogExportJob, kind: 'forward_sql' | 'rollback_sql') {
    setActionID(job.id); setError('')
    try { await downloadBinlogArtifact(job.id, kind) }
    catch (downloadError) { setError(apiMessage(downloadError, 'Failed to download artifact.')) }
    finally { setActionID(null) }
	}

	async function showPreview(job: BinlogExportJob, kind: 'forward_sql' | 'rollback_sql') {
		setActionID(job.id); setError('')
		try {
			const result = await previewBinlogArtifact(job.id, kind)
			setPreview({ title: `#${job.id} ${kind === 'forward_sql' ? 'Forward' : 'Rollback'} SQL`, sql: result.sql, truncated: result.truncated })
		} catch (previewError) { setError(apiMessage(previewError, 'Failed to preview artifact.')) }
		finally { setActionID(null) }
	}

	async function probeTimes() {
		if (!draft.connectionID || binlogs.length === 0) return
		setProbingTimes(true); setError('')
		let failures = 0
		for (const file of binlogs) {
			try {
				const [result] = await probeBinlogTimestamps(Number(draft.connectionID), file.name)
				if (result) setBinlogs((current) => mergeBinlogStart(current, result.file, result.start_time))
			} catch { failures += 1 }
		}
		if (failures > 0) setError(`Failed to probe ${failures} binlog file${failures === 1 ? '' : 's'}.`)
		setProbingTimes(false)
	}

  if (loading) return <div className="p-3 sm:p-4"><LoadingBlock message="Loading binlog exports..." className="min-h-[360px] rounded-xl border-border bg-panel" /></div>

  return (
    <div className="space-y-5 p-3 sm:p-4 lg:p-6">
      {error ? <InlineAlert tone="error">{error}</InlineAlert> : null}
      {notice ? <InlineAlert tone="success">{notice}</InlineAlert> : null}

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.35fr)_minmax(300px,0.65fr)]">
        <form onSubmit={(event) => void handleSubmit(event)} className="rounded-lg border border-border bg-panel p-4 shadow-soft sm:p-5">
          <div className="mb-4 flex items-center gap-2"><Database className="h-4 w-4 text-brand" /><h3 className="text-[15px] font-semibold text-ink">New Export</h3></div>
          <fieldset disabled={!canExecute || submitting} className="space-y-4">
            <Field label="DB Connection"><DropdownSelect value={draft.connectionID} onChange={(value) => setDraft((current) => ({ ...current, connectionID: value, database: '', tables: [] }))} options={connectionOptions} ariaLabel="DB Connection" searchable /></Field>
            <div className="grid grid-cols-2 gap-2 rounded-lg bg-panel-soft p-1" aria-label="Range mode">
              {(['time', 'position'] as const).map((mode) => <button key={mode} type="button" onClick={() => setDraft((current) => ({ ...current, rangeMode: mode }))} className={`h-9 rounded-md text-[12px] font-semibold capitalize ${draft.rangeMode === mode ? 'bg-panel text-ink shadow-soft' : 'text-muted hover:text-ink'}`}>{mode}</button>)}
            </div>
            {draft.rangeMode === 'time' ? (
              <div className="grid gap-3 md:grid-cols-2"><Field label="Start time"><input aria-label="Start time" type="datetime-local" value={draft.startTime} onChange={(e) => setDraft((c) => ({ ...c, startTime: e.target.value }))} className={inputClass} /></Field><Field label="End time"><input aria-label="End time" type="datetime-local" value={draft.endTime} onChange={(e) => setDraft((c) => ({ ...c, endTime: e.target.value }))} className={inputClass} /></Field><Field label="Timezone" className="md:col-span-2"><DropdownSelect value={draft.timezone} onChange={(value) => setDraft((c) => ({ ...c, timezone: value }))} options={timezoneOptions} ariaLabel="Timezone" searchable /></Field></div>
            ) : (
              <div className="grid gap-3 md:grid-cols-2"><Field label="Start file"><DropdownSelect value={draft.startFile} onChange={(value) => setDraft((c) => ({ ...c, startFile: value }))} options={binlogOptions} ariaLabel="Start file" searchable /></Field><Field label="Start position"><input aria-label="Start position" type="number" min="0" value={draft.startPos} onChange={(e) => setDraft((c) => ({ ...c, startPos: e.target.value }))} className={inputClass} /></Field><Field label="End file"><DropdownSelect value={draft.endFile} onChange={(value) => { const file = binlogs.find((item) => item.name === value); setDraft((c) => ({ ...c, endFile: value, endPos: file ? String(file.size_bytes) : c.endPos })) }} options={binlogOptions} ariaLabel="End file" searchable /></Field><Field label="End position"><input aria-label="End position" type="number" min="0" value={draft.endPos} onChange={(e) => setDraft((c) => ({ ...c, endPos: e.target.value }))} className={inputClass} /></Field></div>
            )}
            <div className="grid gap-3 md:grid-cols-2"><Field label="Database"><DropdownSelect value={draft.database} onChange={(value) => setDraft((c) => ({ ...c, database: value, tables: [] }))} options={databaseOptions} ariaLabel="Database" searchable disabled={draft.connectionID === '' || loadingContext} /></Field><Field label="Tables"><SearchableMultiSelect values={draft.tables} options={tables} onChange={(values) => setDraft((c) => ({ ...c, tables: values }))} ariaLabel="Tables" disabled={draft.database === ''} /></Field></div>
            <Field label="DML types"><div className="flex flex-wrap gap-2">{(['insert', 'update', 'delete'] as const).map((type) => <label key={type} className="inline-flex items-center gap-2 rounded-lg border border-border bg-panel-soft px-3 py-2 text-[12px] font-medium text-ink"><input type="checkbox" checked={draft.dmlTypes.includes(type)} onChange={() => setDraft((c) => ({ ...c, dmlTypes: toggle(c.dmlTypes, type) }))} />{type.toUpperCase()}</label>)}</div></Field>
            {unfiltered ? <InlineAlert tone="warning"><label className="flex items-start gap-2"><input aria-label="Acknowledge unfiltered export" type="checkbox" checked={draft.acknowledgeUnfiltered} onChange={(e) => setDraft((c) => ({ ...c, acknowledgeUnfiltered: e.target.checked }))} className="mt-0.5" /><span>Run without database or table filters.</span></label></InlineAlert> : null}
            <div className="flex justify-end"><button type="submit" disabled={!canExecute || submitting || draft.connectionID === '' || draft.dmlTypes.length === 0 || (unfiltered && !draft.acknowledgeUnfiltered)} className="inline-flex h-10 items-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-50">{submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}{submitting ? 'Queueing...' : 'Queue Export'}</button></div>
          </fieldset>
        </form>

        <aside className="rounded-lg border border-border bg-panel p-4 shadow-soft sm:p-5">
		  <div className="mb-3 flex items-center justify-between gap-2"><div className="flex items-center gap-2"><FileClock className="h-4 w-4 text-brand" /><h3 className="text-[15px] font-semibold text-ink">Binlog Files</h3></div><button type="button" disabled={!draft.connectionID || binlogs.length === 0 || probingTimes} onClick={() => void probeTimes()} className="inline-flex h-8 items-center gap-1.5 rounded-md border border-border px-2.5 text-[11px] font-semibold text-ink disabled:opacity-50">{probingTimes ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <FileClock className="h-3.5 w-3.5" />}Probe times</button></div>
		  <div className="max-h-[520px] overflow-auto"><table className="w-full text-left text-[12px]"><thead className="sticky top-0 bg-panel-soft text-muted"><tr><th className="px-3 py-2 font-semibold">File</th><th className="px-3 py-2 font-semibold">Time range</th><th className="px-3 py-2 text-right font-semibold">Size</th></tr></thead><tbody>{binlogs.map((item) => <tr key={item.name} className="border-t border-border"><td className="px-3 py-2 font-mono text-ink">{item.name}{item.active ? <span className="ml-2 text-[10px] font-sans font-semibold text-emerald-600">ACTIVE</span> : null}</td><td className="px-3 py-2 text-muted">{item.start_time ? <><span className="block">{formatDateTime(item.start_time, true)}</span><span className="block">{item.end_time ? formatDateTime(item.end_time, true) : item.active ? 'Active' : 'End pending'}</span></> : 'Not probed'}</td><td className="px-3 py-2 text-right text-muted">{formatBytes(item.size_bytes)}</td></tr>)}</tbody></table>{draft.connectionID !== '' && !loadingContext && binlogs.length === 0 ? <p className="py-8 text-center text-[12px] text-muted">No binlog files found.</p> : null}{draft.connectionID === '' ? <p className="py-8 text-center text-[12px] text-muted">Select a connection.</p> : null}</div>
        </aside>
      </div>

      <section className="overflow-hidden rounded-lg border border-border bg-panel shadow-soft">
        <div className="flex items-center justify-between border-b border-border px-4 py-3"><h3 className="text-[15px] font-semibold text-ink">Export Jobs</h3><div className="flex items-center gap-2"><span className="text-[12px] text-muted">{jobTotal} jobs</span><button type="button" title="Refresh export jobs" aria-label="Refresh export jobs" onClick={() => void refreshJobs(false, jobOffset)} className="grid h-8 w-8 place-items-center rounded-md border border-border bg-panel text-muted hover:border-border-strong hover:text-ink"><RefreshCw className="h-4 w-4" /></button></div></div>
		<div className="overflow-x-auto"><table className="w-full min-w-[980px] text-left text-[12px]"><thead className="bg-panel-soft text-muted"><tr>{['Job', 'Connection', 'Range', 'Filters', 'Status', 'Created', 'Actions'].map((label) => <th key={label} className="px-4 py-2.5 font-semibold">{label}</th>)}</tr></thead><tbody>{jobs.map((job) => { const expired = job.artifact_expires_at ? new Date(job.artifact_expires_at).getTime() <= Date.now() : false; return <tr key={job.id} className="border-t border-border align-top"><td className="px-4 py-3 font-mono text-ink">#{job.id}</td><td className="px-4 py-3 text-ink">{connections.find((item) => item.id === job.source_connection_id)?.name ?? `#${job.source_connection_id}`}</td><td className="px-4 py-3 text-muted">{rangeLabel(job)}</td><td className="px-4 py-3 text-muted">{filterLabel(job)}</td><td className="px-4 py-3"><JobStatus status={job.status} phase={job.phase} />{expired ? <p className="mt-1 text-muted">Artifacts expired</p> : null}{job.error_message ? <p className="mt-1 max-w-[260px] text-danger">{job.error_message}</p> : null}</td><td className="whitespace-nowrap px-4 py-3 text-muted">{formatDateTime(job.created_at, true)}</td><td className="px-4 py-3"><div className="flex flex-wrap gap-1.5">{canExecute && (job.status === 'queued' || job.status === 'running') ? <IconButton label={`Cancel job ${job.id}`} onClick={() => void runAction(job, 'cancel')} disabled={actionID === job.id}><Ban className="h-4 w-4" /></IconButton> : null}{canExecute && ['failed', 'cancelled', 'interrupted'].includes(job.status) ? <IconButton label={`Retry job ${job.id}`} onClick={() => void runAction(job, 'retry')} disabled={actionID === job.id}><RotateCcw className="h-4 w-4" /></IconButton> : null}{job.status === 'succeeded' && !expired ? <><IconButton label={`Preview forward SQL for job ${job.id}`} onClick={() => void showPreview(job, 'forward_sql')} disabled={actionID === job.id}><Eye className="h-4 w-4" /></IconButton><IconButton label={`Download forward SQL for job ${job.id}`} onClick={() => void download(job, 'forward_sql')} disabled={actionID === job.id}><Download className="h-4 w-4" /><span>Forward</span></IconButton><IconButton label={`Preview rollback SQL for job ${job.id}`} onClick={() => void showPreview(job, 'rollback_sql')} disabled={actionID === job.id}><Eye className="h-4 w-4" /></IconButton><IconButton label={`Download rollback SQL for job ${job.id}`} onClick={() => void download(job, 'rollback_sql')} disabled={actionID === job.id}><Download className="h-4 w-4" /><span>Rollback</span></IconButton></> : null}</div></td></tr> })}</tbody></table>{jobs.length === 0 ? <p className="py-10 text-center text-[13px] text-muted">No binlog export jobs yet.</p> : null}</div>
		{jobOffset > 0 || jobTotal > JOB_PAGE_SIZE ? <div className="border-t border-border px-4 py-3"><Pagination offset={jobOffset} pageSize={JOB_PAGE_SIZE} count={jobs.length} total={jobTotal} onChange={(nextOffset) => { setJobOffset(nextOffset); void refreshJobs(false, nextOffset) }} /></div> : null}
		</section>
		{preview ? <div className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-950/30 p-4" role="dialog" aria-modal="true" aria-label={preview.title}><div className="flex max-h-[85vh] w-full max-w-5xl flex-col overflow-hidden rounded-lg border border-border bg-panel shadow-xl"><div className="flex items-center justify-between border-b border-border px-4 py-3"><h3 className="text-[14px] font-semibold text-ink">{preview.title}</h3><button type="button" title="Close preview" aria-label="Close preview" onClick={() => setPreview(null)} className="grid h-8 w-8 place-items-center rounded-md text-muted hover:bg-panel-soft hover:text-ink"><X className="h-4 w-4" /></button></div>{preview.truncated ? <InlineAlert tone="warning">Preview is limited to the first 64 KiB. Download the artifact for the complete SQL.</InlineAlert> : null}<pre className="overflow-auto p-4 font-mono text-[12px] leading-5 text-ink"><code>{preview.sql}</code></pre></div></div> : null}
    </div>
  )
}

function Field({ label, children, className = '' }: { label: string; children: ReactNode; className?: string }) { return <div className={`block min-w-0 ${className}`}><span className="mb-1.5 block text-[12px] font-semibold text-ink">{label}</span>{children}</div> }
function IconButton({ label, onClick, disabled, children }: { label: string; onClick: () => void; disabled: boolean; children: ReactNode }) { return <button type="button" title={label} aria-label={label} onClick={onClick} disabled={disabled} className="inline-flex h-8 items-center gap-1.5 rounded-md border border-border bg-panel px-2.5 text-[11px] font-semibold text-ink hover:border-border-strong disabled:opacity-50">{children}</button> }

function JobStatus({ status, phase }: { status: BinlogExportStatus; phase: string }) {
  const style = status === 'succeeded' ? 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300' : status === 'failed' || status === 'interrupted' ? 'border-red-200 bg-red-50 text-danger dark:border-red-800 dark:bg-red-950/40' : status === 'cancelled' ? 'border-border bg-panel-soft text-muted' : 'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-300'
  return <span className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-[10px] font-semibold uppercase ${style}`}>{activeStatuses.includes(status) ? <Loader2 className="h-3 w-3 animate-spin" /> : null}{status === 'running' ? phase.replace(/_/g, ' ') : status.replace(/_/g, ' ')}</span>
}

function buildPayload(draft: Draft): CreateBinlogExportPayload {
  if (!draft.connectionID) throw new Error('DB connection is required.')
  if (draft.dmlTypes.length === 0) throw new Error('Select at least one DML type.')
  const payload: CreateBinlogExportPayload = { source_connection_id: Number(draft.connectionID), range_mode: draft.rangeMode, timezone: draft.timezone.trim() || 'UTC', database: draft.database || undefined, tables: draft.tables, dml_types: draft.dmlTypes, acknowledge_unfiltered: draft.acknowledgeUnfiltered }
  if (draft.rangeMode === 'time') {
    if (!draft.startTime || !draft.endTime) throw new Error('Start and end time are required.')
    payload.start_time = wallTimeToISO(draft.startTime, payload.timezone)
    payload.end_time = wallTimeToISO(draft.endTime, payload.timezone)
    if (payload.start_time >= payload.end_time) throw new Error('End time must be after start time.')
  } else {
    if (!draft.startFile || !draft.endFile || draft.startPos === '' || draft.endPos === '') throw new Error('Start and end binlog positions are required.')
    Object.assign(payload, { start_file: draft.startFile, start_pos: Number(draft.startPos), end_file: draft.endFile, end_pos: Number(draft.endPos) })
  }
  return payload
}

function wallTimeToISO(value: string, timezone: string) {
  const match = value.match(/^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/)
  if (!match) throw new Error('Time value is invalid.')
  const parts = match.slice(1).map(Number)
  const desired = Date.UTC(parts[0], parts[1] - 1, parts[2], parts[3], parts[4])
  let instant = desired
  for (let attempt = 0; attempt < 2; attempt += 1) {
    const formatted = new Intl.DateTimeFormat('en-CA', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).formatToParts(new Date(instant))
    const get = (type: Intl.DateTimeFormatPartTypes) => Number(formatted.find((part) => part.type === type)?.value)
    const rendered = Date.UTC(get('year'), get('month') - 1, get('day'), get('hour'), get('minute'))
    instant += desired - rendered
  }
  return new Date(instant).toISOString()
}

function toUTCInput(value: Date) { return value.toISOString().slice(0, 16) }
function toggle(values: string[], value: string) { return values.includes(value) ? values.filter((item) => item !== value) : [...values, value] }
function formatBytes(value: number) { if (value < 1024) return `${value} B`; if (value < 1024 ** 2) return `${(value / 1024).toFixed(1)} KiB`; return `${(value / 1024 ** 2).toFixed(1)} MiB` }
function mergeBinlogStart(items: BinlogFile[], file: string, startTime: string) {
  const index = items.findIndex((item) => item.name === file)
  if (index < 0) return items
  return items.map((item, itemIndex) => itemIndex === index
    ? { ...item, start_time: startTime }
    : itemIndex === index - 1 ? { ...item, end_time: startTime } : item)
}
function rangeLabel(job: BinlogExportJob) { return job.range_mode === 'time' ? `${formatDateTime(job.requested_start_time)} to ${formatDateTime(job.requested_end_time)}` : `${job.requested_start_file}:${job.requested_start_pos} to ${job.requested_end_file}:${job.requested_end_pos}` }
function filterLabel(job: BinlogExportJob) { const scope = job.source_database_name ? `${job.source_database_name}${job.source_tables.length ? ` / ${job.source_tables.join(', ')}` : ''}` : 'All databases'; return `${scope} · ${job.dml_types.join(', ').toUpperCase()}` }
function apiMessage(error: unknown, fallback: string) { return error instanceof ApiError ? error.message : fallback }

function getTimezoneNames() {
  const intl = Intl as typeof Intl & { supportedValuesOf?: (key: 'timeZone') => string[] }
  const names = intl.supportedValuesOf?.('timeZone') ?? []
  return ['UTC', ...names.filter((name) => name !== 'UTC')]
}

function timezoneLabel(timezone: string) {
  try {
    const offset = new Intl.DateTimeFormat('en', { timeZone: timezone, timeZoneName: 'longOffset' })
      .formatToParts(now)
      .find((part) => part.type === 'timeZoneName')?.value
      .replace('GMT', 'UTC')
    return `${timezone} (${offset === 'UTC' ? 'UTC+00:00' : offset})`
  } catch {
    return timezone
  }
}
