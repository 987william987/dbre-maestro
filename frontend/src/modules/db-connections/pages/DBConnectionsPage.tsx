import { useEffect, useMemo, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Loader2, Pencil, Plus, ServerCog, Trash2, X } from 'lucide-react'
import { createDBConnection, deleteDBConnection, listDBConnections, patchDBConnection, testDBConnection, testRollbackCapability } from '@/modules/db-connections/api'
import { cn } from '@/lib/utils'
import { ApiError } from '@/shared/api/client'
import { useAuth } from '@/shared/auth/AuthContext'
import { formatDateTime } from '@/shared/lib/format'
import type { DBConnection } from '@/shared/types/dbConnection'
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { Pagination } from '@/shared/ui/Pagination'
import { SearchInput } from '@/shared/ui/SearchInput'
import { useToast } from '@/shared/ui/ToastContext'
import {
  DataTable,
  DataTableBody,
  DataTableCell,
  DataTableHead,
  DataTableHeaderCell,
  DataTableRow,
  DataTableScroll,
  DataTableSurface,
} from '@/shared/ui/DataTable'

type DrawerState =
  | { mode: 'create' }
  | { mode: 'edit'; connectionId: number }
  | null

type ConnectionForm = {
  name: string
  dbType: 'mysql' | 'postgres' | 'redis'
  readonlyHost: string
  readonlyPort: string
  readwriteHost: string
  readwritePort: string
  readonlyUsername: string
  readonlyPassword: string
  readwriteUsername: string
  readwritePassword: string
  rollbackUsername: string
  rollbackPassword: string
  sslMode: 'prefer' | 'disable' | 'require'
}

const DEFAULT_PORT_BY_DB_TYPE: Record<ConnectionForm['dbType'], string> = {
  mysql: '3306',
  postgres: '5432',
  redis: '6379',
}

const EMPTY_FORM: ConnectionForm = {
  name: '',
  dbType: 'mysql',
  readonlyHost: '',
  readonlyPort: '3306',
  readwriteHost: '',
  readwritePort: '3306',
  readonlyUsername: '',
  readonlyPassword: '',
  readwriteUsername: '',
  readwritePassword: '',
  rollbackUsername: '',
  rollbackPassword: '',
  sslMode: 'prefer',
}

const SSL_MODE_OPTIONS = [
  { value: 'prefer', label: 'prefer', description: 'Use SSL when the server supports it, and fall back to an unencrypted connection when it does not.' },
  { value: 'disable', label: 'disable', description: 'Do not use SSL. This is usually only appropriate for private networks or local development.' },
  { value: 'require', label: 'require', description: 'Require SSL. The connection fails if the target does not support it.' },
] as const

const DB_TYPE_OPTIONS = [
  { value: 'mysql', label: 'MySQL / MariaDB' },
  { value: 'postgres', label: 'PostgreSQL' },
  { value: 'redis', label: 'Redis' },
] as const

const PAGE_SIZE = 20

