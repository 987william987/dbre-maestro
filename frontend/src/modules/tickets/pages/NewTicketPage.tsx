import { useEffect, useMemo, useState } from 'react'
import { ArrowLeft, CheckCircle2, FileText, Loader2, ScrollText, Wand2, XCircle } from 'lucide-react'
import { Link, useNavigate } from 'react-router-dom'
import { format as formatSQL } from 'sql-formatter'
import { ApiError } from '@/shared/api/client'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { PageIntro } from '@/shared/ui/PageIntro'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { TicketReviewResult, TicketType } from '@/shared/types/ticket'
import { createTicket, listConnections, listTicketDatabases, reviewTicketSQL } from '@/modules/tickets/api'

function formatConnectionOptionLabel(connection: DBConnection) {
  return `${connection.name} · ${connection.db_type.toUpperCase()}`
}

export function NewTicketPage() {
  const navigate = useNavigate()
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [ticketType, setTicketType] = useState<TicketType>('ddl')
  const [dbConnectionId, setDbConnectionId] = useState('')
  const [databaseName, setDatabaseName] = useState('')
  const [sqlContent, setSqlContent] = useState('')
  const [reviewResults, setReviewResults] = useState<TicketReviewResult[]>([])
  const [reviewPassed, setReviewPassed] = useState(false)
  const [connections, setConnections] = useState<DBConnection[]>([])
  const [databases, setDatabases] = useState<string[]>([])
  const [loadingConnections, setLoadingConnections] = useState(true)
  const [loadingDatabases, setLoadingDatabases] = useState(false)
  const [reviewing, setReviewing] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  const selectedConnection = useMemo(
    () => connections.find((connection) => String(connection.id) === dbConnectionId) ?? null,
    [connections, dbConnectionId],
  )

  useEffect(() => {
    let active = true

    async function loadConnections() {
      setLoadingConnections(true)
      try {
        const response = await listConnections()
        if (active) {
          setConnections(response.connections)
        }
      } catch (loadError) {
        if (active) {
          setError(loadError instanceof ApiError ? loadError.message : 'Failed to load database connections.')
        }
      } finally {
        if (active) {
          setLoadingConnections(false)
        }
      }
    }

    void loadConnections()

    return () => {
      active = false
    }
  }, [])

  useEffect(() => {
    setReviewResults([])
    setReviewPassed(false)
    setError('')
  }, [dbConnectionId, databaseName, sqlContent, ticketType])

  useEffect(() => {
    if (!dbConnectionId) {
      setDatabases([])
      setDatabaseName('')
      return
    }

    let active = true

    async function loadDatabases() {
      setLoadingDatabases(true)
      try {
        const response = await listTicketDatabases(Number(dbConnectionId))
        if (!active) {
          return
        }
        const nextDatabases = response.databases
          .map((item) => item.name.trim())
          .filter((item) => item !== '')
        setDatabases(nextDatabases)

        const defaultDatabase = (selectedConnection?.database_name ?? '').trim()
        if (defaultDatabase !== '' && nextDatabases.includes(defaultDatabase)) {
          setDatabaseName(defaultDatabase)
          return
        }
        setDatabaseName('')
      } catch (loadError) {
        if (!active) {
          return
        }
        setDatabases([])
        setDatabaseName('')
        setError(loadError instanceof ApiError ? loadError.message : 'Failed to load ticket databases.')
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
  }, [dbConnectionId, selectedConnection?.database_name])

  function handleFormatSQL() {
    if (!sqlContent.trim()) {
      return
    }
    try {
      const language = selectedConnection?.db_type === 'postgres' ? 'postgresql' : selectedConnection?.db_type === 'mysql' ? 'mysql' : 'sql'
      const formatted = formatSQL(sqlContent, {
        language,
        keywordCase: 'upper',
      }).trimEnd()
      setSqlContent(formatted)
    } catch (formatError) {
      setError(formatError instanceof Error ? formatError.message : 'Failed to format SQL.')
    }
  }

  async function handleReviewSQL() {
    if (sqlContent.trim() === '' || dbConnectionId === '' || databaseName.trim() === '') {
      return
    }
    setReviewing(true)
    setError('')
    try {
      const response = await reviewTicketSQL({
        sql_content: sqlContent,
        ticket_type: ticketType,
        db_connection_id: Number(dbConnectionId),
        database_name: databaseName.trim(),
      })
      setReviewResults(response.results)
      setReviewPassed(response.passed)
      if (!response.passed) {
        setError('SQL review did not pass. Fix the issues before submitting.')
      }
    } catch (reviewError) {
      setReviewResults([])
      setReviewPassed(false)
      setError(reviewError instanceof ApiError ? reviewError.message : 'Failed to review SQL.')
    } finally {
      setReviewing(false)
    }
  }

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (title.trim() === '' || sqlContent.trim() === '' || dbConnectionId === '' || databaseName.trim() === '' || !reviewPassed) {
      return
    }
    setError('')
    setSubmitting(true)

    try {
      const created = await createTicket({
        title,
        description: description.trim() || null,
        sql_content: sqlContent,
        ticket_type: ticketType,
        db_connection_id: dbConnectionId ? Number(dbConnectionId) : null,
        database_name: databaseName.trim(),
      })
      navigate(`/tickets/${created.id}`, { replace: true })
    } catch (submitError) {
      setError(submitError instanceof ApiError ? submitError.message : 'Failed to create ticket. Please try again.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
      <PageIntro
        title="New Ticket"
        description="Fill in the change details and target database. After submission, a Reviewer / DBA will handle the review and execution."
        actions={
          <Link
            to="/tickets"
            className="inline-flex h-10 shrink-0 items-center gap-2 rounded-lg border border-border bg-white px-4 text-[13px] font-semibold text-ink transition hover:bg-panel-soft"
          >
            <ArrowLeft className="h-4 w-4" />
            Back to List
          </Link>
        }
      />

      <form className="grid items-start gap-3 xl:grid-cols-[0.95fr_1.05fr]" onSubmit={handleSubmit}>
        <section className="rounded-xl border border-border bg-panel shadow-soft">
          <div className="border-b border-border/80 px-4 py-3">
            <div className="flex items-center gap-2">
              <FileText className="h-4 w-4 text-muted" />
              <p className="text-[13px] font-semibold text-ink">Ticket Info</p>
            </div>
          </div>

          <div className="grid gap-4 px-4 py-4">
            <label className="flex flex-col gap-1.5">
              <span className="text-[12px] font-semibold text-ink">
                Title <span className="text-danger">*</span>
              </span>
              <input
                value={title}
                onChange={(event) => setTitle(event.target.value)}
                className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                placeholder="e.g. Add index, backfill order status"
                disabled={submitting}
              />
            </label>

            <label className="flex flex-col gap-1.5">
              <span className="text-[12px] font-semibold text-ink">Description</span>
              <textarea
                value={description}
                onChange={(event) => setDescription(event.target.value)}
                className="min-h-28 rounded-lg border border-border bg-panel-soft px-3 py-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                placeholder="Add context, affected scope, rollback plan, and execution considerations."
                disabled={submitting}
              />
            </label>

            <div className="grid gap-4 sm:grid-cols-2">
              <label className="flex flex-col gap-1.5">
                <span className="text-[12px] font-semibold text-ink">Ticket Type</span>
                <DropdownSelect
                  ariaLabel="Ticket Type"
                  value={ticketType}
                  onChange={(value) => setTicketType(value as TicketType)}
                  disabled={submitting}
                  options={[
                    { value: 'ddl', label: 'DDL' },
                    { value: 'dml', label: 'DML' },
                  ]}
                />
              </label>

              <label className="flex flex-col gap-1.5">
                <span className="text-[12px] font-semibold text-ink">
                  Target Instance <span className="text-danger">*</span>
                </span>
                <DropdownSelect
                  ariaLabel="Target Instance"
                  value={dbConnectionId}
                  onChange={setDbConnectionId}
                  disabled={submitting || loadingConnections}
                  options={[
                    { value: '', label: 'Not Selected' },
                    ...connections.map((connection) => ({
                      value: String(connection.id),
                      label: formatConnectionOptionLabel(connection),
                    })),
                  ]}
                />
              </label>
            </div>

            <label className="flex flex-col gap-1.5">
              <span className="text-[12px] font-semibold text-ink">
                Target Database <span className="text-danger">*</span>
              </span>
              <DropdownSelect
                ariaLabel="Target Database"
                value={databaseName}
                onChange={setDatabaseName}
                disabled={submitting || reviewing || loadingDatabases || dbConnectionId === '' || databases.length === 0}
                placeholder={loadingDatabases ? 'Loading databases...' : dbConnectionId === '' ? 'Select instance first' : 'Select database'}
                options={[
                  { value: '', label: 'Not Selected' },
                  ...databases.map((name) => ({ value: name, label: name })),
                ]}
              />
            </label>
          </div>
        </section>

        <section className="flex flex-col rounded-xl border border-border bg-panel shadow-soft">
          <div className="border-b border-border/80 px-4 py-3">
            <div className="flex items-center gap-2">
              <ScrollText className="h-4 w-4 text-muted" />
              <p className="text-[13px] font-semibold text-ink">
                SQL Content <span className="text-danger">*</span>
              </p>
            </div>
          </div>

          <label className="flex flex-col gap-1.5 px-4 py-4">
            <span className="sr-only">SQL Content</span>
            <textarea
              value={sqlContent}
              onChange={(event) => setSqlContent(event.target.value)}
              className="block min-h-[360px] w-full resize-y rounded-xl border border-border bg-panel-soft px-4 py-4 font-mono text-[13px] leading-7 text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20 lg:min-h-[420px]"
              placeholder={'ALTER TABLE ...;\nUPDATE ...;'}
              disabled={submitting}
            />
          </label>

          <div className="border-t border-border/80 px-4 py-3">
            <div className="flex flex-wrap items-center justify-end gap-2">
              <button
                type="button"
                onClick={handleFormatSQL}
                disabled={submitting || reviewing || sqlContent.trim() === ''}
                className="inline-flex h-10 items-center justify-center gap-2 rounded-lg border border-border bg-white px-4 text-[13px] font-semibold text-ink transition hover:bg-panel-soft disabled:cursor-not-allowed disabled:opacity-50"
              >
                <Wand2 className="h-4 w-4" />
                Format
              </button>
              <button
                type="button"
                onClick={() => void handleReviewSQL()}
                disabled={submitting || reviewing || sqlContent.trim() === '' || dbConnectionId === '' || databaseName.trim() === ''}
                className="inline-flex h-10 items-center justify-center gap-2 rounded-lg border border-border bg-white px-4 text-[13px] font-semibold text-ink transition hover:bg-panel-soft disabled:cursor-not-allowed disabled:opacity-50"
              >
                {reviewing ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
                {reviewing ? 'Reviewing...' : 'SQL Review'}
              </button>
            </div>
          </div>
        </section>

        <div className="xl:col-span-2">
          {error ? (
            <div className="mb-4 rounded-lg border border-danger/20 bg-red-50 px-4 py-3 text-[13px] text-danger">
              {error}
            </div>
          ) : null}

          {reviewResults.length > 0 ? (
            <section className="mb-4 overflow-hidden rounded-xl border border-border bg-panel shadow-soft">
              <div className="border-b border-border/80 px-4 py-3">
                <div className="flex items-center justify-between gap-3">
                  <p className="text-[13px] font-semibold text-ink">SQL Review Results</p>
                  <span className={`inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-[11px] font-semibold ${
                    reviewPassed ? 'bg-emerald-50 text-emerald-700' : 'bg-red-50 text-danger'
                  }`}>
                    {reviewPassed ? <CheckCircle2 className="h-3.5 w-3.5" /> : <XCircle className="h-3.5 w-3.5" />}
                    {reviewPassed ? 'Passed' : 'Failed'}
                  </span>
                </div>
              </div>
              <div className="overflow-x-auto">
                <table className="min-w-full border-collapse">
                  <thead className="bg-panel-soft text-left text-[11px] font-semibold text-faint">
                    <tr>
                      <th className="px-4 py-3">ID</th>
                      <th className="px-4 py-3">SQL</th>
                      <th className="px-4 py-3">Scan / Impact Rows</th>
                      <th className="px-4 py-3">Review Status</th>
                      <th className="px-4 py-3">Review Message</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border text-[13px] text-ink">
                    {reviewResults.map((result) => (
                      <tr key={`${result.seq}-${result.sql_stmt}`}>
                        <td className="px-4 py-3 align-top">{result.seq}</td>
                        <td className="px-4 py-3 align-top font-mono text-[12px]">{result.sql_stmt}</td>
                        <td className="px-4 py-3 align-top">{result.scan_rows}</td>
                        <td className="px-4 py-3 align-top">
                          <span className={`inline-flex rounded-full px-2 py-1 text-[11px] font-semibold ${
                            result.status === 'pass' ? 'bg-emerald-50 text-emerald-700' : 'bg-red-50 text-danger'
                          }`}>
                            {result.status}
                          </span>
                        </td>
                        <td className="px-4 py-3 align-top text-muted">{result.message || '—'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </section>
          ) : null}

          <div className="flex flex-wrap items-center justify-end gap-2.5 rounded-xl border border-border bg-panel px-4 py-3 shadow-soft">
            <Link
              to="/tickets"
              className="inline-flex h-10 items-center justify-center rounded-lg border border-border bg-white px-4 text-[13px] font-semibold text-ink transition hover:bg-panel-soft"
            >
              Cancel
            </Link>
            <button
              type="submit"
              disabled={submitting || reviewing || title.trim() === '' || sqlContent.trim() === '' || dbConnectionId === '' || databaseName.trim() === '' || !reviewPassed}
              className="inline-flex h-10 items-center justify-center gap-2 rounded-lg bg-brand px-5 text-[13px] font-bold text-white shadow-soft transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
              {submitting ? 'Submitting...' : 'Submit Ticket'}
            </button>
          </div>
        </div>
      </form>
    </div>
  )
}
