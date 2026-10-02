import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { CircleStop, Power, RefreshCw, Search, ShieldCheck, TimerOff } from 'lucide-react'
import {
  cancelPrefix, cancelSession, createLoopJob, getAWSTopology, listAWSClusters, listLoopJobs, listSessionConnections,
  listSessions, previewPrefix, stopLoopJob, terminateSession, type DBSession, type PrefixPreview, type SessionCluster,
  type SessionLoopJob, type SessionTarget,
} from '@/modules/session-management/api'
import { ApiError } from '@/shared/api/client'
import { useAuth } from '@/shared/auth/AuthContext'
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { Pagination } from '@/shared/ui/Pagination'

const inputClass = 'h-10 w-full rounded-lg border border-border bg-panel px-3 text-[13px] text-ink outline-none focus:border-border-strong disabled:opacity-60'
const SESSION_PAGE_SIZE = 25
const JOB_PAGE_SIZE = 20
const terminalStatuses = new Set(['completed', 'stopped', 'expired', 'limit_reached', 'failed', 'interrupted'])

export function SessionManagementPage() {
  const { user } = useAuth()
  const [connections, setConnections] = useState<Awaited<ReturnType<typeof listSessionConnections>>>([])
  const [connectionID, setConnectionID] = useState('')
  const [mode, setMode] = useState<'aws' | 'manual'>('aws')
  const [clusters, setClusters] = useState<SessionCluster[]>([])
  const [clusterKey, setClusterKey] = useState('')
  const [nodeID, setNodeID] = useState('')
  const [manualHost, setManualHost] = useState('')
  const [manualPort, setManualPort] = useState('')
  const [sessions, setSessions] = useState<DBSession[]>([])
  const [keyword, setKeyword] = useState('')
  const [userFilter, setUserFilter] = useState('all')
  const [databaseFilter, setDatabaseFilter] = useState('all')
  const [stateFilter, setStateFilter] = useState('all')
  const [protectionFilter, setProtectionFilter] = useState('all')
  const [sessionOffset, setSessionOffset] = useState(0)
  const [refreshSeconds, setRefreshSeconds] = useState('0')
  const [loading, setLoading] = useState(true)
  const [loadingTopology, setLoadingTopology] = useState(false)
  const [loadingSessions, setLoadingSessions] = useState(false)
  const [truncated, setTruncated] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [pendingAction, setPendingAction] = useState<{ session: DBSession; action: 'cancel' | 'terminate' } | null>(null)
  const [actionLoading, setActionLoading] = useState(false)
  const requestRef = useRef<AbortController | null>(null)
  const [prefixDatabase, setPrefixDatabase] = useState('')
  const [prefix, setPrefix] = useState('')
  const [minimumAge, setMinimumAge] = useState('0')
  const [prefixResult, setPrefixResult] = useState<PrefixPreview | null>(null)
  const [prefixLoading, setPrefixLoading] = useState(false)
  const [pendingPrefixAction, setPendingPrefixAction] = useState<'cancel' | 'loop' | null>(null)
  const [intervalSeconds, setIntervalSeconds] = useState('2')
  const [durationSeconds, setDurationSeconds] = useState('600')
  const [maxKills, setMaxKills] = useState('100')
  const [jobs, setJobs] = useState<SessionLoopJob[]>([])
  const [jobOffset, setJobOffset] = useState(0)
  const [jobsLoading, setJobsLoading] = useState(false)

  useEffect(() => {
    let active = true
    listSessionConnections().then((items) => { if (active) setConnections(items) })
      .catch((err) => { if (active) setError(message(err, 'Failed to load DB connections.')) })
      .finally(() => { if (active) setLoading(false) })
    listLoopJobs(JOB_PAGE_SIZE, 0).then((items) => { if (active) setJobs(items) })
      .catch((err) => { if (active) setError(message(err, 'Failed to load loop jobs.')) })
    return () => { active = false }
  }, [])

  const selectedConnection = connections.find((item) => String(item.id) === connectionID)
  const selectedCluster = clusters.find((item) => `${item.region}|${item.id}` === clusterKey)
  const target = useMemo<SessionTarget | null>(() => {
    if (!connectionID) return null
    if (mode === 'aws') return selectedCluster && nodeID ? { connection_id: Number(connectionID), mode, region: selectedCluster.region, cluster_id: selectedCluster.id, node_id: nodeID } : null
    return manualHost.trim() && Number(manualPort) > 0 ? { connection_id: Number(connectionID), mode, host: manualHost.trim(), port: Number(manualPort) } : null
  }, [connectionID, manualHost, manualPort, mode, nodeID, selectedCluster])
  const targetKey = target ? JSON.stringify(target) : ''
  const canRefresh = Boolean(target && selectedConnection?.operations_configured)
  const canKill = user?.permissions.includes('db_sessions.kill') === true
  const canLoop = user?.permissions.includes('db_sessions.loop_kill') === true
  const supportsPrefix = Boolean(selectedConnection && selectedConnection.db_type !== 'redis')

  const refreshSessions = useCallback(async (silent = false) => {
    if (!target) return
    requestRef.current?.abort()
    const controller = new AbortController()
    requestRef.current = controller
    if (!silent) setLoadingSessions(true)
    setError('')
    try {
      const result = await listSessions(target, controller.signal)
      if (!controller.signal.aborted) { setSessions(result.items); setTruncated(result.truncated) }
    } catch (err) {
      if (!controller.signal.aborted) setError(message(err, 'Failed to read database sessions.'))
    } finally {
      if (requestRef.current === controller) { requestRef.current = null; setLoadingSessions(false) }
    }
  }, [target])

  useEffect(() => {
    requestRef.current?.abort()
    setSessions([]); setTruncated(false); setSessionOffset(0); setPrefixResult(null)
  }, [targetKey])

  useEffect(() => {
    if (!canRefresh || refreshSeconds === '0') return
    let timer: number | undefined
    const start = () => { if (!document.hidden) timer = window.setInterval(() => void refreshSessions(true), Number(refreshSeconds) * 1000) }
    const stop = () => { if (timer !== undefined) window.clearInterval(timer); timer = undefined }
    const onVisibility = () => { stop(); if (document.hidden) requestRef.current?.abort(); else { void refreshSessions(true); start() } }
    start(); document.addEventListener('visibilitychange', onVisibility)
    return () => { stop(); document.removeEventListener('visibilitychange', onVisibility); requestRef.current?.abort() }
  }, [canRefresh, refreshSeconds, refreshSessions])
  useEffect(() => () => requestRef.current?.abort(), [])

  useEffect(() => {
    setClusters([]); setClusterKey(''); setNodeID(''); setError('')
    if (!connectionID || mode !== 'aws') return
    setLoadingTopology(true)
    listAWSClusters(Number(connectionID)).then(setClusters).catch((err) => setError(message(err, 'Failed to load live AWS topology.'))).finally(() => setLoadingTopology(false))
  }, [connectionID, mode])

  async function selectCluster(value: string) {
    setClusterKey(value); setNodeID(''); setSessions([])
    const [region, clusterID] = value.split('|')
    if (!region || !clusterID || !connectionID) return
    setLoadingTopology(true); setError('')
    try {
      const topology = await getAWSTopology(Number(connectionID), region, clusterID)
      setClusters((current) => current.map((item) => item.region === region && item.id === clusterID ? topology : item))
    } catch (err) { setError(message(err, 'Failed to refresh selected topology.')) }
    finally { setLoadingTopology(false) }
  }

  const filtered = useMemo(() => {
    const term = keyword.trim().toLowerCase()
    return sessions.filter((item) => {
      const values = [item.id, item.user, item.database, item.client, item.state, item.query, item.command]
      return (!term || values.some((value) => value?.toLowerCase().includes(term))) && (userFilter === 'all' || item.user === userFilter) &&
        (databaseFilter === 'all' || item.database === databaseFilter) && (stateFilter === 'all' || item.state === stateFilter) &&
        (protectionFilter === 'all' || (protectionFilter === 'protected') === item.protected)
    })
  }, [databaseFilter, keyword, protectionFilter, sessions, stateFilter, userFilter])
  const pagedSessions = filtered.slice(sessionOffset, sessionOffset + SESSION_PAGE_SIZE)
  useEffect(() => setSessionOffset(0), [databaseFilter, keyword, protectionFilter, stateFilter, userFilter])

  async function runPendingAction() {
    if (!pendingAction || !target) return
    const expected = { user: pendingAction.session.user ?? '', database: pendingAction.session.database ?? '', client: pendingAction.session.client ?? '', query_hash: pendingAction.session.query_hash, backend_start: pendingAction.session.backend_start ?? '' }
    setActionLoading(true); setError(''); setNotice('')
    try {
      if (pendingAction.action === 'cancel') await cancelSession(pendingAction.session.id, { ...target, expected })
      else await terminateSession(pendingAction.session.id, { ...target, expected })
      setNotice(pendingAction.action === 'cancel' ? 'Query cancellation requested.' : selectedConnection?.db_type === 'redis' ? 'Redis client disconnected.' : 'Session termination requested.')
      setPendingAction(null); await refreshSessions()
    } catch (err) { setError(message(err, 'Failed to modify the database session.')) }
    finally { setActionLoading(false) }
  }

  function clearPreview() { setPrefixResult(null); setPendingPrefixAction(null) }
  async function runPreview() {
    if (!target) return
    setPrefixLoading(true); setError(''); setNotice(''); clearPreview()
    try { setPrefixResult(await previewPrefix({ ...target, database: prefixDatabase.trim(), prefix, minimum_age_seconds: Number(minimumAge) })) }
    catch (err) { setError(message(err, 'Failed to preview matching sessions.')) }
    finally { setPrefixLoading(false) }
  }
  async function runPrefixAction() {
    if (!target || !prefixResult || !pendingPrefixAction) return
    if (new Date(prefixResult.expires_at).getTime() <= Date.now()) {
      setError('The prefix preview has expired. Run preview again.'); clearPreview(); return
    }
    setPrefixLoading(true); setError(''); setNotice('')
    try {
      if (pendingPrefixAction === 'cancel') {
        const result = await cancelPrefix({ ...target, preview_token: prefixResult.preview_token })
        setNotice(`Prefix cancellation finished: ${result.cancelled_count} cancelled, ${result.skipped_count} skipped, ${result.failed_count} failed.`)
      } else {
        await createLoopJob({ ...target, preview_token: prefixResult.preview_token, interval_seconds: Number(intervalSeconds), duration_seconds: Number(durationSeconds), max_kills: Number(maxKills) })
        setNotice('Loop cancellation job created.'); await refreshJobs(0)
      }
      clearPreview(); await refreshSessions()
    } catch (err) { setError(message(err, 'Failed to run prefix action.')) }
    finally { setPrefixLoading(false) }
  }
  async function refreshJobs(offset = jobOffset) {
    setJobsLoading(true); setError('')
    try { setJobs(await listLoopJobs(JOB_PAGE_SIZE, offset)); setJobOffset(offset) }
    catch (err) { setError(message(err, 'Failed to load loop jobs.')) }
    finally { setJobsLoading(false) }
  }
  async function stopJob(id: number) {
    setJobsLoading(true); setError(''); setNotice('')
    try { await stopLoopJob(id); setNotice('Loop cancellation job stopped.'); await refreshJobs() }
    catch (err) { setError(message(err, 'Failed to stop loop job.')) }
    finally { setJobsLoading(false) }
  }

  if (loading) return <LoadingBlock message="Loading Session Management..." />
  const uniqueOptions = (values: Array<string | undefined>) => Array.from(new Set(values.filter(Boolean) as string[])).sort().map((value) => ({ value, label: value }))
  const prefixValid = Boolean(target && canKill && prefixDatabase.trim() && prefix.trim().length >= 16 && Number(minimumAge) >= 0)

  return <div className="space-y-4 p-3 sm:p-4">
    {error ? <InlineAlert tone="error">{error}</InlineAlert> : null}{notice ? <InlineAlert tone="success">{notice}</InlineAlert> : null}
    <section className="rounded-lg border border-border bg-panel">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-3"><div><h1 className="text-lg font-semibold text-ink">Session Management</h1><p className="text-[12px] text-muted">Inspect live MySQL, PostgreSQL, and Redis sessions on a physical node.</p></div><div className="flex gap-2"><DropdownSelect ariaLabel="Auto refresh" value={refreshSeconds} onChange={setRefreshSeconds} options={[{ value: '0', label: 'Manual refresh' }, ...[2, 5, 10, 30].map((value) => ({ value: String(value), label: `Every ${value}s` }))]} className="w-36" /><button type="button" onClick={() => void refreshSessions()} disabled={!canRefresh || loadingSessions} className="inline-flex h-9 items-center gap-2 rounded-lg bg-ink px-3 text-[12px] font-semibold text-panel disabled:opacity-50"><RefreshCw className={loadingSessions ? 'h-4 w-4 animate-spin' : 'h-4 w-4'} />Refresh Sessions</button></div></div>
      <div className="grid gap-3 p-4 md:grid-cols-2 xl:grid-cols-4"><label className="grid gap-1.5 text-[12px] font-medium text-muted">DB Connection<DropdownSelect ariaLabel="DB Connection" value={connectionID} onChange={setConnectionID} options={[{ value: '', label: 'Select connection' }, ...connections.map((item) => ({ value: String(item.id), label: `${item.name} (${item.db_type})` }))]} /></label><div className="grid gap-1.5 text-[12px] font-medium text-muted"><span>Target Source</span><div className="flex h-10 rounded-lg border border-border bg-panel-soft p-1">{(['aws', 'manual'] as const).map((item) => <button key={item} type="button" onClick={() => setMode(item)} className={`flex-1 rounded-md text-[12px] ${mode === item ? 'bg-panel font-semibold text-ink' : 'text-muted'}`}>{item === 'aws' ? 'AWS Live' : 'Manual'}</button>)}</div></div>{mode === 'aws' ? <><label className="grid gap-1.5 text-[12px] font-medium text-muted">Cluster<DropdownSelect ariaLabel="Cluster" value={clusterKey} onChange={(value) => void selectCluster(value)} disabled={!connectionID || loadingTopology} options={[{ value: '', label: loadingTopology ? 'Loading topology...' : 'Select cluster' }, ...clusters.map((item) => ({ value: `${item.region}|${item.id}`, label: `${item.id} (${item.region})` }))]} /></label><label className="grid gap-1.5 text-[12px] font-medium text-muted">Physical Node<DropdownSelect ariaLabel="Physical Node" value={nodeID} onChange={setNodeID} disabled={!selectedCluster} options={[{ value: '', label: 'Select node' }, ...(selectedCluster?.nodes ?? []).map((item) => ({ value: item.id, label: `${item.id} · ${item.role}` }))]} /></label></> : <><label className="grid gap-1.5 text-[12px] font-medium text-muted">Host<input aria-label="Host" className={inputClass} value={manualHost} onChange={(event) => setManualHost(event.target.value)} placeholder="db.internal" /></label><label className="grid gap-1.5 text-[12px] font-medium text-muted">Port<input aria-label="Port" className={inputClass} value={manualPort} onChange={(event) => setManualPort(event.target.value)} inputMode="numeric" placeholder={selectedConnection?.db_type === 'redis' ? '6379' : selectedConnection?.db_type?.startsWith('post') ? '5432' : '3306'} /></label></>}</div>
      {selectedConnection && !selectedConnection.operations_configured ? <div className="px-4 pb-4"><InlineAlert tone="warning">Configure an operations credential for this DB connection before reading sessions.</InlineAlert></div> : null}
    </section>
    <section className="rounded-lg border border-border bg-panel">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-3"><div className="flex items-center gap-2"><ShieldCheck className="h-4 w-4 text-muted" /><h2 className="text-[14px] font-semibold text-ink">Live Sessions</h2><span className="text-[12px] text-muted">{filtered.length} shown</span></div><label className="relative block"><Search className="absolute left-3 top-2.5 h-4 w-4 text-muted" /><input aria-label="Filter sessions" className={`${inputClass} w-64 pl-9`} value={keyword} onChange={(event) => setKeyword(event.target.value)} placeholder="SQL, client, or ID" /></label></div>
      <div className="grid gap-2 border-b border-border p-3 sm:grid-cols-2 lg:grid-cols-4"><DropdownSelect ariaLabel="Filter by user" value={userFilter} onChange={setUserFilter} options={[{ value: 'all', label: 'All users' }, ...uniqueOptions(sessions.map((item) => item.user))]} /><DropdownSelect ariaLabel="Filter by database" value={databaseFilter} onChange={setDatabaseFilter} options={[{ value: 'all', label: 'All databases' }, ...uniqueOptions(sessions.map((item) => item.database))]} /><DropdownSelect ariaLabel="Filter by state" value={stateFilter} onChange={setStateFilter} options={[{ value: 'all', label: 'All states' }, ...uniqueOptions(sessions.map((item) => item.state))]} /><DropdownSelect ariaLabel="Filter by protection" value={protectionFilter} onChange={setProtectionFilter} options={[{ value: 'all', label: 'All sessions' }, { value: 'operable', label: 'Operable only' }, { value: 'protected', label: 'Protected only' }]} /></div>
      {truncated ? <div className="p-4 pb-0"><InlineAlert tone="warning">Only the first 1,000 sessions are shown. Narrow the target or filter.</InlineAlert></div> : null}
      <SessionTable items={pagedSessions} loading={loadingSessions} hasSessions={sessions.length > 0} engine={selectedConnection?.db_type} canKill={canKill} onAction={(session, action) => setPendingAction({ session, action })} />
      <div className="border-t border-border px-4 py-3"><Pagination offset={sessionOffset} pageSize={SESSION_PAGE_SIZE} count={pagedSessions.length} total={filtered.length} onChange={setSessionOffset} /></div>
    </section>
    {supportsPrefix ? <section className="rounded-lg border border-border bg-panel"><div className="border-b border-border px-4 py-3"><h2 className="text-[14px] font-semibold text-ink">SQL Prefix Actions</h2><p className="text-[12px] text-muted">Preview active sessions before a one-time cancellation or bounded loop job.</p></div><div className="grid gap-3 p-4 lg:grid-cols-[minmax(160px,0.7fr)_minmax(280px,2fr)_120px_auto]"><label className="grid gap-1.5 text-[12px] font-medium text-muted">Database<input aria-label="Prefix database" className={inputClass} value={prefixDatabase} onChange={(event) => { setPrefixDatabase(event.target.value); clearPreview() }} /></label><label className="grid gap-1.5 text-[12px] font-medium text-muted">SQL prefix<input aria-label="SQL prefix" className={inputClass} value={prefix} onChange={(event) => { setPrefix(event.target.value); clearPreview() }} placeholder="At least 16 characters" /></label><label className="grid gap-1.5 text-[12px] font-medium text-muted">Minimum age (s)<input aria-label="Minimum age" className={inputClass} type="number" min="0" value={minimumAge} onChange={(event) => { setMinimumAge(event.target.value); clearPreview() }} /></label><button type="button" onClick={() => void runPreview()} disabled={!prefixValid || prefixLoading} className="mt-auto h-10 rounded-lg bg-ink px-4 text-[12px] font-semibold text-panel disabled:opacity-50">Preview matches</button></div>{!canKill ? <div className="px-4 pb-4"><InlineAlert tone="info">Read-only access: prefix actions require db_sessions.kill.</InlineAlert></div> : null}{prefixResult ? <PrefixResultView result={prefixResult} canLoop={canLoop} onAction={setPendingPrefixAction} /> : null}</section> : null}
    <LoopJobs jobs={jobs} loading={jobsLoading} offset={jobOffset} canLoop={canLoop} onRefresh={() => void refreshJobs()} onPage={(next) => void refreshJobs(next)} onStop={(id) => void stopJob(id)} />
    <ConfirmDialog open={pendingAction !== null} title={pendingAction?.action === 'cancel' ? 'Cancel Query' : selectedConnection?.db_type === 'redis' ? 'Disconnect Redis Client' : 'Terminate Session'} description={pendingAction ? <>Revalidate and {pendingAction.action === 'cancel' ? 'cancel the query for' : 'terminate'} session <span className="font-mono font-semibold text-ink">{pendingAction.session.id}</span>? If its identity changed, the server will reject this action.</> : null} confirmLabel={pendingAction?.action === 'cancel' ? 'Cancel Query' : selectedConnection?.db_type === 'redis' ? 'Disconnect Client' : 'Terminate Session'} tone="danger" loading={actionLoading} onCancel={() => setPendingAction(null)} onConfirm={() => void runPendingAction()} />
    <ConfirmDialog open={pendingPrefixAction !== null} title={pendingPrefixAction === 'loop' ? 'Create Loop Job' : 'Cancel Matching Queries'} description={pendingPrefixAction === 'loop' ? <div className="grid gap-3"><p>This consumes the current 60-second preview and continues after leaving the page.</p><label>Interval seconds<input aria-label="Loop interval" className={inputClass} type="number" min="1" max="30" value={intervalSeconds} onChange={(event) => setIntervalSeconds(event.target.value)} /></label><label>Duration seconds<input aria-label="Loop duration" className={inputClass} type="number" min="1" max="3600" value={durationSeconds} onChange={(event) => setDurationSeconds(event.target.value)} /></label><label>Maximum cancellations<input aria-label="Loop maximum kills" className={inputClass} type="number" min="1" max="1000" value={maxKills} onChange={(event) => setMaxKills(event.target.value)} /></label></div> : <>Revalidate and cancel the {prefixResult?.items.length ?? 0} sessions in this preview? Stale or changed sessions will be skipped.</>} confirmLabel={pendingPrefixAction === 'loop' ? 'Create Job' : 'Cancel Queries'} confirmDisabled={pendingPrefixAction === 'loop' && !(Number(intervalSeconds) >= 1 && Number(intervalSeconds) <= 30 && Number(durationSeconds) >= 1 && Number(durationSeconds) <= 3600 && Number(maxKills) >= 1 && Number(maxKills) <= 1000)} tone="danger" loading={prefixLoading} onCancel={() => setPendingPrefixAction(null)} onConfirm={() => void runPrefixAction()} />
  </div>
}