export function DBConnectionsPage() {
  const { user } = useAuth()
  const { pushToast } = useToast()
  const canWrite = user?.permissions.includes('db_connections.write') ?? false
  const [connections, setConnections] = useState<DBConnection[]>([])
  const [offset, setOffset] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [drawerState, setDrawerState] = useState<DrawerState>(null)
  const [drawerError, setDrawerError] = useState('')
  const [drawerLoading, setDrawerLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [form, setForm] = useState<ConnectionForm>(EMPTY_FORM)
  const [selectedConnection, setSelectedConnection] = useState<DBConnection | null>(null)
  const [connectionTestingId, setConnectionTestingId] = useState<number | null>(null)
  const [rollbackTestingId, setRollbackTestingId] = useState<number | null>(null)
  const [deletingId, setDeletingId] = useState<number | null>(null)
  const [pendingDeleteId, setPendingDeleteId] = useState<number | null>(null)
  const [nameKeyword, setNameKeyword] = useState('')
  const [typeKeyword, setTypeKeyword] = useState('')
  const [endpointKeyword, setEndpointKeyword] = useState('')

  useEffect(() => {
    void loadConnections()
  }, [])

  const activeSSLMode = SSL_MODE_OPTIONS.find((option) => option.value === form.sslMode)
  const drawerReadOnly = !canWrite
  async function loadConnections() {
    setLoading(true)
    setError('')
    try {
      const response = await listDBConnections()
      setConnections(response.connections)
    } catch (loadError) {
      setError(loadError instanceof ApiError ? loadError.message : 'Failed to load database connections.')
    } finally {
      setLoading(false)
    }
  }

  function openCreateDrawer() {
    setDrawerState({ mode: 'create' })
    setDrawerError('')
    setDrawerLoading(false)
    setSelectedConnection(null)
    setForm(EMPTY_FORM)
  }

  function openEditDrawer(connection: DBConnection) {
    setDrawerState({ mode: 'edit', connectionId: connection.id })
    setDrawerError('')
    setDrawerLoading(false)
    setSelectedConnection(connection)
    setForm({
      name: connection.name,
      dbType: normalizeDBType(connection.db_type),
      readonlyHost: connection.readonly_host || connection.host,
      readonlyPort: String(connection.readonly_port || connection.port),
      readwriteHost: connection.readwrite_host || connection.readonly_host || connection.host,
      readwritePort: String(connection.readwrite_port || connection.readonly_port || connection.port),
      readonlyUsername: findCredentialUsername(connection, 'readonly') ?? connection.username ?? '',
      readonlyPassword: '',
      readwriteUsername: findCredentialUsername(connection, 'readwrite') ?? '',
      readwritePassword: '',
      rollbackUsername: findCredentialUsername(connection, 'rollback') ?? '',
      rollbackPassword: '',
      sslMode: normalizeSSLMode(connection.ssl_mode),
    })
  }

  function closeDrawer() {
    setDrawerState(null)
    setDrawerError('')
    setDrawerLoading(false)
    setSubmitting(false)
    setSelectedConnection(null)
    setForm(EMPTY_FORM)
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canWrite) {
      return
    }
    if (!drawerState) {
      return
    }

    setSubmitting(true)
    setDrawerError('')

    try {
      if (drawerState.mode === 'create') {
        await createDBConnection(toPayload(form))
        pushToast('Database connection created', 'success')
      } else if (selectedConnection) {
        const endpointError = getEndpointPasswordError(selectedConnection, form)
        if (endpointError) {
          setDrawerError(endpointError)
          return
        }
        await patchDBConnection(selectedConnection.id, toPayload(form))
        pushToast('Database connection updated', 'success')
      }
      await loadConnections()
      closeDrawer()
    } catch (submitError) {
      setDrawerError(submitError instanceof ApiError ? submitError.message : drawerState.mode === 'create' ? 'Failed to create the database connection.' : 'Failed to update the database connection.')
    } finally {
      setSubmitting(false)
    }
  }

  async function handleTest(id: number) {
    if (!canWrite) {
      return
    }
    setConnectionTestingId(id)
    setError('')
    const targetConnection = connections.find((connection) => connection.id === id)
    const connectionName = targetConnection?.name ?? `#${id}`
    try {
      const result = await testDBConnection(id)
      setConnections((current) => current.map((connection) => (
        connection.id === id
          ? {
            ...connection,
            last_test_status: result.last_test_status,
            last_test_error: result.last_test_error ?? null,
            last_tested_at: result.last_tested_at ?? new Date().toISOString(),
          }
          : connection
      )))
      if (result.ok) {
        pushToast(`${connectionName} connection test succeeded for readonly and readwrite`, 'success', { placement: 'center' })
      } else {
        pushToast(`${connectionName} connection test failed: ${formatConnectionTestFailure(result)}`, 'error', { placement: 'center', durationMs: 4200 })
      }
    } catch (testError) {
      const message = testError instanceof ApiError ? testError.message : 'Connection test failed'
      pushToast(`${connectionName} connection test failed: ${message}`, 'error', { placement: 'center', durationMs: 3600 })
    } finally {
      setConnectionTestingId(null)
    }
  }

  async function handleTestRollback(id: number) {
    if (!canWrite) {
      return
    }
    setRollbackTestingId(id)
    setError('')
    const targetConnection = connections.find((connection) => connection.id === id)
    const connectionName = targetConnection?.name ?? `#${id}`
    try {
      const result = await testRollbackCapability(id)
      if (result.ok) {
        const binlog = result.binlog ? ` (${result.binlog.file}:${result.binlog.pos})` : ''
        pushToast(`${connectionName} rollback capability test passed${binlog}`, 'success', { placement: 'center', durationMs: 4200 })
      } else {
        pushToast(`${connectionName} rollback capability test failed: ${result.message}`, 'error', { placement: 'center', durationMs: 5200 })
      }
    } catch (testError) {
      const message = testError instanceof ApiError ? testError.message : 'Rollback capability test failed'
      pushToast(`${connectionName} rollback capability test failed: ${message}`, 'error', { placement: 'center', durationMs: 4200 })
    } finally {
      setRollbackTestingId(null)
    }
  }

  async function handleDelete(id: number) {
    if (!canWrite) {
      return
    }
    setDeletingId(id)
    setError('')
    try {
      await deleteDBConnection(id)
      await loadConnections()
      pushToast('Database connection deleted', 'success')
      if (drawerState?.mode === 'edit' && drawerState.connectionId === id) {
        closeDrawer()
      }
    } catch (deleteError) {
      setError(deleteError instanceof ApiError ? deleteError.message : 'Failed to delete the database connection.')
    } finally {
      setDeletingId(null)
      setPendingDeleteId(null)
    }
  }

  const sortedConnections = useMemo(() => {
    const normalizedNameKeyword = nameKeyword.trim().toLowerCase()
    const normalizedTypeKeyword = typeKeyword.trim().toLowerCase()
    const normalizedEndpointKeyword = endpointKeyword.trim().toLowerCase()

    return connections.filter((connection) => {
      const nameMatches = normalizedNameKeyword === '' || connection.name.toLowerCase().includes(normalizedNameKeyword)
      const typeText = `${connection.db_type} ${formatDBType(connection.db_type)}`.toLowerCase()
      const typeMatches = normalizedTypeKeyword === '' || typeText.includes(normalizedTypeKeyword)
      const endpointText = [
        formatReadonlyConnectionTarget(connection),
        formatReadwriteConnectionTarget(connection),
        connection.host,
        connection.readonly_host,
        connection.readwrite_host,
      ].filter(Boolean).join(' ').toLowerCase()
      const endpointMatches = normalizedEndpointKeyword === '' || endpointText.includes(normalizedEndpointKeyword)
      return nameMatches && typeMatches && endpointMatches
    }).sort((left, right) => {
      const leftFailed = left.last_test_status === 'failed' ? 1 : 0
      const rightFailed = right.last_test_status === 'failed' ? 1 : 0
      if (leftFailed !== rightFailed) {
        return rightFailed - leftFailed
      }
      return right.created_at.localeCompare(left.created_at)
    })
  }, [connections, endpointKeyword, nameKeyword, typeKeyword])
  const pagedConnections = useMemo(() => sortedConnections.slice(offset, offset + PAGE_SIZE), [offset, sortedConnections])
  const endpointPasswordError = drawerState?.mode === 'edit' && selectedConnection
    ? getEndpointPasswordError(selectedConnection, form)
    : ''

  useEffect(() => {
    if (offset > 0 && offset >= sortedConnections.length) {
      setOffset(Math.max(0, Math.floor((Math.max(sortedConnections.length - 1, 0)) / PAGE_SIZE) * PAGE_SIZE))
    }
  }, [offset, sortedConnections.length])

  return (
    <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
      <div className="grid gap-3 lg:grid-cols-[minmax(180px,0.85fr)_140px_minmax(260px,1fr)_auto]">
        <SearchInput
          aria-label="Connection name search"
          value={nameKeyword}
          onChange={(event) => {
            setNameKeyword(event.target.value)
            setOffset(0)
          }}
          placeholder="Name"
        />
        <SearchInput
          aria-label="Connection type search"
          value={typeKeyword}
          onChange={(event) => {
            setTypeKeyword(event.target.value)
            setOffset(0)
          }}
          placeholder="Type"
        />
        <SearchInput
          aria-label="Connection endpoint search"
          value={endpointKeyword}
          onChange={(event) => {
            setEndpointKeyword(event.target.value)
            setOffset(0)
          }}
          placeholder="Endpoint"
        />
        {canWrite ? (
        <div className="flex justify-end">
          <button
            type="button"
            onClick={openCreateDrawer}
            className="inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg bg-brand px-3 text-[12px] font-bold text-white shadow-soft transition hover:bg-slate-800"
          >
            <Plus className="h-4 w-4" />
            New Connection
          </button>
        </div>
        ) : null}
      </div>

      <DataTableSurface>
            {loading ? (
              <LoadingBlock message="Loading connections..." className="h-48 rounded-none border-0 bg-transparent" />
            ) : sortedConnections.length === 0 ? (
              <div className="m-4 flex h-48 items-center justify-center rounded-xl border border-dashed border-border bg-panel-soft text-sm text-muted">
                {connections.length === 0 ? 'No database connections yet.' : 'No database connections match the current filters.'}
              </div>
            ) : (
              <DataTableScroll>
                <DataTable>
                  <DataTableHead>
                    <tr>
                      <DataTableHeaderCell>Name</DataTableHeaderCell>
                      <DataTableHeaderCell>Type</DataTableHeaderCell>
                      <DataTableHeaderCell>Readonly Endpoint</DataTableHeaderCell>
                      <DataTableHeaderCell>Readwrite Endpoint</DataTableHeaderCell>
                      <DataTableHeaderCell>
                        <div className="group relative inline-flex">
                          <span>SSL</span>
                          <div className="pointer-events-none absolute left-0 top-[calc(100%+8px)] z-10 hidden w-52 rounded-md border border-border bg-white px-3 py-2 text-[11px] font-medium normal-case tracking-normal text-muted shadow-soft group-hover:block">
                            Use SSL when supported, otherwise fall back to an unencrypted connection.
                          </div>
                        </div>
                      </DataTableHeaderCell>
                      <DataTableHeaderCell>Created</DataTableHeaderCell>
                      <DataTableHeaderCell>Updated</DataTableHeaderCell>
                      <DataTableHeaderCell>Actions</DataTableHeaderCell>
                    </tr>
                  </DataTableHead>
                  <DataTableBody>
                    {pagedConnections.map((connection) => {
                      const isFailed = connection.last_test_status === 'failed'
                      return (
                        <DataTableRow
                          key={connection.id}
                          className={
                            isFailed
                              ? 'border-red-200 bg-red-50/75 text-danger hover:bg-red-50'
                              : undefined
                          }
                        >
                          <DataTableCell>
                            <p className="whitespace-nowrap">{connection.name}</p>
                          </DataTableCell>
                          <DataTableCell className={`whitespace-nowrap ${isFailed ? 'text-danger' : ''}`}>{formatDBType(connection.db_type)}</DataTableCell>
                          <DataTableCell>
                            <ExpandableEndpointValue value={formatReadonlyConnectionTarget(connection)} className={isFailed ? 'text-danger' : ''} />
                          </DataTableCell>
                          <DataTableCell>
                            <ExpandableEndpointValue value={formatReadwriteConnectionTarget(connection)} className={isFailed ? 'text-danger' : ''} />
                          </DataTableCell>
                          <DataTableCell className="whitespace-nowrap">
                            <p className={isFailed ? 'text-danger' : ''}>{connection.ssl_mode}</p>
                          </DataTableCell>
                          <DataTableCell className={`whitespace-nowrap ${isFailed ? 'text-danger/80' : ''}`}>{formatDateTime(connection.created_at)}</DataTableCell>
                          <DataTableCell className={`whitespace-nowrap ${isFailed ? 'text-danger/80' : ''}`}>{formatDateTime(connection.updated_at)}</DataTableCell>
                          <DataTableCell>
                            <div className="flex flex-nowrap items-center gap-1 whitespace-nowrap">
                              {canWrite ? (
                                <button
                                  type="button"
                                  onClick={() => void handleTest(connection.id)}
                                  disabled={connectionTestingId === connection.id}
                                  className="inline-flex h-7 items-center justify-center gap-1 rounded-md border border-border bg-panel-soft px-2 text-[11px] font-semibold text-ink transition hover:bg-page disabled:opacity-50"
                                >
                                  {connectionTestingId === connection.id ? <Loader2 className="h-3 w-3 animate-spin" /> : null}
                                  Test
                                </button>
                              ) : null}
                              {canWrite && connection.db_type === 'mysql' ? (
                                <button
                                  type="button"
                                  onClick={() => void handleTestRollback(connection.id)}
                                  disabled={rollbackTestingId === connection.id}
                                  className="inline-flex h-7 items-center justify-center gap-1 rounded-md border border-border bg-panel-soft px-2 text-[11px] font-semibold text-ink transition hover:bg-page disabled:opacity-50"
                                >
                                  {rollbackTestingId === connection.id ? <Loader2 className="h-3 w-3 animate-spin" /> : null}
                                  Test Rollback
                                </button>
                              ) : null}
                              <button
                                type="button"
                                onClick={() => openEditDrawer(connection)}
                                className="inline-flex h-7 items-center justify-center gap-1 rounded-md border border-border bg-panel-soft px-2 text-[11px] font-semibold text-ink transition hover:bg-page"
                              >
                                {canWrite ? <Pencil className="h-3 w-3" /> : null}
                                {canWrite ? 'Edit' : 'View'}
                              </button>
                              {canWrite ? (
                                <button
                                  type="button"
                                  onClick={() => setPendingDeleteId(connection.id)}
                                  disabled={deletingId === connection.id}
                                  className="inline-flex h-7 items-center justify-center gap-1 rounded-md border border-danger/20 bg-red-50 px-2 text-[11px] font-semibold text-danger transition hover:bg-red-100 disabled:opacity-50"
                                >
                                  {deletingId === connection.id ? <Loader2 className="h-3 w-3 animate-spin" /> : <Trash2 className="h-3 w-3" />}
                                  Delete
                                </button>
                              ) : null}
                            </div>
                          </DataTableCell>
                        </DataTableRow>
                      )
                    })}
                  </DataTableBody>
                </DataTable>
              </DataTableScroll>
            )}
      </DataTableSurface>

      <Pagination
        offset={offset}
        pageSize={PAGE_SIZE}
        count={pagedConnections.length}
        total={sortedConnections.length}
        onChange={setOffset}
      />

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      {drawerState ? createPortal(
        <div className="fixed inset-0 z-[110] flex justify-end bg-slate-950/28 px-3 py-3 sm:px-4 sm:py-4">
          <div
            role="dialog"
            aria-modal="true"
            aria-labelledby="db-connections-drawer-title"
            className="flex h-full w-full max-w-[680px] flex-col overflow-hidden rounded-xl border border-border bg-panel shadow-[0_22px_60px_rgba(15,23,42,0.18)]"
          >
            <div className="flex items-start justify-between border-b border-border/80 px-5 py-4">
              <div>
                <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">Connection Detail</p>
                <h3 id="db-connections-drawer-title" className="mt-1 text-[22px] font-bold tracking-[-0.03em] text-ink">
                  {drawerState.mode === 'create' ? 'New Connection' : selectedConnection?.name ?? (canWrite ? 'Edit Connection' : 'View Connection')}
                </h3>
              </div>
              <button
                type="button"
                onClick={closeDrawer}
                className="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-border bg-panel-soft text-muted transition hover:bg-page hover:text-ink"
                aria-label="Close"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
              {drawerLoading ? (
                <LoadingBlock message="Loading details..." className="min-h-[240px] rounded-xl border-border bg-panel" />
              ) : (
                <div className="grid gap-4">
                  <CardSection title="Connection Profile" icon={<ServerCog className="h-4 w-4 text-accent" />}>
                    <form className="grid gap-3" onSubmit={handleSubmit}>
                      <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                        Name
                        <input
                          value={form.name}
                          onChange={(event) => setForm((current) => ({ ...current, name: event.target.value }))}
                          className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                          disabled={submitting || drawerReadOnly}
                        />
                      </label>

                      <div className="grid gap-3 sm:grid-cols-2">
                        <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                          DB Type
                          <DropdownSelect
                            ariaLabel="DB Type"
                            value={form.dbType}
                            onChange={(value) => {
                              const nextType = normalizeDBType(value)
                              setForm((current) => ({
                                ...current,
                                dbType: nextType,
                                readonlyPort: current.readonlyPort === DEFAULT_PORT_BY_DB_TYPE[current.dbType] || current.readonlyPort === '' ? DEFAULT_PORT_BY_DB_TYPE[nextType] : current.readonlyPort,
                                readwritePort: current.readwritePort === DEFAULT_PORT_BY_DB_TYPE[current.dbType] || current.readwritePort === '' ? DEFAULT_PORT_BY_DB_TYPE[nextType] : current.readwritePort,
                              }))
                            }}
                            disabled={submitting || drawerReadOnly}
                            options={DB_TYPE_OPTIONS.map((option) => ({ value: option.value, label: option.label }))}
                          />
                        </label>
                        <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                          SSL Mode
                          <DropdownSelect
                            ariaLabel="SSL Mode"
                            value={form.sslMode}
                            onChange={(value) => setForm((current) => ({ ...current, sslMode: normalizeSSLMode(value) }))}
                            disabled={submitting || drawerReadOnly}
                            options={SSL_MODE_OPTIONS.map((option) => ({ value: option.value, label: option.label }))}
                          />
                        </label>
                      </div>

                      <div className="grid gap-3 sm:grid-cols-2">
                        <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                          Readonly Host
                          <input
                            value={form.readonlyHost}
                            onChange={(event) => setForm((current) => ({ ...current, readonlyHost: event.target.value }))}
                            className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                            disabled={submitting || drawerReadOnly}
                          />
                        </label>
                        <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                          Readonly Port
                          <input
                            value={form.readonlyPort}
                            onChange={(event) => setForm((current) => ({ ...current, readonlyPort: event.target.value }))}
                            className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                            disabled={submitting || drawerReadOnly}
                          />
                        </label>
                      </div>

                      <div className="grid gap-3 sm:grid-cols-2">
                        <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                          Readwrite Host
                          <input
                            value={form.readwriteHost}
                            onChange={(event) => setForm((current) => ({ ...current, readwriteHost: event.target.value }))}
                            className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                            disabled={submitting || drawerReadOnly}
                          />
                        </label>
                        <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                          Readwrite Port
                          <input
                            value={form.readwritePort}
                            onChange={(event) => setForm((current) => ({ ...current, readwritePort: event.target.value }))}
                            className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                            disabled={submitting || drawerReadOnly}
                          />
                        </label>
                      </div>

                      <div className="grid gap-3 sm:grid-cols-2">
                        <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                          Readonly Username
                          <input
                            value={form.readonlyUsername}
                            onChange={(event) => setForm((current) => ({ ...current, readonlyUsername: event.target.value }))}
                            className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                            disabled={submitting || drawerReadOnly}
                          />
                        </label>
                        <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                          Readonly Password
                          <input
                            value={form.readonlyPassword}
                            type="password"
                            onChange={(event) => setForm((current) => ({ ...current, readonlyPassword: event.target.value }))}
                            placeholder={drawerState.mode === 'edit' ? 'Required when readonly endpoint changes' : ''}
                            className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                            disabled={submitting || drawerReadOnly}
                          />
                        </label>
                      </div>

                      <div className="grid gap-3 sm:grid-cols-2">
                        <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                          Readwrite Username
                          <input
                            value={form.readwriteUsername}
                            onChange={(event) => setForm((current) => ({ ...current, readwriteUsername: event.target.value }))}
                            className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                            disabled={submitting || drawerReadOnly}
                          />
                        </label>
                        <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                          Readwrite Password
                          <input
                            value={form.readwritePassword}
                            type="password"
                            onChange={(event) => setForm((current) => ({ ...current, readwritePassword: event.target.value }))}
                            placeholder={drawerState.mode === 'edit' ? 'Required when readwrite endpoint changes' : ''}
                            className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                            disabled={submitting || drawerReadOnly}
                          />
                        </label>
                      </div>

                      {form.dbType === 'mysql' ? (
                        <div className="grid gap-3 sm:grid-cols-2">
                          <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                            Rollback Username
                            <input
                              value={form.rollbackUsername}
                              onChange={(event) => setForm((current) => ({ ...current, rollbackUsername: event.target.value }))}
                              className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                              disabled={submitting || drawerReadOnly}
                            />
                          </label>
                          <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                            Rollback Password
                            <input
                              value={form.rollbackPassword}
                              type="password"
                              onChange={(event) => setForm((current) => ({ ...current, rollbackPassword: event.target.value }))}
                              placeholder={drawerState.mode === 'edit' ? 'Leave blank to keep existing rollback password' : ''}
                              className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                              disabled={submitting || drawerReadOnly}
                            />
                          </label>
                        </div>
                      ) : null}

                      <div className="rounded-lg border border-border bg-panel-soft px-3 py-3 text-[12px] text-muted">
                        <p className="font-semibold text-ink">Credential Policy</p>
                        <p className="mt-1">SQL Editor and DB Metadata use `readonly`; ticket execution uses `readwrite`; MySQL rollback generation uses `rollback` on the writer endpoint. In edit mode, leaving a password blank keeps the current password only when the corresponding endpoint is unchanged.</p>
                      </div>

                      <div className="rounded-lg border border-border bg-panel-soft px-3 py-3 text-[12px] text-muted">
                        <p className="font-semibold text-ink">SSL Mode Notes</p>
                        <p className="mt-1">{activeSSLMode?.description}</p>
                      </div>

                      {endpointPasswordError ? <InlineAlert>{endpointPasswordError}</InlineAlert> : null}

                      {canWrite ? (
                        <button
                          type="submit"
                          disabled={submitting || !isFormSubmittable(form, drawerState.mode === 'edit') || Boolean(endpointPasswordError)}
                          className="inline-flex h-10 items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
                        >
                          {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : drawerState.mode === 'create' ? <Plus className="h-4 w-4" /> : <Pencil className="h-4 w-4" />}
                          {drawerState.mode === 'create' ? 'Create Connection' : 'Save Changes'}
                        </button>
                      ) : null}
                    </form>
                  </CardSection>
                  {drawerError ? <InlineAlert>{drawerError}</InlineAlert> : null}
                </div>
              )}
            </div>
          </div>
        </div>,
        document.body,
      ) : null}

      <ConfirmDialog
        open={pendingDeleteId !== null}
        title="Delete Database Connection"
        description={<DeleteConnectionDescription />}
        confirmLabel="Confirm Delete"
        tone="danger"
        loading={pendingDeleteId !== null && deletingId === pendingDeleteId}
        onCancel={() => setPendingDeleteId(null)}
        onConfirm={() => {
          if (pendingDeleteId !== null) {
            void handleDelete(pendingDeleteId)
          }
        }}
      />
    </div>
  )
}

