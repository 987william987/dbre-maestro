import { useCallback, useEffect, useMemo, useRef, useState, type MutableRefObject, type ReactNode } from 'react'
import { Database, Download, Eye, Loader2, Play } from 'lucide-react'
import { SearchableMultiSelect } from '@/modules/binlog-export/components/SearchableMultiSelect'
import { SchemaJobsPanel } from '@/modules/table-schemas/components/SchemaJobsPanel'
import { ApiError } from '@/shared/api/client'
import { useAuth } from '@/shared/auth/AuthContext'
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { PageTabs } from '@/shared/ui/PageTabs'
import { Switch } from '@/shared/ui/Switch'
import { createTableSchemaSyncJob, downloadTableSchemaExport, listTableSchemaConnections, listTableSchemaDatabases, listTableSchemaTables, previewTableSchemaExport, previewTableSchemaSync, type TableSchemaConnection, type TableSchemaExportRequest, type TableSchemaPreview, type TableSchemaSyncPreview, type TableSchemaSyncRequest, type TableSchemaTransformation } from '@/modules/table-schemas/api'

type Tab = 'export' | 'sync'
type Draft = { sourceConnection: string; sourceDatabase: string; tables: string[]; targetConnection: string; targetDatabase: string; transformation: TableSchemaTransformation }
const EMPTY_TRANSFORMATION: TableSchemaTransformation = { reset_auto_increment: false, engine: '', charset: '', collation: '', row_format: '' }
const EMPTY_DRAFT: Draft = { sourceConnection: '', sourceDatabase: '', tables: [], targetConnection: '', targetDatabase: '', transformation: EMPTY_TRANSFORMATION }
const inputClass = 'h-10 w-full rounded-lg border border-border bg-panel px-3 text-[13px] text-ink outline-none focus:border-border-strong disabled:opacity-50'

