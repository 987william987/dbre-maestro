import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { Ban, ChevronRight, Loader2, RefreshCw, RotateCcw } from 'lucide-react'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog'
import { Pagination } from '@/shared/ui/Pagination'
import { cancelTableSchemaJob, getTableSchemaJob, listTableSchemaJobs, retryTableSchemaJob, type TableSchemaJob, type TableSchemaJobItem } from '@/modules/table-schemas/api'

const PAGE_SIZE = 20
const active = new Set(['queued', 'running', 'cancel_requested'])
const retryable = new Set(['failed', 'cancelled', 'interrupted'])
type Pending = { action: 'cancel' | 'retry'; job: TableSchemaJob }

export function SchemaJobsPanel({ canSync, refreshKey, onError, onNotice }: { canSync: boolean; refreshKey: number; onError: (message: string) => void; onNotice: (message: string) => void }) {
  const [jobs, setJobs] = useState<TableSchemaJob[]>([])
  const [total, setTotal] = useState(0)
  const [offset, setOffset] = useState(0)
  const [loading, setLoading] = useState(true)
  const [detail, setDetail] = useState<{ job: TableSchemaJob; items: TableSchemaJobItem[] } | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [pending, setPending] = useState<Pending | null>(null)
  const [acting, setActing] = useState(false)
  const detailRequest = useRef<AbortController | null>(null)
  const listRequest = useRef<AbortController | null>(null)

  const loadJobs = useCallback(async (silent = false) => {
	listRequest.current?.abort(); const controller = new AbortController(); listRequest.current = controller
    if (!silent) setLoading(true)
	try { const result = await listTableSchemaJobs(PAGE_SIZE, offset, controller.signal); if (!controller.signal.aborted) { setJobs(result.items); setTotal(result.total) } }
	catch (cause) { if (!silent && !controller.signal.aborted) onError(errorMessage(cause, 'Failed to load schema sync jobs.')) }
	finally { if (!silent && !controller.signal.aborted) setLoading(false) }
  }, [offset, onError])

  useEffect(() => { void loadJobs() }, [loadJobs, refreshKey])
  useEffect(() => {
    if (!jobs.some((job) => active.has(job.status))) return
    const timer = window.setInterval(() => void loadJobs(true), 2000)
    return () => window.clearInterval(timer)
  }, [jobs, loadJobs])
  useEffect(() => () => { detailRequest.current?.abort(); listRequest.current?.abort() }, [])

  async function openDetail(id: number) {
    detailRequest.current?.abort(); const controller = new AbortController(); detailRequest.current = controller
    setDetailLoading(true)
    try { const result = await getTableSchemaJob(id, controller.signal); if (!controller.signal.aborted) setDetail(result) }
    catch (cause) { if (!controller.signal.aborted) onError(errorMessage(cause, 'Failed to load sync job detail.')) }
    finally { if (!controller.signal.aborted) setDetailLoading(false) }
  }
  async function runAction() {
    if (!pending) return
    setActing(true)
    try {
      if (pending.action === 'cancel') await cancelTableSchemaJob(pending.job.id)
      else await retryTableSchemaJob(pending.job.id)
      onNotice(pending.action === 'cancel' ? `Cancellation requested for schema sync job #${pending.job.id}.` : `Retry queued for schema sync job #${pending.job.id}.`)
      setPending(null); setDetail(null); if (pending.action === 'retry') setOffset(0); await loadJobs(true)
    } catch (cause) { onError(errorMessage(cause, `Failed to ${pending.action} schema sync job.`)); setPending(null) }
    finally { setActing(false) }
  }

  return <>
    <section className="overflow-hidden rounded-lg border border-border bg-panel">
      <header className="flex items-center justify-between gap-3 border-b border-border px-4 py-3"><div><h2 className="text-[15px] font-semibold text-ink">Sync Jobs</h2><p className="text-[12px] text-muted">{total} jobs</p></div><button type="button" aria-label="Refresh sync jobs" title="Refresh sync jobs" onClick={() => void loadJobs()} className="grid h-9 w-9 place-items-center rounded-lg border border-border text-muted hover:text-ink"><RefreshCw className={loading ? 'h-4 w-4 animate-spin' : 'h-4 w-4'} /></button></header>
      <div className="overflow-x-auto"><table className="w-full min-w-[900px] text-left text-[12px]"><thead className="bg-panel-soft text-muted"><tr>{['Job', 'Source', 'Target', 'Progress', 'Status', 'Created', 'Actions'].map((label) => <th key={label} className="px-4 py-2.5 font-medium">{label}</th>)}</tr></thead><tbody>{jobs.map((job) => <tr key={job.id} className="border-t border-border"><td className="px-4 py-3 font-mono text-ink">#{job.id}</td><td className="px-4 py-3">#{job.source_connection_id} / {job.source_database}</td><td className="px-4 py-3">#{job.target_connection_id} / {job.target_database}</td><td className="px-4 py-3">{job.created_count}/{job.table_count}{job.failed_count ? ` · ${job.failed_count} failed` : ''}</td><td className="px-4 py-3"><JobStatus status={job.status} />{job.error_code ? <p className="mt-1 text-danger">{job.error_code}</p> : null}</td><td className="px-4 py-3 whitespace-nowrap text-muted">{formatDateTime(job.created_at, true)}</td><td className="px-4 py-3"><div className="flex gap-1.5"><IconButton label={`View job ${job.id}`} onClick={() => void openDetail(job.id)}><ChevronRight className="h-4 w-4" /></IconButton>{canSync && active.has(job.status) ? <IconButton label={`Cancel job ${job.id}`} onClick={() => setPending({ action: 'cancel', job })}><Ban className="h-4 w-4" /></IconButton> : null}{canSync && retryable.has(job.status) ? <IconButton label={`Retry job ${job.id}`} onClick={() => setPending({ action: 'retry', job })}><RotateCcw className="h-4 w-4" /></IconButton> : null}</div></td></tr>)}</tbody></table>{!loading && jobs.length === 0 ? <p className="px-4 py-10 text-center text-[13px] text-muted">No schema sync jobs found.</p> : null}</div>
      <div className="border-t border-border px-4 py-3"><Pagination offset={offset} pageSize={PAGE_SIZE} count={jobs.length} total={total} onChange={setOffset} /></div>
    </section>
    {detailLoading ? <section className="rounded-lg border border-border bg-panel p-6 text-center text-sm text-muted"><Loader2 className="mx-auto mb-2 h-4 w-4 animate-spin" />Loading job detail...</section> : null}
    {detail ? <section className="overflow-hidden rounded-lg border border-border bg-panel"><header className="border-b border-border px-4 py-3"><h2 className="text-[15px] font-semibold text-ink">Job #{detail.job.id}</h2><p className="text-[12px] text-muted">{detail.job.source_database} → {detail.job.target_database}</p></header><div className="overflow-x-auto"><table className="w-full min-w-[620px] text-left text-[12px]"><thead className="bg-panel-soft text-muted"><tr>{['Order', 'Table', 'Status', 'Duration', 'Error'].map((label) => <th key={label} className="px-4 py-2.5 font-medium">{label}</th>)}</tr></thead><tbody>{detail.items.map((item) => <tr key={item.id} className="border-t border-border"><td className="px-4 py-3">{item.dependency_order + 1}</td><td className="px-4 py-3 font-mono text-ink">{item.table_name}</td><td className="px-4 py-3"><JobStatus status={item.status} /></td><td className="px-4 py-3">{item.duration_ms == null ? '—' : `${item.duration_ms} ms`}</td><td className="px-4 py-3 text-danger">{item.error_code ?? '—'}</td></tr>)}</tbody></table></div></section> : null}
    <ConfirmDialog open={pending !== null} title={pending?.action === 'cancel' ? 'Cancel schema sync job' : 'Retry schema sync job'} description={pending?.action === 'cancel' ? 'The current CREATE may finish before cancellation is observed. Completed tables will be preserved.' : 'Retry only proceeds when previously created target tables still match their recorded hashes.'} confirmLabel={pending?.action === 'cancel' ? 'Request Cancel' : 'Retry'} tone={pending?.action === 'cancel' ? 'danger' : 'default'} loading={acting} onCancel={() => setPending(null)} onConfirm={() => void runAction()} />
  </>
}

function JobStatus({ status }: { status: string }) { const tone = status === 'completed' || status === 'created' ? 'text-success border-success/30 bg-success/10' : status === 'failed' ? 'text-danger border-danger/30 bg-danger/10' : status === 'running' || status === 'queued' || status === 'pending' ? 'text-warning border-warning/30 bg-warning/10' : 'text-muted border-border bg-panel-soft'; return <span className={`inline-flex rounded-full border px-2 py-0.5 text-[10px] font-semibold ${tone}`}>{status.replace(/_/g, ' ')}</span> }
function IconButton({ label, onClick, children }: { label: string; onClick: () => void; children: ReactNode }) { return <button type="button" aria-label={label} title={label} onClick={onClick} className="grid h-8 w-8 place-items-center rounded-md border border-border text-muted hover:text-ink">{children}</button> }
function errorMessage(cause: unknown, fallback: string) { return cause instanceof ApiError || cause instanceof Error ? cause.message : fallback }