function DeleteConnectionDescription() {
  return (
    <div className="space-y-3">
      <p>Delete this database connection? New tickets, SQL Editor, exports, metadata scan, masking, query access, scheduled reports, and rollback generation will no longer be able to use it.</p>
      <div>
        <p className="font-semibold text-ink">Cleaned dependencies</p>
        <p className="mt-1">Credentials, user and group DB scope, metadata snapshots, rollback artifacts, masking config, query access rules, scheduled reports, and metadata scan settings.</p>
      </div>
      <div>
        <p className="font-semibold text-ink">Retained history</p>
        <p className="mt-1">Tickets, audit logs, SQL Editor history, and saved queries remain visible for traceability.</p>
      </div>
    </div>
  )
}

function formatConnectionTestFailure(result: { error?: string; results?: Array<{ credential_role: string; ok: boolean; error?: string }> }) {
  const failures = (result.results ?? [])
    .filter((item) => !item.ok)
    .map((item) => `${item.credential_role}: ${item.error || 'Connection test failed'}`)
  if (failures.length > 0) {
    return failures.join('; ')
  }
  return result.error ?? 'Connection test failed'
}

function toPayload(form: ConnectionForm) {
  const databaseName = form.dbType === 'postgres' ? 'postgres' : null
  return {
    name: form.name.trim(),
    db_type: form.dbType,
    host: form.readonlyHost.trim(),
    port: Number(form.readonlyPort),
    readonly_host: form.readonlyHost.trim(),
    readonly_port: Number(form.readonlyPort),
    readwrite_host: form.readwriteHost.trim(),
    readwrite_port: Number(form.readwritePort),
    database_name: databaseName,
    username: form.readonlyUsername.trim(),
    password: form.readonlyPassword,
    ssl_mode: form.sslMode,
    credentials: [
      { credential_role: 'readonly', username: form.readonlyUsername.trim(), password: form.readonlyPassword },
      { credential_role: 'readwrite', username: form.readwriteUsername.trim(), password: form.readwritePassword },
      { credential_role: 'rollback', username: form.rollbackUsername.trim(), password: form.rollbackPassword },
    ].filter((item) => item.username || item.password),
  }
}

