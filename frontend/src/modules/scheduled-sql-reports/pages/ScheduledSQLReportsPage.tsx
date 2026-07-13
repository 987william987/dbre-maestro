import { useEffect, useMemo, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { CalendarClock, Check, Pencil, Plus, Trash2, X } from 'lucide-react'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { StatusBadge } from '@/shared/ui/StatusBadge'
import {
  createScheduledSQLReport,
  deleteScheduledSQLReport,
  getScheduledSQLReport,
  listScheduledReportConnections,
  listScheduledReportRecipients,
  listScheduledReportMetadata,
  listScheduledSQLReports,
  updateScheduledSQLReport,
  type ScheduledReportRecipient,
  type ScheduledSQLReport,
  type ScheduledSQLReportRun,
} from '@/modules/scheduled-sql-reports/api'
import type { DBConnection } from '@/shared/types/dbConnection'

type ReportDraft = {
  name: string
  description: string
  dbConnectionID: string
  databaseName: string
  schemaName: string
  sqlContent: string
  cronExpression: string
  timezone: string
  recipientUserIDs: number[]
  isActive: boolean
}

const EMPTY_DRAFT: ReportDraft = {
  name: '',
  description: '',
  dbConnectionID: '',
  databaseName: '',
  schemaName: '',
  sqlContent: '',
  cronExpression: '0 9 * * *',
  timezone: 'Asia/Taipei',
  recipientUserIDs: [],
  isActive: true,
}

export function ScheduledSQLReportsPage() {
  const [reports, setReports] = useState<ScheduledSQLReport[]>([])
  const [connections, setConnections] = useState<DBConnection[]>([])
  const [recipients, setRecipients] = useState<ScheduledReportRecipient[]>([])
  const [selectedReportID, setSelectedReportID] = useState<number | null>(null)
  const [runs, setRuns] = useState<ScheduledSQLReportRun[]>([])
  const [draft, setDraft] = useState<ReportDraft>(EMPTY_DRAFT)
  const [databaseOptions, setDatabaseOptions] = useState<string[]>([])
  const [schemaOptions, setSchemaOptions] = useState<string[]>([])
  const [loadingDatabases, setLoadingDatabases] = useState(false)
  const [loadingSchemas, setLoadingSchemas] = useState(false)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  const selectedReport = useMemo(
    () => reports.find((report) => report.id === selectedReportID) ?? null,
    [reports, selectedReportID],
  )
  const selectedConnection = useMemo(
    () => connections.find((connection) => String(connection.id) === draft.dbConnectionID) ?? null,
    [connections, draft.dbConnectionID],
  )
  const isPostgresConnection = selectedConnection?.db_type === 'postgres' || selectedConnection?.db_type === 'postgresql'
  const connectionOptions = useMemo(
    () => [
      { value: '', label: 'Select connection' },
      ...connections.map((connection) => ({ value: String(connection.id), label: `${connection.name} (${connection.db_type})` })),
    ],
    [connections],
  )
  const databaseSelectOptions = useMemo(
    () => [
      { value: '', label: loadingDatabases ? 'Loading databases...' : 'Select database' },
      ...uniqueOptions(databaseOptions, draft.databaseName),
    ],
    [databaseOptions, draft.databaseName, loadingDatabases],
  )
  const schemaSelectOptions = useMemo(
    () => [
      { value: '', label: loadingSchemas ? 'Loading schemas...' : isPostgresConnection ? 'Select schema' : 'Not required for MySQL' },
      ...uniqueOptions(schemaOptions, draft.schemaName),
    ],
    [draft.schemaName, isPostgresConnection, loadingSchemas, schemaOptions],
  )
  const recipientOptions = useMemo(
    () => recipients.filter((recipient) => recipient.lark_recipient.trim() !== ''),
    [recipients],
  )

  async function loadAll() {
    setLoading(true)
    setError('')
    try {
      const [nextReports, nextConnections, nextRecipients] = await Promise.all([
        listScheduledSQLReports(),
        listScheduledReportConnections(),
        listScheduledReportRecipients(),
      ])
      setReports(nextReports)
      setConnections(nextConnections)
      setRecipients(nextRecipients)
    } catch (loadError) {
      setError(loadError instanceof ApiError ? loadError.message : 'Failed to load scheduled SQL reports.')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void loadAll()
  }, [])

  useEffect(() => {
    if (draft.dbConnectionID === '') {
      setDatabaseOptions([])
      setSchemaOptions([])
      return
    }

    let active = true
    async function loadDatabases() {
      setLoadingDatabases(true)
      try {
        const response = await listScheduledReportMetadata(Number(draft.dbConnectionID))
        if (!active) {
          return
        }
        const nextDatabases = response.items
          .filter((item) => item.kind === 'database')
          .map((item) => item.name.trim())
          .filter((item) => item !== '')
        setDatabaseOptions(nextDatabases)
      } catch (loadError) {
        if (!active) {
          return
        }
        setDatabaseOptions([])
        setSchemaOptions([])
        setError(loadError instanceof ApiError ? loadError.message : 'Failed to load databases.')
      } finally {
        if (active) {
          setLoadingDatabases(false)
        }
      }
    }

    void loadDatabases()
    return () => {
      active = false
    }
  }, [draft.dbConnectionID])

  useEffect(() => {
    if (!isPostgresConnection || draft.dbConnectionID === '' || draft.databaseName === '') {
      setSchemaOptions([])
      return
    }

    let active = true
    async function loadSchemas() {
      setLoadingSchemas(true)
      try {
        const response = await listScheduledReportMetadata(Number(draft.dbConnectionID), { database: draft.databaseName })
        if (!active) {
          return
        }
        const nextSchemas = response.items
          .filter((item) => item.kind === 'schema')
          .map((item) => item.name.trim())
          .filter((item) => item !== '')
        setSchemaOptions(nextSchemas)
      } catch (loadError) {
        if (!active) {
          return
        }
        setSchemaOptions([])
        setError(loadError instanceof ApiError ? loadError.message : 'Failed to load schemas.')
      } finally {
        if (active) {
          setLoadingSchemas(false)
        }
      }
    }

    void loadSchemas()
    return () => {
      active = false
    }
  }, [draft.databaseName, draft.dbConnectionID, isPostgresConnection])

  async function selectReport(report: ScheduledSQLReport) {
    setSelectedReportID(report.id)
    setDraft(reportToDraft(report))
    setNotice('')
    setError('')
    try {
      const detail = await getScheduledSQLReport(report.id)
      setRuns(detail.runs)
    } catch (loadError) {
      setError(loadError instanceof ApiError ? loadError.message : 'Failed to load run history.')
    }
  }

  function startCreate() {
    setSelectedReportID(null)
    setRuns([])
    setDraft(EMPTY_DRAFT)
    setNotice('')
    setError('')
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSaving(true)
    setError('')
    setNotice('')
    try {
      const payload = {
        name: draft.name,
        description: draft.description,
        db_connection_id: Number(draft.dbConnectionID),
        database_name: draft.databaseName,
        schema_name: draft.schemaName,
        sql_content: draft.sqlContent,
        cron_expression: draft.cronExpression,
        timezone: draft.timezone,
        recipient_user_ids: draft.recipientUserIDs,
        is_active: draft.isActive,
      }
      const saved = selectedReportID == null
        ? await createScheduledSQLReport(payload)
        : await updateScheduledSQLReport(selectedReportID, payload)
      await loadAll()
      setSelectedReportID(saved.id)
      setDraft(reportToDraft(saved))
      setNotice('Scheduled SQL report saved.')
    } catch (saveError) {
      setError(saveError instanceof ApiError ? saveError.message : 'Failed to save scheduled SQL report.')
    } finally {
      setSaving(false)
    }
  }

  async function handleDelete(report: ScheduledSQLReport) {
    if (!window.confirm(`Delete scheduled SQL report "${report.name}"?`)) {
      return
    }
    setError('')
    setNotice('')
    try {
      await deleteScheduledSQLReport(report.id)
      if (selectedReportID === report.id) {
        startCreate()
      }
      await loadAll()
      setNotice('Scheduled SQL report deleted.')
    } catch (deleteError) {
      setError(deleteError instanceof ApiError ? deleteError.message : 'Failed to delete scheduled SQL report.')
    }
  }

  function toggleRecipient(userID: number) {
    setDraft((current) => ({
      ...current,
      recipientUserIDs: current.recipientUserIDs.includes(userID)
        ? current.recipientUserIDs.filter((item) => item !== userID)
        : [...current.recipientUserIDs, userID],
    }))
  }

  if (loading) {
    return <div className="p-4"><LoadingBlock message="Loading scheduled SQL reports..." className="min-h-[420px]" /></div>
  }

  return (
    <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
      {error ? <InlineAlert tone="error">{error}</InlineAlert> : null}
      {notice ? <InlineAlert tone="success">{notice}</InlineAlert> : null}

      <div className="grid gap-3 xl:grid-cols-[minmax(0,1fr)_420px]">
        <form onSubmit={handleSubmit} className="rounded-lg border border-border bg-panel p-4 shadow-soft">
          <div className="mb-4 flex items-center justify-between gap-3">
            <div>
              <h3 className="text-[16px] font-semibold text-ink">{selectedReport ? 'Edit Report' : 'Create Report'}</h3>
              <p className="mt-1 text-[12px] text-muted">Only SELECT, WITH, and SHOW are accepted. Sensitive columns are rejected during save.</p>
            </div>
            <div className="flex shrink-0 items-center gap-3">
              {selectedReport ? (
                <button type="button" onClick={startCreate} className="inline-flex h-9 items-center gap-2 rounded-lg bg-brand px-3 text-[12px] font-bold text-white shadow-soft hover:bg-slate-800">
                  <Plus className="h-4 w-4" />
                  New Report
                </button>
              ) : null}
              <label className="inline-flex items-center gap-2 text-[12px] font-semibold text-ink">
                <input type="checkbox" checked={draft.isActive} onChange={(event) => setDraft((current) => ({ ...current, isActive: event.target.checked }))} />
                Active
              </label>
            </div>
          </div>

          <div className="grid gap-3 lg:grid-cols-2">
            <Field label="Name">
              <input value={draft.name} onChange={(event) => setDraft((current) => ({ ...current, name: event.target.value }))} className={inputClassName} />
            </Field>
            <Field label="DB Connection">
              <DropdownSelect
                value={draft.dbConnectionID}
                onChange={(value) => setDraft((current) => ({
                  ...current,
                  dbConnectionID: value,
                  databaseName: '',
                  schemaName: '',
                }))}
                options={connectionOptions}
                ariaLabel="DB Connection"
              />
            </Field>
            <Field label="Database">
              <DropdownSelect
                value={draft.databaseName}
                onChange={(value) => setDraft((current) => ({
                  ...current,
                  databaseName: value,
                  schemaName: '',
                }))}
                options={databaseSelectOptions}
                ariaLabel="Database"
                disabled={draft.dbConnectionID === '' || loadingDatabases}
              />
            </Field>
            <Field label="Schema">
              <DropdownSelect
                value={draft.schemaName}
                onChange={(value) => setDraft((current) => ({ ...current, schemaName: value }))}
                options={schemaSelectOptions}
                ariaLabel="Schema"
                disabled={!isPostgresConnection || draft.databaseName === '' || loadingSchemas}
              />
            </Field>
            <Field label="Cron">
              <input value={draft.cronExpression} onChange={(event) => setDraft((current) => ({ ...current, cronExpression: event.target.value }))} className={inputClassName} placeholder="0 9 * * *" />
            </Field>
            <Field label="Timezone">
              <input value={draft.timezone} onChange={(event) => setDraft((current) => ({ ...current, timezone: event.target.value }))} className={inputClassName} placeholder="Asia/Taipei" />
            </Field>
          </div>

          <Field label="SQL" className="mt-3">
            <textarea value={draft.sqlContent} onChange={(event) => setDraft((current) => ({ ...current, sqlContent: event.target.value }))} className={`${inputClassName} min-h-[180px] py-3 font-mono`} />
          </Field>

          <Field label="Lark Recipients" className="mt-3">
            <div className="grid max-h-[168px] gap-2 overflow-auto rounded-lg border border-border bg-panel-soft p-2 sm:grid-cols-2">
              {recipientOptions.map((recipient) => {
                const selected = draft.recipientUserIDs.includes(recipient.id)
                return (
                  <button key={recipient.id} type="button" onClick={() => toggleRecipient(recipient.id)} className={`flex items-center justify-between rounded-md px-3 py-2 text-left text-[12px] transition ${selected ? 'bg-white text-ink shadow-soft' : 'text-muted hover:bg-white/70'}`}>
                    <span className="min-w-0">
                      <span className="block truncate font-semibold">{recipient.username}</span>
                      <span className="block truncate">{recipient.email}</span>
                    </span>
                    {selected ? <Check className="h-4 w-4 shrink-0 text-brand" /> : null}
                  </button>
                )
              })}
              {recipientOptions.length === 0 ? <p className="px-2 py-4 text-[12px] text-muted">No users have Lark Open ID configured.</p> : null}
            </div>
          </Field>

          <Field label="Description" className="mt-3">
            <textarea value={draft.description} onChange={(event) => setDraft((current) => ({ ...current, description: event.target.value }))} className={`${inputClassName} min-h-[76px] py-3`} />
          </Field>

          <div className="mt-4 flex items-center justify-end gap-2">
            {selectedReport ? (
              <button type="button" onClick={startCreate} className="inline-flex h-10 items-center gap-2 rounded-lg border border-border bg-white px-4 text-[13px] font-bold text-ink shadow-soft hover:border-slate-300">
                <X className="h-4 w-4" />
                Cancel
              </button>
            ) : null}
            <button type="submit" disabled={saving} className="inline-flex h-10 items-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white shadow-soft disabled:opacity-60">
              <CalendarClock className="h-4 w-4" />
              {saving ? 'Saving...' : 'Save Report'}
            </button>
          </div>
        </form>

        <aside className="grid gap-3">
          <section className="rounded-lg border border-border bg-panel p-3 shadow-soft">
            <h3 className="mb-2 text-[14px] font-semibold text-ink">Reports</h3>
            <div className="grid max-h-[360px] gap-2 overflow-auto">
              {reports.map((report) => (
                <button key={report.id} type="button" onClick={() => void selectReport(report)} className={`rounded-lg border px-3 py-2 text-left transition ${selectedReportID === report.id ? 'border-brand bg-panel-soft' : 'border-border bg-white hover:border-slate-300'}`}>
                  <div className="flex items-start justify-between gap-2">
                    <span className="min-w-0 truncate text-[13px] font-semibold text-ink">{report.name}</span>
                    <StatusBadge status={report.is_active ? 'completed' : 'stopped'} />
                  </div>
                  <p className="mt-1 truncate text-[12px] text-muted">{report.cron_expression} / {report.timezone}</p>
                  <p className="mt-1 text-[12px] text-muted">Next: {report.next_run_at ? formatDateTime(report.next_run_at) : '-'}</p>
                  <div className="mt-2 flex gap-2">
                    <span className="inline-flex items-center gap-1 text-[12px] font-semibold text-muted"><Pencil className="h-3.5 w-3.5" /> Edit</span>
                    <span onClick={(event) => { event.stopPropagation(); void handleDelete(report) }} className="inline-flex items-center gap-1 text-[12px] font-semibold text-danger"><Trash2 className="h-3.5 w-3.5" /> Delete</span>
                  </div>
                </button>
              ))}
              {reports.length === 0 ? <p className="px-2 py-8 text-center text-[12px] text-muted">No scheduled SQL reports yet.</p> : null}
            </div>
          </section>

          <section className="rounded-lg border border-border bg-panel p-3 shadow-soft">
            <h3 className="mb-2 text-[14px] font-semibold text-ink">Run History</h3>
            <div className="grid max-h-[300px] gap-2 overflow-auto">
              {runs.map((run) => (
                <div key={run.id} className="rounded-lg border border-border bg-white p-3">
                  <div className="flex items-center justify-between gap-2">
                    <StatusBadge status={run.status === 'success' ? 'completed' : run.status === 'running' ? 'executing' : 'failed'} />
                    <span className="text-[12px] text-muted">{formatDateTime(run.started_at)}</span>
                  </div>
                  <p className="mt-2 text-[12px] text-muted">Rows: {run.row_count}</p>
                  {run.file_name ? <p className="mt-1 truncate text-[12px] text-muted">{run.file_name}</p> : null}
                  {run.error_message ? <p className="mt-1 text-[12px] text-danger">{run.error_message}</p> : null}
                </div>
              ))}
              {selectedReportID == null ? <p className="px-2 py-8 text-center text-[12px] text-muted">Select a report to view runs.</p> : null}
              {selectedReportID != null && runs.length === 0 ? <p className="px-2 py-8 text-center text-[12px] text-muted">No runs yet.</p> : null}
            </div>
          </section>
        </aside>
      </div>
    </div>
  )
}

function reportToDraft(report: ScheduledSQLReport): ReportDraft {
  return {
    name: report.name,
    description: report.description ?? '',
    dbConnectionID: String(report.db_connection_id),
    databaseName: report.database_name ?? '',
    schemaName: report.schema_name ?? '',
    sqlContent: report.sql_content,
    cronExpression: report.cron_expression,
    timezone: report.timezone,
    recipientUserIDs: report.recipient_user_ids ?? [],
    isActive: report.is_active,
  }
}

function Field({ label, children, className = '' }: { label: string; children: ReactNode; className?: string }) {
  return (
    <label className={`block ${className}`}>
      <span className="mb-1.5 block text-[12px] font-semibold text-muted">{label}</span>
      {children}
    </label>
  )
}

const inputClassName = 'h-10 w-full rounded-lg border border-border bg-white px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20'

function uniqueOptions(options: string[], currentValue: string) {
  const seen = new Set<string>()
  const values = [...options]
  if (currentValue.trim() !== '') {
    values.unshift(currentValue.trim())
  }
  return values
    .filter((value) => {
      const key = value.toLowerCase()
      if (seen.has(key)) {
        return false
      }
      seen.add(key)
      return true
    })
    .map((value) => ({ value, label: value }))
}