function SessionTable({ items, loading, hasSessions, engine, canKill, onAction }: { items: DBSession[]; loading: boolean; hasSessions: boolean; engine?: string; canKill: boolean; onAction: (session: DBSession, action: 'cancel' | 'terminate') => void }) {
  return <div className="overflow-x-auto"><table className="min-w-[1120px] w-full text-left text-[12px]"><thead className="bg-panel-soft text-muted"><tr>{['ID', 'User', 'Database', 'Client', 'State', 'Duration', 'SQL / Command', 'Protection', 'Actions'].map((name) => <th key={name} className="px-4 py-2.5 font-medium">{name}</th>)}</tr></thead><tbody className="divide-y divide-border">{items.map((item) => <tr key={item.id}><td className="px-4 py-3 font-mono text-ink">{item.id}</td><td className="px-4 py-3 text-ink">{item.user || '—'}</td><td className="px-4 py-3 text-muted">{item.database || '—'}</td><td className="px-4 py-3 font-mono text-muted">{item.client || '—'}</td><td className="px-4 py-3 text-muted">{item.state || '—'}</td><td className="px-4 py-3 text-muted">{formatDuration(item.duration_seconds)}</td><td className="max-w-[420px] truncate px-4 py-3 font-mono text-ink" title={item.query || item.command}>{item.query || item.command || '—'}</td><td className="px-4 py-3">{item.protected ? <span className="rounded bg-warning-soft px-2 py-1 text-warning-strong">{item.protected_reason || 'Protected'}</span> : <span className="text-muted">—</span>}</td><td className="px-4 py-3"><SessionActions engine={engine} session={item} canKill={canKill} onAction={(action) => onAction(item, action)} /></td></tr>)}</tbody></table>{loading ? <LoadingBlock message="Refreshing live sessions..." /> : items.length === 0 ? <div className="px-4 py-10 text-center text-[13px] text-muted">{hasSessions ? 'No sessions match the current filters.' : 'Select a target and refresh to inspect live sessions.'}</div> : null}</div>
}