function normalizeDBType(value: string): ConnectionForm['dbType'] {
  if (value === 'postgres' || value === 'redis') {
    return value
  }
  return 'mysql'
}

function normalizeSSLMode(value: string): ConnectionForm['sslMode'] {
  if (value === 'disable' || value === 'require') {
    return value
  }
  return 'prefer'
}

function isFormSubmittable(form: ConnectionForm, isEdit = false) {
  if (!form.name.trim() || !form.readonlyHost.trim() || !form.readonlyPort.trim()) {
    return false
  }
  if (!form.readonlyUsername.trim() && form.dbType !== 'redis') {
    return false
  }
  if (!isEdit && !form.readonlyPassword) {
    return false
  }
  return Number(form.readonlyPort) > 0 && Number(form.readwritePort || form.readonlyPort) > 0
}

function getEndpointPasswordError(connection: DBConnection, form: ConnectionForm) {
  if (readonlyEndpointChanged(connection, form) && !form.readonlyPassword) {
    return 'Readonly endpoint changed. Re-enter the readonly password before saving.'
  }
  if (readwriteEndpointChanged(connection, form) && form.readwriteUsername.trim() && !form.readwritePassword) {
    return 'Readwrite endpoint changed. Re-enter the readwrite password before saving.'
  }
  if (readwriteEndpointChanged(connection, form) && !form.readwriteUsername.trim() && !form.readonlyPassword) {
    return 'Readwrite endpoint changed. Re-enter the connection password before saving.'
  }
  return ''
}

