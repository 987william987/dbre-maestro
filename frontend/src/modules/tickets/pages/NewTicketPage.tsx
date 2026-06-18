import { useEffect, useMemo, useState } from 'react'
import { ArrowLeft, CheckCircle2, FileText, Loader2, ScrollText, Wand2, XCircle } from 'lucide-react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { format as formatSQL } from 'sql-formatter'
import { ApiError } from '@/shared/api/client'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { PageIntro } from '@/shared/ui/PageIntro'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { MetadataResponse } from '@/shared/types/sqlEditor'
import type { QueryAccessScopeMode, TicketReviewResult, TicketType } from '@/shared/types/ticket'
import { listMetadata } from '@/modules/sql-editor/api'
import { createTicket, listConnections, listTicketDatabases, reviewTicketSQL } from '@/modules/tickets/api'

function formatConnectionOptionLabel(connection: DBConnection) {
  return `${connection.name} · ${connection.db_type.toUpperCase()}`
}

type QueryAccessTableOption = {
  key: string
  label: string
  databaseName: string
  tableName: string
}

const MAX_QUERY_ACCESS_DURATION_MINUTES = 3 * 24 * 60

function parseTicketType(value: string | null): TicketType | null {
  switch (value) {
    case 'ddl':
    case 'dml':
    case 'redis_command':
    case 'sql_export':
    case 'sensitive_query_access':
    case 'query_access':
      return value
    default:
      return null
  }
}

function normalizeTableOptions(response: MetadataResponse, databaseName: string) {
  return response.items
    .filter((item) => item.kind === 'table')
    .map((item) => {
      const schemaLabel = item.schema && item.schema !== databaseName ? `${item.schema}.` : ''
      return {
        key: `${item.schema ?? ''}:${item.name}`,
        label: `${schemaLabel}${item.name}`,
        databaseName,
        tableName: item.name,
      } satisfies QueryAccessTableOption
    })
}

function parseQueryAccessDuration(rawValue: string) {
  const value = rawValue.trim()
  if (!/^\d+$/.test(value)) {
    return { error: 'Approved duration must be a whole number of minutes.' }
  }

  const minutes = Number(value)
  if (minutes < 1 || minutes > MAX_QUERY_ACCESS_DURATION_MINUTES) {
    return { error: `Approved duration must be between 1 and ${MAX_QUERY_ACCESS_DURATION_MINUTES} minutes.` }
  }

  return { minutes }
}