function PrefixResultView({ result, canLoop, onAction }: { result: PrefixPreview; canLoop: boolean; onAction: (action: 'cancel' | 'loop') => void }) {
  return <div className="border-t border-border p-4"><div className="flex flex-wrap items-center justify-between gap-3"><div><p className="text-[13px] font-semibold text-ink">{result.items.length} matching sessions</p><p className="text-[12px] text-muted">Preview expires {new Date(result.expires_at).toLocaleTimeString()} · {result.prefix_shape}</p></div><div className="flex gap-2"><button type="button" disabled={!result.items.length} onClick={() => onAction('cancel')} className="h-9 rounded-lg border border-danger/30 px-3 text-[12px] font-semibold text-danger disabled:opacity-50">Cancel once</button>{canLoop ? <button type="button" onClick={() => onAction('loop')} className="h-9 rounded-lg bg-ink px-3 text-[12px] font-semibold text-panel">Create loop job</button> : null}</div></div><div className="mt-3 overflow-x-auto"><table className="min-w-[720px] w-full text-left text-[12px]"><thead className="text-muted"><tr><th className="py-2">ID</th><th>User</th><th>Database</th><th>Duration</th><th>Query shape</th></tr></thead><tbody className="divide-y divide-border">{result.items.map((item) => <tr key={item.id}><td className="py-2 font-mono">{item.id}</td><td>{item.user || '—'}</td><td>{item.database || '—'}</td><td>{formatDuration(item.duration_seconds)}</td><td className="max-w-[420px] truncate font-mono" title={item.query_shape}>{item.query_shape}</td></tr>)}</tbody></table></div></div>
}