function readonlyEndpointChanged(connection: DBConnection, form: ConnectionForm) {
  return endpointChanged(
    connection.readonly_host || connection.host,
    connection.readonly_port || connection.port,
    form.readonlyHost,
    form.readonlyPort,
  )
}

function readwriteEndpointChanged(connection: DBConnection, form: ConnectionForm) {
  return endpointChanged(
    connection.readwrite_host || connection.readonly_host || connection.host,
    connection.readwrite_port || connection.readonly_port || connection.port,
    form.readwriteHost || form.readonlyHost,
    form.readwritePort || form.readonlyPort,
  )
}

function endpointChanged(currentHost: string, currentPort: number, nextHost: string, nextPort: string) {
  return currentHost.trim() !== nextHost.trim() || currentPort !== Number(nextPort)
}

function formatDBType(dbType: string) {
  if (dbType === 'postgres') {
    return 'PostgreSQL'
  }
  if (dbType === 'redis') {
    return 'Redis'
  }
  return 'MySQL'
}

function findCredentialUsername(connection: DBConnection, role: 'readonly' | 'readwrite' | 'rollback') {
  return connection.credentials?.find((item) => item.credential_role === role)?.username
}

function formatReadonlyConnectionTarget(connection: DBConnection) {
  return `${connection.readonly_host || connection.host}:${connection.readonly_port || connection.port}`
}