export function NewTicketPage() {
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const prefilledTicketType = parseTicketType(searchParams.get('ticket_type'))
  const prefilledConnectionId = searchParams.get('db_connection_id')?.trim() ?? ''
  const prefilledDatabaseName = searchParams.get('database_name')?.trim() ?? ''
  const prefilledTableName = searchParams.get('table_name')?.trim() ?? ''
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [ticketType, setTicketType] = useState<TicketType>(prefilledTicketType ?? 'ddl')
  const [dbConnectionId, setDbConnectionId] = useState(prefilledConnectionId)
  const [databaseName, setDatabaseName] = useState('')
  const [sqlContent, setSqlContent] = useState('')
  const [reviewResults, setReviewResults] = useState<TicketReviewResult[]>([])
  const [reviewPassed, setReviewPassed] = useState(false)
  const [connections, setConnections] = useState<DBConnection[]>([])
  const [databases, setDatabases] = useState<string[]>([])
  const [queryAccessScopeMode, setQueryAccessScopeMode] = useState<QueryAccessScopeMode>(prefilledTableName ? 'table' : 'database')
  const [queryAccessDuration, setQueryAccessDuration] = useState('60')
  const [queryAccessDatabaseSelections, setQueryAccessDatabaseSelections] = useState<string[]>(
    prefilledDatabaseName && !prefilledTableName ? [prefilledDatabaseName] : [],
  )
  const [queryAccessTableDatabase, setQueryAccessTableDatabase] = useState(prefilledDatabaseName)
  const [queryAccessTables, setQueryAccessTables] = useState<QueryAccessTableOption[]>([])
  const [queryAccessTableSelections, setQueryAccessTableSelections] = useState<string[]>([])
  const [queryAccessTablePrefillApplied, setQueryAccessTablePrefillApplied] = useState(false)
  const [loadingConnections, setLoadingConnections] = useState(true)
  const [loadingDatabases, setLoadingDatabases] = useState(false)
  const [loadingQueryAccessTables, setLoadingQueryAccessTables] = useState(false)
  const [reviewing, setReviewing] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  const isQueryAccessTicket = ticketType === 'query_access'
  const selectedConnection = useMemo(
    () => connections.find((connection) => String(connection.id) === dbConnectionId) ?? null,
    [connections, dbConnectionId],
  )
  const filteredConnections = useMemo(() => {
    if (ticketType === 'redis_command') {
      return connections.filter((connection) => connection.db_type === 'redis')
    }
    return connections.filter((connection) => connection.db_type !== 'redis')
  }, [connections, ticketType])
  const parserResults = useMemo(() => reviewResults.filter((result) => result.phase === 'parser'), [reviewResults])
  const validationResults = useMemo(() => reviewResults.filter((result) => !result.phase || result.phase === 'validation'), [reviewResults])
  const selectedQueryAccessItems = useMemo(
    () => queryAccessTables.filter((item) => queryAccessTableSelections.includes(item.key)),
    [queryAccessTableSelections, queryAccessTables],
  )
  const requiresDatabaseSelection = !isQueryAccessTicket
  const hasValidQueryAccessScope = isQueryAccessTicket && (
    (queryAccessScopeMode === 'database' && queryAccessDatabaseSelections.length > 0) ||
    (queryAccessScopeMode === 'table' && queryAccessTableDatabase.trim() !== '' && selectedQueryAccessItems.length > 0)
  )
  const canSubmit = isQueryAccessTicket
    ? title.trim() !== '' && dbConnectionId !== '' && hasValidQueryAccessScope
    : title.trim() !== '' &&
      sqlContent.trim() !== '' &&
      dbConnectionId !== '' &&
      (!requiresDatabaseSelection || databaseName.trim() !== '') &&
      reviewPassed

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
    if (selectedConnection && ticketType === 'redis_command' && selectedConnection.db_type !== 'redis') {
      setDbConnectionId('')
      setDatabaseName('')
    }
    if (selectedConnection && ticketType !== 'redis_command' && selectedConnection.db_type === 'redis') {
      setDbConnectionId('')
      setDatabaseName('')
    }
  }, [selectedConnection, ticketType])

  useEffect(() => {
    if (!isQueryAccessTicket) {
      return
    }
    setQueryAccessDatabaseSelections([])
    setQueryAccessTableSelections([])
    setQueryAccessTables([])
    setQueryAccessTablePrefillApplied(false)
    if (prefilledConnectionId !== dbConnectionId) {
      setQueryAccessTableDatabase('')
    }
  }, [dbConnectionId, isQueryAccessTicket, prefilledConnectionId])

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

        if (isQueryAccessTicket) {
          if (queryAccessScopeMode === 'database' && queryAccessDatabaseSelections.length === 0 && prefilledDatabaseName && nextDatabases.includes(prefilledDatabaseName)) {
            setQueryAccessDatabaseSelections([prefilledDatabaseName])
          }
          if (queryAccessScopeMode === 'table' && queryAccessTableDatabase.trim() === '' && prefilledDatabaseName && nextDatabases.includes(prefilledDatabaseName)) {
            setQueryAccessTableDatabase(prefilledDatabaseName)
          }
        }

        if (!requiresDatabaseSelection) {
          return
        }
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
  }, [
    dbConnectionId,
    isQueryAccessTicket,
    prefilledDatabaseName,
    queryAccessScopeMode,
    requiresDatabaseSelection,
    selectedConnection?.database_name,
  ])

  useEffect(() => {
    if (!isQueryAccessTicket || queryAccessScopeMode !== 'table' || dbConnectionId === '' || queryAccessTableDatabase.trim() === '') {
      setQueryAccessTables([])
      return
    }

    let active = true

    async function loadQueryAccessTables() {
      setLoadingQueryAccessTables(true)
      try {
        const response = await listMetadata(Number(dbConnectionId), { database: queryAccessTableDatabase.trim() })
        if (!active) {
          return
        }

        let nextTables = normalizeTableOptions(response, queryAccessTableDatabase.trim())
        if (response.level === 'schema') {
          const schemaResponses = await Promise.all(
            response.items
              .filter((item) => item.kind === 'schema')
              .map((item) => listMetadata(Number(dbConnectionId), {
                database: queryAccessTableDatabase.trim(),
                schema: item.name,
              })),
          )
          if (!active) {
            return
          }
          nextTables = schemaResponses.flatMap((item) => normalizeTableOptions(item, queryAccessTableDatabase.trim()))
        }

        setQueryAccessTables(nextTables)
        if (prefilledTableName && !queryAccessTablePrefillApplied) {
          const matched = nextTables.find((item) => item.tableName === prefilledTableName)
          if (matched) {
            setQueryAccessTableSelections([matched.key])
            setQueryAccessTablePrefillApplied(true)
          }
        }
      } catch (loadError) {
        if (!active) {
          return
        }
        setQueryAccessTables([])
        setError(loadError instanceof ApiError ? loadError.message : 'Failed to load tables for query access.')
      } finally {
        if (active) {
          setLoadingQueryAccessTables(false)
        }
      }
    }

    void loadQueryAccessTables()

    return () => {
      active = false
    }
  }, [dbConnectionId, isQueryAccessTicket, prefilledTableName, queryAccessScopeMode, queryAccessTableDatabase, queryAccessTablePrefillApplied])

  useEffect(() => {
    if (!isQueryAccessTicket) {
      return
    }
    setDatabaseName('')
  }, [isQueryAccessTicket])

  function toggleDatabaseSelection(name: string) {
    setQueryAccessDatabaseSelections((current) =>
      current.includes(name) ? current.filter((item) => item !== name) : [...current, name],
    )
  }

  function toggleTableSelection(key: string) {
    setQueryAccessTableSelections((current) =>
      current.includes(key) ? current.filter((item) => item !== key) : [...current, key],
    )
  }

  function handleFormatSQL() {
    if (!sqlContent.trim()) {
      return
    }
    if (ticketType === 'redis_command') {
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
    if (sqlContent.trim() === '' || dbConnectionId === '' || (requiresDatabaseSelection && databaseName.trim() === '')) {
      return
    }
    setReviewing(true)
    setError('')
    try {
      const response = await reviewTicketSQL({
        sql_content: sqlContent,
        ticket_type: ticketType,
        db_connection_id: Number(dbConnectionId),
        database_name: requiresDatabaseSelection ? databaseName.trim() : '',
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
    if (!canSubmit) {
      return
    }
    setError('')
    setSubmitting(true)

    try {
      if (isQueryAccessTicket) {
        const duration = parseQueryAccessDuration(queryAccessDuration)
        if ('error' in duration) {
          setError(duration.error ?? 'Approved duration is invalid.')
          return
        }

        const items = queryAccessScopeMode === 'database'
          ? queryAccessDatabaseSelections.map((name) => ({
            database_name: name,
            table_name: null,
          }))
          : selectedQueryAccessItems.map((item) => ({
            database_name: item.databaseName,
            table_name: item.tableName,
          }))

        const created = await createTicket({
          title,
          description: description.trim() || null,
          sql_content: 'QUERY ACCESS REQUEST',
          ticket_type: ticketType,
          db_connection_id: Number(dbConnectionId),
          database_name: queryAccessScopeMode === 'table'
            ? queryAccessTableDatabase.trim() || null
            : queryAccessDatabaseSelections[0] ?? null,
          approved_duration_minutes: duration.minutes,
          scope_mode: queryAccessScopeMode,
          items,
        })
        navigate(`/tickets/${created.id}`, { replace: true })
        return
      }

      const created = await createTicket({
        title,
        description: description.trim() || null,
        sql_content: sqlContent,
        ticket_type: ticketType,
        db_connection_id: dbConnectionId ? Number(dbConnectionId) : null,
        database_name: requiresDatabaseSelection ? databaseName.trim() : null,
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
                    { value: 'redis_command', label: 'Redis' },
                    { value: 'query_access', label: 'Query Access' },
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
                    ...filteredConnections.map((connection) => ({
                      value: String(connection.id),
                      label: formatConnectionOptionLabel(connection),
                    })),
                  ]}
                />
              </label>
            </div>

            {requiresDatabaseSelection ? (
              <label className="flex flex-col gap-1.5">
                <span className="text-[12px] font-semibold text-ink">
                  {ticketType === 'redis_command' ? 'Target Database Index' : 'Target Database'} <span className="text-danger">*</span>
                </span>
                <DropdownSelect
                  ariaLabel={ticketType === 'redis_command' ? 'Target Database Index' : 'Target Database'}
                  value={databaseName}
                  onChange={setDatabaseName}
                  disabled={submitting || reviewing || loadingDatabases || dbConnectionId === '' || databases.length === 0}
                  placeholder={
                    loadingDatabases
                      ? 'Loading databases...'
                      : dbConnectionId === ''
                        ? 'Select instance first'
                        : ticketType === 'redis_command'
                          ? 'Select database index'
                          : 'Select database'
                  }
                  options={[
                    { value: '', label: 'Not Selected' },
                    ...databases.map((name) => ({ value: name, label: name })),
                  ]}
                />
              </label>
            ) : null}
          </div>
        </section>

        <section className="flex h-full flex-col rounded-xl border border-border bg-panel shadow-soft">
          <div className="border-b border-border/80 px-4 py-3">
            <div className="flex items-center gap-2">
              <ScrollText className="h-4 w-4 text-muted" />
              <p className="text-[13px] font-semibold text-ink">
                {isQueryAccessTicket ? 'Query Access Scope' : ticketType === 'redis_command' ? 'Command Content' : 'SQL Content'} {isQueryAccessTicket ? null : <span className="text-danger">*</span>}
              </p>
            </div>
          </div>

          {isQueryAccessTicket ? (
            <div className="grid gap-4 px-4 py-4">
              <div className="grid gap-4 sm:grid-cols-[180px_minmax(0,1fr)] sm:items-start">
                <div className="space-y-1.5">
                  <p className="text-[12px] font-semibold text-ink">Scope Level</p>
                  <p className="text-[12px] text-muted">Grant access by database or by table.</p>
                </div>
                <div className="inline-flex items-center rounded-lg border border-border bg-panel-soft p-1">
                  <button
                    type="button"
                    onClick={() => {
                      setQueryAccessScopeMode('database')
                      setQueryAccessTableSelections([])
                    }}
                    className={`inline-flex h-9 items-center rounded-md px-3 text-[12px] font-semibold ${
                      queryAccessScopeMode === 'database' ? 'bg-white text-ink shadow-soft' : 'text-muted hover:text-ink'
                    }`}
                  >
                    Database
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setQueryAccessScopeMode('table')
                      setQueryAccessDatabaseSelections([])
                    }}
                    className={`inline-flex h-9 items-center rounded-md px-3 text-[12px] font-semibold ${
                      queryAccessScopeMode === 'table' ? 'bg-white text-ink shadow-soft' : 'text-muted hover:text-ink'
                    }`}
                  >
                    Table
                  </button>
                </div>
              </div>

              <label className="flex flex-col gap-1.5">
                <span className="text-[12px] font-semibold text-ink">
                  Approved Duration (minutes) <span className="text-danger">*</span>
                </span>
                <input
                  type="number"
                  min={1}
                  max={MAX_QUERY_ACCESS_DURATION_MINUTES}
                  step={1}
                  inputMode="numeric"
                  value={queryAccessDuration}
                  onChange={(event) => setQueryAccessDuration(event.target.value)}
                  className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                  placeholder="60"
                  disabled={submitting}
                />
                <p className="text-[12px] text-muted">Maximum {MAX_QUERY_ACCESS_DURATION_MINUTES} minutes (3 days).</p>
              </label>

              {queryAccessScopeMode === 'database' ? (
                <div className="flex flex-col gap-1.5">
                  <span className="text-[12px] font-semibold text-ink">
                    Target Databases <span className="text-danger">*</span>
                  </span>
                  <div className="rounded-xl border border-border bg-panel-soft p-3">
                    {loadingDatabases ? (
                      <p className="text-[12px] text-muted">Loading databases...</p>
                    ) : dbConnectionId === '' ? (
                      <p className="text-[12px] text-muted">Select instance first.</p>
                    ) : databases.length === 0 ? (
                      <p className="text-[12px] text-muted">No databases available.</p>
                    ) : (
                      <div className="grid gap-2 sm:grid-cols-2">
                        {databases.map((name) => (
                          <label key={name} className="flex items-center gap-2 rounded-lg border border-border bg-white px-3 py-2 text-[13px] text-ink">
                            <input
                              type="checkbox"
                              checked={queryAccessDatabaseSelections.includes(name)}
                              onChange={() => toggleDatabaseSelection(name)}
                              disabled={submitting}
                            />
                            <span>{name}</span>
                          </label>
                        ))}
                      </div>
                    )}
                  </div>
                </div>
              ) : (
                <>
                  <label className="flex flex-col gap-1.5">
                    <span className="text-[12px] font-semibold text-ink">
                      Target Database <span className="text-danger">*</span>
                    </span>
                    <DropdownSelect
                      ariaLabel="Query Access Target Database"
                      value={queryAccessTableDatabase}
                      onChange={(value) => {
                        setQueryAccessTableDatabase(value)
                        setQueryAccessTableSelections([])
                        setQueryAccessTablePrefillApplied(false)
                      }}
                      disabled={submitting || loadingDatabases || dbConnectionId === ''}
                      placeholder={dbConnectionId === '' ? 'Select instance first' : 'Select database'}
                      options={[
                        { value: '', label: 'Not Selected' },
                        ...databases.map((name) => ({ value: name, label: name })),
                      ]}
                    />
                  </label>
                  <div className="flex flex-col gap-1.5">
                    <span className="text-[12px] font-semibold text-ink">
                      Target Tables <span className="text-danger">*</span>
                    </span>
                    <div className="rounded-xl border border-border bg-panel-soft p-3">
                      {loadingQueryAccessTables ? (
                        <p className="text-[12px] text-muted">Loading tables...</p>
                      ) : queryAccessTableDatabase.trim() === '' ? (
                        <p className="text-[12px] text-muted">Select database first.</p>
                      ) : queryAccessTables.length === 0 ? (
                        <p className="text-[12px] text-muted">No tables available.</p>
                      ) : (
                        <div className="grid gap-2 sm:grid-cols-2">
                          {queryAccessTables.map((item) => (
                            <label key={item.key} className="flex items-center gap-2 rounded-lg border border-border bg-white px-3 py-2 text-[13px] text-ink">
                              <input
                                type="checkbox"
                                checked={queryAccessTableSelections.includes(item.key)}
                                onChange={() => toggleTableSelection(item.key)}
                                disabled={submitting}
                              />
                              <span>{item.label}</span>
                            </label>
                          ))}
                        </div>
                      )}
                    </div>
                  </div>
                </>
              )}
            </div>
          ) : (
            <>
              <label className="flex flex-col gap-1.5 px-4 py-4">
                <span className="sr-only">SQL Content</span>
                <textarea
                  value={sqlContent}
                  onChange={(event) => setSqlContent(event.target.value)}
                  className="block min-h-[280px] w-full resize-y rounded-xl border border-border bg-panel-soft px-4 py-4 font-mono text-[13px] leading-7 text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20 lg:min-h-[320px]"
                  placeholder={ticketType === 'redis_command' ? 'SET my:key "value"\nEXPIRE my:key 60' : 'ALTER TABLE ...;\nUPDATE ...;'}
                  disabled={submitting}
                />
              </label>

              <div className="px-4 pb-4">
                <div className="flex flex-wrap items-center justify-end gap-2">
                  <button
                    type="button"
                    onClick={handleFormatSQL}
                    disabled={submitting || reviewing || sqlContent.trim() === '' || ticketType === 'redis_command'}
                    className="inline-flex h-10 items-center justify-center gap-2 rounded-lg border border-border bg-white px-4 text-[13px] font-semibold text-ink transition hover:bg-panel-soft disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    <Wand2 className="h-4 w-4" />
                    Format
                  </button>
                  <button
                    type="button"
                    onClick={() => void handleReviewSQL()}
                    disabled={submitting || reviewing || sqlContent.trim() === '' || dbConnectionId === '' || (requiresDatabaseSelection && databaseName.trim() === '')}
                    className="inline-flex h-10 items-center justify-center gap-2 rounded-lg border border-border bg-white px-4 text-[13px] font-semibold text-ink transition hover:bg-panel-soft disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {reviewing ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
                    {reviewing ? 'Reviewing...' : 'SQL Review'}
                  </button>
                </div>
              </div>
            </>
          )}
        </section>

        <div className="xl:col-span-2">
          {error ? (
            <div className="mb-4 rounded-lg border border-danger/20 bg-red-50 px-4 py-3 text-[13px] text-danger">
              {error}
            </div>
          ) : null}

          {!isQueryAccessTicket && reviewResults.length > 0 ? (
            <section className="mb-4 overflow-hidden rounded-xl border border-border bg-panel shadow-soft">
              <div className="border-b border-border/80 px-4 py-3">
                <div className="flex items-center justify-between gap-3">
                  <p className="text-[13px] font-semibold text-ink">Review Results</p>
                  <span className={`inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-[11px] font-semibold ${
                    reviewPassed ? 'bg-emerald-50 text-emerald-700' : 'bg-red-50 text-danger'
                  }`}>
                    {reviewPassed ? <CheckCircle2 className="h-3.5 w-3.5" /> : <XCircle className="h-3.5 w-3.5" />}
                    {reviewPassed ? 'Passed' : 'Failed'}
                  </span>
                </div>
              </div>
              {parserResults.length > 0 ? (
                <div className="px-4 pt-4">
                  <p className="text-[12px] font-semibold text-ink">Parser Results</p>
                  <div className="mt-3 overflow-x-auto rounded-xl border border-border">
                    <table className="min-w-full border-collapse">
                      <thead className="bg-panel-soft text-left text-[11px] font-semibold text-faint">
                        <tr>
                          <th className="px-4 py-3">ID</th>
                          <th className="px-4 py-3">{ticketType === 'redis_command' ? 'Command' : 'SQL'}</th>
                          <th className="px-4 py-3">Statement Kind</th>
                          <th className="px-4 py-3">Object Type</th>
                          <th className="px-4 py-3">Status</th>
                          <th className="px-4 py-3">Message</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-border text-[13px] text-ink">
                        {parserResults.map((result, index) => (
                          <tr key={`parser-${result.seq}-${index}`}>
                            <td className="px-4 py-3 align-top">{result.seq}</td>
                            <td className="px-4 py-3 align-top font-mono text-[12px]">{result.sql_stmt}</td>
                            <td className="px-4 py-3 align-top">{result.statement_kind || '—'}</td>
                            <td className="px-4 py-3 align-top">{result.object_type || '—'}</td>
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
                </div>
              ) : null}

              {validationResults.length > 0 ? (
                <div className="px-4 py-4">
                  <p className="text-[12px] font-semibold text-ink">Validation Results</p>
                  <div className="mt-3 overflow-x-auto rounded-xl border border-border">
                    <table className="min-w-full border-collapse">
                      <thead className="bg-panel-soft text-left text-[11px] font-semibold text-faint">
                        <tr>
                          <th className="px-4 py-3">ID</th>
                          <th className="px-4 py-3">{ticketType === 'redis_command' ? 'Command' : 'SQL'}</th>
                          <th className="px-4 py-3">Method</th>
                          <th className="px-4 py-3">Stage</th>
                          <th className="px-4 py-3">Kind</th>
                          <th className="px-4 py-3">Object</th>
                          <th className="px-4 py-3">Scan / Impact Rows</th>
                          <th className="px-4 py-3">Status</th>
                          <th className="px-4 py-3">Message</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-border text-[13px] text-ink">
                        {validationResults.map((result, index) => (
                          <tr key={`validation-${result.seq}-${index}`}>
                            <td className="px-4 py-3 align-top">{result.seq}</td>
                            <td className="px-4 py-3 align-top font-mono text-[12px]">{result.sql_stmt}</td>
                            <td className="px-4 py-3 align-top">{result.validation_method || '—'}</td>
                            <td className="px-4 py-3 align-top">{result.validation_stage || '—'}</td>
                            <td className="px-4 py-3 align-top">{result.statement_kind || '—'}</td>
                            <td className="px-4 py-3 align-top">{result.object_type || '—'}</td>
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
                </div>
              ) : null}
            </section>
          ) : null}

          <div className="flex flex-wrap items-center justify-end gap-2.5 px-1 py-1">
            <Link
              to="/tickets"
              className="inline-flex h-10 items-center justify-center rounded-lg border border-border bg-white px-4 text-[13px] font-semibold text-ink transition hover:bg-panel-soft"
            >
              Cancel
            </Link>
            <button
              type="submit"
              disabled={submitting || reviewing || !canSubmit}
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