export function TableSchemasPage() {
  const { user } = useAuth()
  const canSync = user?.permissions.includes('table_schemas.sync') ?? false
  const [tab, setTab] = useState<Tab>('export')
  const [draft, setDraft] = useState<Draft>(EMPTY_DRAFT)
  const [connections, setConnections] = useState<TableSchemaConnection[]>([])
  const [sourceDatabases, setSourceDatabases] = useState<string[]>([])
  const [targetDatabases, setTargetDatabases] = useState<string[]>([])
  const [tables, setTables] = useState<string[]>([])
  const [preview, setPreview] = useState<TableSchemaPreview | TableSchemaSyncPreview | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [jobsRefreshKey, setJobsRefreshKey] = useState(0)
  const requestRef = useRef<AbortController | null>(null)
  const connectionOptions = useMemo(() => [{ value: '', label: 'Select connection' }, ...connections.map((item) => ({ value: String(item.id), label: item.name }))], [connections])
  const sourceDatabaseOptions = useMemo(() => selectOptions(sourceDatabases), [sourceDatabases])
  const targetDatabaseOptions = useMemo(() => selectOptions(targetDatabases), [targetDatabases])
  const valid = Boolean(draft.sourceConnection && draft.sourceDatabase && draft.tables.length && (tab === 'export' || (draft.targetConnection && draft.targetDatabase)))
  const handleJobsError = useCallback((next: string) => { setError(next); setNotice('') }, [])
  const handleJobsNotice = useCallback((next: string) => { setNotice(next); setError('') }, [])

  useEffect(() => {
    const controller = new AbortController()
    listTableSchemaConnections(controller.signal).then(setConnections).catch((cause) => !controller.signal.aborted && setError(message(cause, 'Failed to load connections.'))).finally(() => !controller.signal.aborted && setLoading(false))
    return () => controller.abort()
  }, [])
  useEffect(() => {
    setSourceDatabases([]); setTables([]); setBusy(false); invalidate(requestRef, setPreview)
    if (!draft.sourceConnection) return
    const controller = new AbortController()
    listTableSchemaDatabases(Number(draft.sourceConnection), controller.signal).then(setSourceDatabases).catch((cause) => !controller.signal.aborted && setError(message(cause, 'Failed to load source databases.')))
    return () => controller.abort()
  }, [draft.sourceConnection])
  useEffect(() => {
    setTables([]); setBusy(false); invalidate(requestRef, setPreview)
    if (!draft.sourceConnection || !draft.sourceDatabase) return
    const controller = new AbortController()
    listTableSchemaTables(Number(draft.sourceConnection), draft.sourceDatabase, controller.signal).then((result) => setTables(result.items.map((item) => item.name))).catch((cause) => !controller.signal.aborted && setError(message(cause, 'Failed to load source tables.')))
    return () => controller.abort()
  }, [draft.sourceConnection, draft.sourceDatabase])
  useEffect(() => {
    setTargetDatabases([]); setBusy(false); invalidate(requestRef, setPreview)
    if (!draft.targetConnection) return
    const controller = new AbortController()
    listTableSchemaDatabases(Number(draft.targetConnection), controller.signal).then(setTargetDatabases).catch((cause) => !controller.signal.aborted && setError(message(cause, 'Failed to load target databases.')))
    return () => controller.abort()
  }, [draft.targetConnection])
  useEffect(() => { setBusy(false); invalidate(requestRef, setPreview) }, [tab, draft.sourceDatabase, draft.tables, draft.targetDatabase, draft.transformation])

  function exportPayload(): TableSchemaExportRequest { return { source: { connection_id: Number(draft.sourceConnection), database: draft.sourceDatabase, tables: draft.tables }, transformation: draft.transformation } }
  function syncPayload(): TableSchemaSyncRequest { return { ...exportPayload(), target: { connection_id: Number(draft.targetConnection), database: draft.targetDatabase } } }
  async function runPreview() {
    if (!valid) return
    requestRef.current?.abort(); const controller = new AbortController(); requestRef.current = controller
    setBusy(true); setError(''); setNotice('')
    try { const result = tab === 'export' ? await previewTableSchemaExport(exportPayload(), controller.signal) : await previewTableSchemaSync(syncPayload(), controller.signal); if (!controller.signal.aborted) setPreview(result) }
    catch (cause) { if (!controller.signal.aborted) setError(message(cause, 'Preview failed.')) }
    finally { if (!controller.signal.aborted) setBusy(false) }
  }
  async function download() { setBusy(true); setError(''); try { await downloadTableSchemaExport(exportPayload()) } catch (cause) { setError(message(cause, 'Download failed.')) } finally { setBusy(false) } }
  async function createJob() {
    if (!preview || !('preview_token' in preview)) return
    setBusy(true); setError('')
    try { const job = await createTableSchemaSyncJob({ ...syncPayload(), preview_token: preview.preview_token }); setNotice(`Schema sync job #${job.id} queued.`); setPreview(null); setJobsRefreshKey((value) => value + 1) }
    catch (cause) { setError(message(cause, 'Failed to create sync job.')) }
    finally { setBusy(false); setConfirming(false) }
  }

  if (loading) return <div className="p-3 sm:p-4"><LoadingBlock message="Loading table schemas..." className="min-h-[360px] rounded-lg border-border bg-panel" /></div>
  return <div className="space-y-4 p-3 sm:p-4 lg:p-6">
    {error ? <InlineAlert tone="error">{error}</InlineAlert> : null}{notice ? <InlineAlert tone="success">{notice}</InlineAlert> : null}
    <section className="rounded-lg border border-border bg-panel">
      <header className="flex items-center gap-3 border-b border-border px-4 py-3"><span className="grid h-9 w-9 place-items-center rounded-lg border border-border bg-panel-soft text-muted">{tab === 'export' ? <Download className="h-4 w-4" /> : <Database className="h-4 w-4" />}</span><h1 className="text-lg font-semibold text-ink">Table Schemas</h1></header>
      <PageTabs className="px-4" items={[{ key: 'export', label: 'Export', active: tab === 'export', onClick: () => setTab('export') }, { key: 'sync', label: 'Sync', active: tab === 'sync', onClick: () => setTab('sync') }]} />
      <div className="grid gap-4 p-4 xl:grid-cols-[minmax(300px,0.8fr)_minmax(0,1.2fr)]">
        <div className="space-y-4">
          <Panel title="Source"><Field label="Connection"><DropdownSelect value={draft.sourceConnection} onChange={(value) => setDraft((current) => ({ ...current, sourceConnection: value, sourceDatabase: '', tables: [] }))} options={connectionOptions} ariaLabel="Source connection" searchable /></Field><Field label="Database"><DropdownSelect value={draft.sourceDatabase} onChange={(value) => setDraft((current) => ({ ...current, sourceDatabase: value, tables: [] }))} options={sourceDatabaseOptions} ariaLabel="Source database" searchable disabled={!draft.sourceConnection} /></Field><Field label="Tables"><SearchableMultiSelect values={draft.tables} options={tables} onChange={(values) => setDraft((current) => ({ ...current, tables: values }))} ariaLabel="Source tables" emptyLabel="Select tables" disabled={!draft.sourceDatabase} /></Field></Panel>
          {tab === 'sync' ? <Panel title="Target"><Field label="Connection"><DropdownSelect value={draft.targetConnection} onChange={(value) => setDraft((current) => ({ ...current, targetConnection: value, targetDatabase: '' }))} options={connectionOptions} ariaLabel="Target connection" searchable /></Field><Field label="Database"><DropdownSelect value={draft.targetDatabase} onChange={(value) => setDraft((current) => ({ ...current, targetDatabase: value }))} options={targetDatabaseOptions} ariaLabel="Target database" searchable disabled={!draft.targetConnection} /></Field></Panel> : null}
          <Transformation value={draft.transformation} onChange={(transformation) => setDraft((current) => ({ ...current, transformation }))} />
          <button type="button" disabled={!valid || busy || (tab === 'sync' && !canSync)} onClick={() => void runPreview()} className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-semibold text-white disabled:opacity-50">{busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Eye className="h-4 w-4" />}Preview</button>
        </div>
        <PreviewPanel preview={preview} tab={tab} busy={busy} canSync={canSync} onDownload={() => void download()} onSync={() => setConfirming(true)} />
      </div>
    </section>
    <SchemaJobsPanel canSync={canSync} refreshKey={jobsRefreshKey} onError={handleJobsError} onNotice={handleJobsNotice} />
    <ConfirmDialog open={confirming} title="Create tables on target" description={<>Create {preview?.tables.length ?? 0} tables in <span className="font-mono text-ink">{draft.targetDatabase}</span>? Completed MySQL DDL is not rolled back automatically.</>} confirmLabel="Queue Sync" loading={busy} onCancel={() => setConfirming(false)} onConfirm={() => void createJob()} />
  </div>
}

