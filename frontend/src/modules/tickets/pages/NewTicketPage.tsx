import { useEffect, useMemo, useState } from 'react'
import { ArrowLeft, CheckCircle2, FileText, Loader2, Plus, ScrollText, Trash2, Wand2, XCircle } from 'lucide-react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { format as formatSQL } from 'sql-formatter'
import { ApiError } from '@/shared/api/client'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { PageIntro } from '@/shared/ui/PageIntro'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { MetadataResponse } from '@/shared/types/sqlEditor'
import type { QueryAccessEffect, TicketReviewResult, TicketType } from '@/shared/types/ticket'
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

type QueryAccessRuleDraft = {
  id: string
  effect: QueryAccessEffect
  connectionId: string
  databasePattern: string
  tablePattern: string
}

const QUERY_ACCESS_DURATION_OPTIONS = [
  { value: String(24 * 60), label: '1 day' },
  { value: String(7 * 24 * 60), label: '1 week' },
  { value: String(30 * 24 * 60), label: '1 month' },
  { value: String(365 * 24 * 60), label: '1 year' },
  { value: String(3 * 365 * 24 * 60), label: '3 years' },
]
const MAX_QUERY_ACCESS_DURATION_MINUTES = 3 * 365 * 24 * 60

function createQueryAccessRuleDraft(overrides: Partial<QueryAccessRuleDraft> = {}): QueryAccessRuleDraft {
  return {
    id: `rule-${Date.now()}-${Math.random().toString(16).slice(2)}`,
    effect: 'allow',
    connectionId: '',
    databasePattern: '*',
    tablePattern: '*',
    ...overrides,
  }
}

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
  const [queryAccessDuration, setQueryAccessDuration] = useState(String(24 * 60))
  const [queryAccessRules, setQueryAccessRules] = useState<QueryAccessRuleDraft[]>([
    createQueryAccessRuleDraft({
      connectionId: prefilledConnectionId,
      databasePattern: prefilledDatabaseName || '*',
      tablePattern: prefilledTableName || '*',
    }),
  ])
  const [queryAccessDatabasesByConnection, setQueryAccessDatabasesByConnection] = useState<Record<string, string[]>>({})
  const [queryAccessTablesByRule, setQueryAccessTablesByRule] = useState<Record<string, QueryAccessTableOption[]>>({})
  const [loadingQueryAccessConnections, setLoadingQueryAccessConnections] = useState<Record<string, boolean>>({})
  const [loadingQueryAccessRuleTables, setLoadingQueryAccessRuleTables] = useState<Record<string, boolean>>({})
  const [loadingConnections, setLoadingConnections] = useState(true)
  const [loadingDatabases, setLoadingDatabases] = useState(false)
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
  const requiresDatabaseSelection = !isQueryAccessTicket
  const hasValidQueryAccessScope = isQueryAccessTicket && (
    queryAccessRules.length > 0 &&
    queryAccessRules.every((rule) =>
      rule.connectionId !== '' &&
      rule.databasePattern.trim() !== '' &&
      rule.tablePattern.trim() !== '',
    )
  )
  const canSubmit = isQueryAccessTicket
    ? title.trim() !== '' && hasValidQueryAccessScope
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
    setDbConnectionId('')
  }, [isQueryAccessTicket])

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
    requiresDatabaseSelection,
    selectedConnection?.database_name,
  ])

  useEffect(() => {
    if (!isQueryAccessTicket) {
      return
    }
    setDatabaseName('')
  }, [isQueryAccessTicket])

  useEffect(() => {
    if (!isQueryAccessTicket) {
      return
    }
    queryAccessRules.forEach((rule) => {
      if (rule.connectionId !== '') {
        void loadQueryAccessDatabases(rule.connectionId)
      }
      if (rule.connectionId !== '' && rule.databasePattern !== '*') {
        void loadQueryAccessRuleTables(rule)
      }
    })
  }, [isQueryAccessTicket, queryAccessRules])

  async function loadQueryAccessDatabases(connectionId: string) {
    if (connectionId === '' || queryAccessDatabasesByConnection[connectionId]) {
      return
    }
    setLoadingQueryAccessConnections((current) => ({ ...current, [connectionId]: true }))
    try {
      const response = await listTicketDatabases(Number(connectionId))
      setQueryAccessDatabasesByConnection((current) => ({
        ...current,
        [connectionId]: response.databases.map((item) => item.name.trim()).filter((item) => item !== ''),
      }))
    } catch (loadError) {
      setError(loadError instanceof ApiError ? loadError.message : 'Failed to load query access databases.')
    } finally {
      setLoadingQueryAccessConnections((current) => ({ ...current, [connectionId]: false }))
    }
  }

  async function loadQueryAccessRuleTables(rule: QueryAccessRuleDraft) {
    if (rule.connectionId === '' || rule.databasePattern === '*' || queryAccessTablesByRule[rule.id]) {
      return
    }
    setLoadingQueryAccessRuleTables((current) => ({ ...current, [rule.id]: true }))
    try {
      const response = await listMetadata(Number(rule.connectionId), { database: rule.databasePattern })
      let nextTables = normalizeTableOptions(response, rule.databasePattern)
      if (response.level === 'schema') {
        const schemaResponses = await Promise.all(
          response.items
            .filter((item) => item.kind === 'schema')
            .map((item) => listMetadata(Number(rule.connectionId), {
              database: rule.databasePattern,
              schema: item.name,
            })),
        )
        nextTables = schemaResponses.flatMap((item) => normalizeTableOptions(item, rule.databasePattern))
      }
      setQueryAccessTablesByRule((current) => ({ ...current, [rule.id]: nextTables }))
    } catch (loadError) {
      setQueryAccessTablesByRule((current) => ({ ...current, [rule.id]: [] }))
      setError(loadError instanceof ApiError ? loadError.message : 'Failed to load query access tables.')
    } finally {
      setLoadingQueryAccessRuleTables((current) => ({ ...current, [rule.id]: false }))
    }
  }

  function updateQueryAccessRule(ruleId: string, patch: Partial<QueryAccessRuleDraft>) {
    setQueryAccessRules((current) => current.map((rule) => {
      if (rule.id !== ruleId) {
        return rule
      }
      const nextRule = { ...rule, ...patch }
      if (patch.connectionId != null) {
        nextRule.databasePattern = '*'
        nextRule.tablePattern = '*'
        setQueryAccessTablesByRule((tables) => {
          const nextTables = { ...tables }
          delete nextTables[ruleId]
          return nextTables
        })
        void loadQueryAccessDatabases(patch.connectionId)
      }
      if (patch.databasePattern != null) {
        nextRule.tablePattern = '*'
        setQueryAccessTablesByRule((tables) => {
          const nextTables = { ...tables }
          delete nextTables[ruleId]
          return nextTables
        })
        if (nextRule.connectionId !== '' && patch.databasePattern !== '*') {
          void loadQueryAccessRuleTables(nextRule)
        }
      }
      return nextRule
    }))
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

        const rules = queryAccessRules.map((rule) => ({
          effect: rule.effect,
          connection_id: Number(rule.connectionId),
          database_pattern: rule.databasePattern.trim() || '*',
          table_pattern: rule.tablePattern.trim() || '*',
        }))

        const created = await createTicket({
          title,
          description: description.trim() || null,
          sql_content: 'QUERY ACCESS REQUEST',
          ticket_type: ticketType,
          db_connection_id: rules[0]?.connection_id ?? null,
          database_name: rules[0]?.database_pattern === '*' ? null : rules[0]?.database_pattern ?? null,
          approved_duration_minutes: duration.minutes,
          rules,
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

            <div className={`grid gap-4 ${isQueryAccessTicket ? '' : 'sm:grid-cols-2'}`}>
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

              {!isQueryAccessTicket ? (
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
              ) : null}
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
              <label className="flex flex-col gap-1.5">
                <span className="text-[12px] font-semibold text-ink">
                  Access Duration <span className="text-danger">*</span>
                </span>
                <DropdownSelect
                  ariaLabel="Query Access Duration"
                  value={queryAccessDuration}
                  onChange={setQueryAccessDuration}
                  disabled={submitting}
                  options={QUERY_ACCESS_DURATION_OPTIONS}
                />
                <p className="text-[12px] text-muted">The maximum query access duration is 3 years.</p>
              </label>

              <div className="space-y-3">
                <div className="flex items-center justify-between gap-3">
                  <div>
                    <p className="text-[12px] font-semibold text-ink">Access Rules</p>
                    <p className="text-[12px] text-muted">Use Allow rules to grant access. Use Deny rules to exclude databases or tables from a broader Allow rule.</p>
                  </div>
                  <button
                    type="button"
                    onClick={() => setQueryAccessRules((current) => [...current, createQueryAccessRuleDraft()])}
                    disabled={submitting}
                    className="inline-flex h-9 items-center justify-center gap-2 rounded-lg border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-panel-soft disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    <Plus className="h-4 w-4" />
                    Add Rule
                  </button>
                </div>

                <div className="space-y-3">
                  {queryAccessRules.map((rule, index) => {
                    const ruleDatabases = queryAccessDatabasesByConnection[rule.connectionId] ?? []
                    const ruleTables = queryAccessTablesByRule[rule.id] ?? []
                    const selectedRuleConnection = connections.find((connection) => String(connection.id) === rule.connectionId)
                    const tableOptions = rule.databasePattern === '*' ? [] : ruleTables
                    return (
                      <div key={rule.id} className="rounded-xl border border-border bg-panel-soft p-3">
                        <div className="mb-3 flex items-center justify-between gap-3">
                          <p className="text-[12px] font-semibold text-ink">Rule {index + 1}</p>
                          <button
                            type="button"
                            onClick={() => setQueryAccessRules((current) => current.length === 1 ? current : current.filter((item) => item.id !== rule.id))}
                            disabled={submitting || queryAccessRules.length === 1}
                            className="inline-flex h-8 items-center justify-center gap-1 rounded-lg border border-border bg-white px-2 text-[12px] font-semibold text-muted transition hover:text-danger disabled:cursor-not-allowed disabled:opacity-50"
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                            Remove
                          </button>
                        </div>
                        <div className="grid gap-3 lg:grid-cols-[120px_minmax(0,1.2fr)_minmax(0,1fr)_minmax(0,1fr)]">
                          <label className="flex flex-col gap-1.5">
                            <span className="text-[11px] font-semibold uppercase tracking-wide text-faint">Effect</span>
                            <DropdownSelect
                              ariaLabel={`Query Access Rule ${index + 1} Effect`}
                              value={rule.effect}
                              onChange={(value) => updateQueryAccessRule(rule.id, { effect: value as QueryAccessEffect })}
                              disabled={submitting}
                              options={[
                                { value: 'allow', label: 'Allow' },
                                { value: 'deny', label: 'Deny' },
                              ]}
                            />
                          </label>
                          <label className="flex flex-col gap-1.5">
                            <span className="text-[11px] font-semibold uppercase tracking-wide text-faint">Instance</span>
                            <DropdownSelect
                              ariaLabel={`Query Access Rule ${index + 1} Instance`}
                              value={rule.connectionId}
                              onChange={(value) => updateQueryAccessRule(rule.id, { connectionId: value })}
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
                          <label className="flex flex-col gap-1.5">
                            <span className="text-[11px] font-semibold uppercase tracking-wide text-faint">Database</span>
                            <DropdownSelect
                              ariaLabel={`Query Access Rule ${index + 1} Database`}
                              value={rule.databasePattern}
                              onChange={(value) => updateQueryAccessRule(rule.id, { databasePattern: value })}
                              disabled={submitting || rule.connectionId === '' || loadingQueryAccessConnections[rule.connectionId]}
                              placeholder={rule.connectionId === '' ? 'Select instance first' : 'Select database'}
                              options={[
                                { value: '*', label: 'All Databases' },
                                ...ruleDatabases.map((name) => ({ value: name, label: name })),
                              ]}
                            />
                          </label>
                          <label className="flex flex-col gap-1.5">
                            <span className="text-[11px] font-semibold uppercase tracking-wide text-faint">Table</span>
                            <DropdownSelect
                              ariaLabel={`Query Access Rule ${index + 1} Table`}
                              value={rule.tablePattern}
                              onChange={(value) => updateQueryAccessRule(rule.id, { tablePattern: value })}
                              disabled={submitting || rule.connectionId === '' || rule.databasePattern === '*' || loadingQueryAccessRuleTables[rule.id]}
                              placeholder={rule.databasePattern === '*' ? 'All tables' : 'Select table'}
                              options={[
                                { value: '*', label: 'All Tables' },
                                ...tableOptions.map((item) => ({ value: item.tableName, label: item.label })),
                              ]}
                            />
                          </label>
                        </div>
                        {selectedRuleConnection ? (
                          <p className="mt-2 text-[11px] text-muted">
                            {rule.effect === 'deny' ? 'Exclude' : 'Grant'} {selectedRuleConnection.name} / {rule.databasePattern === '*' ? 'all databases' : rule.databasePattern} / {rule.tablePattern === '*' ? 'all tables' : rule.tablePattern}
                          </p>
                        ) : null}
                      </div>
                    )
                  })}
                </div>
              </div>
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