function formatReadwriteConnectionTarget(connection: DBConnection) {
  return `${connection.readwrite_host || connection.readonly_host || connection.host}:${connection.readwrite_port || connection.readonly_port || connection.port}`
}

function ExpandableEndpointValue({ value, className }: { value: string; className?: string }) {
  const [expanded, setExpanded] = useState(false)

  return (
    <button
      type="button"
      aria-expanded={expanded}
      onClick={() => setExpanded((current) => !current)}
      className={cn(
        'block max-w-[360px] bg-transparent p-0 text-left text-[12px] outline-none transition hover:text-ink focus-visible:rounded focus-visible:ring-2 focus-visible:ring-slate-300',
        expanded ? 'whitespace-normal break-all' : 'truncate whitespace-nowrap',
        className,
      )}
      title={value}
    >
      {value}
    </button>
  )
}

function CardSection({ title, icon, children }: { title: string; icon: ReactNode; children: ReactNode }) {
  return (
    <section className="rounded-xl border border-border bg-panel shadow-soft">
      <div className="border-b border-border/80 px-4 py-3">
        <div className="flex items-center gap-2">
          {icon}
          <p className="text-[13px] font-semibold text-ink">{title}</p>
        </div>
      </div>
      <div className="grid gap-4 px-4 py-4">{children}</div>
    </section>
  )
}