function LoopJobs({ jobs, loading, offset, canLoop, onRefresh, onPage, onStop }: { jobs: SessionLoopJob[]; loading: boolean; offset: number; canLoop: boolean; onRefresh: () => void; onPage: (offset: number) => void; onStop: (id: number) => void }) {
  return <section className="rounded-lg border border-border bg-panel"><div className="flex items-center justify-between border-b border-border px-4 py-3"><div><h2 className="text-[14px] font-semibold text-ink">Loop Jobs</h2><p className="text-[12px] text-muted">Jobs continue after leaving this page.</p></div><button aria-label="Refresh loop jobs" type="button" onClick={onRefresh} disabled={loading} className="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-border"><RefreshCw className={loading ? 'h-4 w-4 animate-spin' : 'h-4 w-4'} /></button></div><div className="overflow-x-auto"><table className="min-w-[900px] w-full text-left text-[12px]"><thead className="bg-panel-soft text-muted"><tr>{['ID', 'Engine', 'Target', 'Database', 'Prefix shape', 'Status', 'Kills', 'Limits', 'Action'].map((name) => <th key={name} className="px-4 py-2.5 font-medium">{name}</th>)}</tr></thead><tbody className="divide-y divide-border">{jobs.map((job) => <tr key={job.id}><td className="px-4 py-3 font-mono">{job.id}</td><td className="px-4 py-3">{job.engine}</td><td className="px-4 py-3 font-mono">{job.node_id || `${job.target_host}:${job.target_port}`}</td><td className="px-4 py-3">{job.database_name}</td><td className="max-w-[260px] truncate px-4 py-3 font-mono" title={job.prefix_shape}>{job.prefix_shape}</td><td className="px-4 py-3">{job.status}</td><td className="px-4 py-3">{job.kill_count}/{job.max_kills}</td><td className="px-4 py-3">{job.interval_seconds}s / {job.duration_seconds}s</td><td className="px-4 py-3">{!terminalStatuses.has(job.status) && canLoop ? <button type="button" onClick={() => onStop(job.id)} className="inline-flex h-8 items-center gap-1.5 rounded-md border border-danger/30 px-2 text-danger"><TimerOff className="h-3.5 w-3.5" />Stop</button> : <span className="text-muted">—</span>}</td></tr>)}</tbody></table>{!loading && jobs.length === 0 ? <div className="px-4 py-10 text-center text-[13px] text-muted">No loop jobs found.</div> : null}</div><div className="border-t border-border px-4 py-3"><Pagination offset={offset} pageSize={JOB_PAGE_SIZE} count={jobs.length} onChange={onPage} /></div></section>
}