function Transformation({ value, onChange }: { value: TableSchemaTransformation; onChange: (value: TableSchemaTransformation) => void }) {
  const set = (key: keyof TableSchemaTransformation, next: string | boolean) => onChange({ ...value, [key]: next })
  return <Panel title="Transformation"><label className="flex items-center justify-between text-[12px] font-medium text-ink"><span>Reset AUTO_INCREMENT</span><Switch checked={value.reset_auto_increment} onChange={(next) => set('reset_auto_increment', next)} ariaLabel="Reset AUTO_INCREMENT" /></label><div className="grid gap-3 sm:grid-cols-2"><Field label="Engine"><input aria-label="Engine" className={inputClass} value={value.engine} onChange={(event) => set('engine', event.target.value)} placeholder="Inherit" /></Field><Field label="Charset"><input aria-label="Charset" className={inputClass} value={value.charset} onChange={(event) => set('charset', event.target.value)} placeholder="Inherit" /></Field><Field label="Collation"><input aria-label="Collation" className={inputClass} value={value.collation} onChange={(event) => set('collation', event.target.value)} placeholder="Inherit" /></Field><Field label="Row format"><DropdownSelect value={value.row_format} onChange={(next) => set('row_format', next)} ariaLabel="Row format" options={[{ value: '', label: 'Inherit' }, ...['DEFAULT', 'DYNAMIC', 'COMPACT', 'COMPRESSED', 'REDUNDANT'].map((item) => ({ value: item, label: item }))]} /></Field></div></Panel>
}
function PreviewPanel({ preview, tab, busy, canSync, onDownload, onSync }: { preview: TableSchemaPreview | TableSchemaSyncPreview | null; tab: Tab; busy: boolean; canSync: boolean; onDownload: () => void; onSync: () => void }) {
  if (!preview) return <div className="grid min-h-[420px] place-items-center rounded-lg border border-dashed border-border bg-panel-soft/40 p-6 text-center text-sm text-muted">Select source tables and run preview.</div>
  return <div className="min-w-0 overflow-hidden rounded-lg border border-border"><div className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-4 py-3"><div><h2 className="text-[14px] font-semibold text-ink">Preview</h2><p className="text-[12px] text-muted">Order: {preview.order.join(' → ')}</p></div><button type="button" disabled={busy || (tab === 'sync' && !canSync)} onClick={tab === 'export' ? onDownload : onSync} className="inline-flex h-9 items-center gap-2 rounded-lg bg-brand px-3 text-[12px] font-semibold text-white disabled:opacity-50">{tab === 'export' ? <Download className="h-4 w-4" /> : <Play className="h-4 w-4" />}{tab === 'export' ? 'Download SQL' : 'Queue Sync'}</button></div>{preview.warnings.map((warning) => <div key={warning} className="border-b border-border px-4 py-2 text-[12px] text-warning">{warning}</div>)}<div className="overflow-x-auto"><table className="w-full min-w-[620px] text-left text-[12px]"><thead className="bg-panel-soft text-muted"><tr>{['Table', 'Engine', 'Charset', 'Collation', 'Row format'].map((name) => <th key={name} className="px-4 py-2.5 font-medium">{name}</th>)}</tr></thead><tbody>{preview.tables.map((table) => <tr key={table.name} className="border-t border-border"><td className="px-4 py-3 font-mono text-ink">{table.name}</td><td className="px-4 py-3">{table.output.engine || '—'}</td><td className="px-4 py-3">{table.output.charset || '—'}</td><td className="px-4 py-3">{table.output.collation || '—'}</td><td className="px-4 py-3">{table.output.row_format || '—'}</td></tr>)}</tbody></table></div><pre className="max-h-[360px] overflow-auto border-t border-border bg-code p-4 text-[12px] leading-5 text-code-foreground">{preview.script}</pre></div>
}
function Panel({ title, children }: { title: string; children: ReactNode }) { return <section className="space-y-3 rounded-lg border border-border p-4"><h2 className="text-[14px] font-semibold text-ink">{title}</h2>{children}</section> }
function Field({ label, children }: { label: string; children: ReactNode }) { return <label className="grid gap-1.5 text-[12px] font-medium text-muted"><span>{label}</span>{children}</label> }
function selectOptions(items: string[]) { return [{ value: '', label: 'Select database' }, ...items.map((name) => ({ value: name, label: name }))] }
function invalidate(ref: MutableRefObject<AbortController | null>, setPreview: (value: null) => void) { ref.current?.abort(); ref.current = null; setPreview(null) }
function message(cause: unknown, fallback: string) { return cause instanceof ApiError || cause instanceof Error ? cause.message : fallback }