function SessionActions({ engine, session, canKill, onAction }: { engine?: string; session: DBSession; canKill: boolean; onAction: (action: 'cancel' | 'terminate') => void }) {
  if (session.protected) return <span className="text-muted">Protected</span>
  if (!canKill) return <span className="text-muted">Read only</span>
  if (engine === 'redis') return <button type="button" onClick={() => onAction('terminate')} className="inline-flex h-8 items-center gap-1.5 rounded-md border border-danger/30 px-2 text-danger"><Power className="h-3.5 w-3.5" />Disconnect</button>
  return <div className="flex items-center gap-1.5"><button type="button" onClick={() => onAction('cancel')} className="inline-flex h-8 items-center gap-1.5 rounded-md border border-border px-2 text-ink"><CircleStop className="h-3.5 w-3.5" />Cancel Query</button><button type="button" onClick={() => onAction('terminate')} className="inline-flex h-8 items-center gap-1.5 rounded-md border border-danger/30 px-2 text-danger"><Power className="h-3.5 w-3.5" />Terminate</button></div>
}

function formatDuration(seconds: number) { if (seconds < 60) return `${Math.round(seconds)}s`; if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${Math.round(seconds % 60)}s`; return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m` }
function message(error: unknown, fallback: string) { return error instanceof ApiError ? error.message : fallback }
