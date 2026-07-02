import { useEffect, useMemo, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Loader2, Pencil, Plus, ServerCog, Trash2, X } from 'lucide-react'
import { createDBConnection, deleteDBConnection, getDBConnectionBindings, listDBConnections, patchDBConnection, testDBConnection } from '@/modules/db-connections/api'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import type { DBConnection, DBConnectionBindings } from '@/shared/types/dbConnection'
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { PageIntro } from '@/shared/ui/PageIntro'
import { Pagination } from '@/shared/ui/Pagination'
import { useToast } from '@/shared/ui/ToastContext'

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
  const { pushToast } = useToast()
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
  const [bindings, setBindings] = useState<DBConnectionBindings | null>(null)
  const [bindingsLoading, setBindingsLoading] = useState(false)
  const [testingId, setTestingId] = useState<number | null>(null)
  const [deletingId, setDeletingId] = useState<number | null>(null)
  const [pendingDeleteId, setPendingDeleteId] = useState<number | null>(null)

  useEffect(() => {
    void loadConnections()
  }, [])

  const activeSSLMode = SSL_MODE_OPTIONS.find((option) => option.value === form.sslMode)
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
    setBindings(null)
    setBindingsLoading(false)
    setForm(EMPTY_FORM)
  }

  function openEditDrawer(connection: DBConnection) {
    setDrawerState({ mode: 'edit', connectionId: connection.id })
    setDrawerError('')
    setDrawerLoading(false)
    setSelectedConnection(connection)
    setBindings(null)
    setBindingsLoading(true)
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
      sslMode: normalizeSSLMode(connection.ssl_mode),
    })
    void loadBindings(connection.id)
  }

  function closeDrawer() {
    setDrawerState(null)
    setDrawerError('')
    setDrawerLoading(false)
    setSubmitting(false)
    setSelectedConnection(null)
    setBindings(null)
    setBindingsLoading(false)
    setForm(EMPTY_FORM)
  }

  async function loadBindings(connectionId: number) {
    try {
      const response = await getDBConnectionBindings(connectionId)
      setBindings(response)
    } catch {
      setBindings(null)
    } finally {
      setBindingsLoading(false)
    }
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
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
    setTestingId(id)
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
      setTestingId(null)
    }
  }

  async function handleDelete(id: number) {
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
    return [...connections].sort((left, right) => {
      const leftFailed = left.last_test_status === 'failed' ? 1 : 0
      const rightFailed = right.last_test_status === 'failed' ? 1 : 0
      if (leftFailed !== rightFailed) {
        return rightFailed - leftFailed
      }
      return right.created_at.localeCompare(left.created_at)
    })
  }, [connections])
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
      <PageIntro
        title="DB Connections"
        description="Supports MySQL, PostgreSQL, and Redis. Leave `database` empty to let the SQL Editor browse available databases automatically."
        actions={
          <button
            type="button"
            onClick={openCreateDrawer}
            className="inline-flex h-10 shrink-0 items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition hover:bg-slate-800"
          >
            <Plus className="h-4 w-4" />
            New Connection
          </button>
        }
      />

      <section className="overflow-hidden rounded-xl border border-border bg-panel shadow-soft">
            {loading ? (
              <LoadingBlock message="Loading connections..." className="h-48 rounded-none border-0 bg-transparent" />
            ) : sortedConnections.length === 0 ? (
              <div className="m-4 flex h-48 items-center justify-center rounded-xl border border-dashed border-border bg-panel-soft text-sm text-muted">
                No database connections yet.
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="min-w-full border-collapse">
                  <thead className="bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                    <tr>
                      <th className="px-3 py-3">Name</th>
                      <th className="px-3 py-3">Type</th>
                      <th className="px-3 py-3">Readonly Endpoint</th>
                      <th className="px-3 py-3">Readwrite Endpoint</th>
                      <th className="px-3 py-3">
                        <div className="group relative inline-flex">
                          <span>SSL</span>
                          <div className="pointer-events-none absolute left-0 top-[calc(100%+8px)] z-10 hidden w-52 rounded-md border border-border bg-white px-3 py-2 text-[11px] font-medium normal-case tracking-normal text-muted shadow-soft group-hover:block">
                            Use SSL when supported, otherwise fall back to an unencrypted connection.
                          </div>
                        </div>
                      </th>
                      <th className="px-3 py-3">Created</th>
                      <th className="px-3 py-3">Updated</th>
                      <th className="px-3 py-3">Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pagedConnections.map((connection) => {
                      const isFailed = connection.last_test_status === 'failed'
                      return (
                        <tr
                          key={connection.id}
                          className={`border-t text-sm transition-colors ${
                            isFailed
                              ? 'border-red-200 bg-red-50/75 text-danger hover:bg-red-50'
                              : 'border-border text-ink hover:bg-slate-50/70'
                          }`}
                        >
                          <td className="px-3 py-2.5 align-top">
                            <p className="whitespace-nowrap text-[13px] font-semibold">{connection.name}</p>
                          </td>
                          <td className={`px-3 py-2.5 align-top text-[12px] font-medium whitespace-nowrap ${isFailed ? 'text-danger' : 'text-ink'}`}>{formatDBType(connection.db_type)}</td>
                          <td className="px-3 py-2.5 align-top">
                            <p className="break-all font-mono text-[12px]">
                              {formatReadonlyConnectionTarget(connection)}
                            </p>
                          </td>
                          <td className="px-3 py-2.5 align-top">
                            <p className="break-all font-mono text-[12px]">
                              {formatReadwriteConnectionTarget(connection)}
                            </p>
                          </td>
                          <td className="px-3 py-2.5 align-top whitespace-nowrap">
                            <p className={`text-[12px] ${isFailed ? 'text-danger' : 'text-ink'}`}>{connection.ssl_mode}</p>
                          </td>
                          <td className={`px-3 py-2.5 align-top text-[12px] whitespace-nowrap ${isFailed ? 'text-danger/80' : 'text-muted'}`}>{formatDateTime(connection.created_at)}</td>
                          <td className={`px-3 py-2.5 align-top text-[12px] whitespace-nowrap ${isFailed ? 'text-danger/80' : 'text-muted'}`}>{formatDateTime(connection.updated_at)}</td>
                          <td className="px-3 py-2.5 align-top">
                            <div className="flex flex-nowrap items-center gap-1.5 whitespace-nowrap">
                              <button
                                type="button"
                                onClick={() => void handleTest(connection.id)}
                                disabled={testingId === connection.id}
                                className="inline-flex h-8 items-center justify-center gap-1 rounded-md border border-border bg-panel-soft px-2.5 text-[12px] font-semibold text-ink transition hover:bg-page disabled:opacity-50"
                              >
                                {testingId === connection.id ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
                                Test
                              </button>
                              <button
                                type="button"
                                onClick={() => openEditDrawer(connection)}
                                className="inline-flex h-8 items-center justify-center gap-1 rounded-md border border-border bg-panel-soft px-2.5 text-[12px] font-semibold text-ink transition hover:bg-page"
                              >
                                <Pencil className="h-3.5 w-3.5" />
                                Edit
                              </button>
                              <button
                                type="button"
                                onClick={() => setPendingDeleteId(connection.id)}
                                disabled={deletingId === connection.id}
                                className="inline-flex h-8 items-center justify-center gap-1 rounded-md border border-danger/20 bg-red-50 px-2.5 text-[12px] font-semibold text-danger transition hover:bg-red-100 disabled:opacity-50"
                              >
                                {deletingId === connection.id ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
                                Delete
                              </button>
                            </div>
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            )}
      </section>

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
                  {drawerState.mode === 'create' ? 'New Connection' : selectedConnection?.name ?? 'Edit Connection'}
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
                          disabled={submitting}
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
                            disabled={submitting}
                            options={DB_TYPE_OPTIONS.map((option) => ({ value: option.value, label: option.label }))}
                          />
                        </label>
                        <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                          SSL Mode
                          <DropdownSelect
                            ariaLabel="SSL Mode"
                            value={form.sslMode}
                            onChange={(value) => setForm((current) => ({ ...current, sslMode: normalizeSSLMode(value) }))}
                            disabled={submitting}
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
                            disabled={submitting}
                          />
                        </label>
                        <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                          Readonly Port
                          <input
                            value={form.readonlyPort}
                            onChange={(event) => setForm((current) => ({ ...current, readonlyPort: event.target.value }))}
                            className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                            disabled={submitting}
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
                            disabled={submitting}
                          />
                        </label>
                        <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                          Readwrite Port
                          <input
                            value={form.readwritePort}
                            onChange={(event) => setForm((current) => ({ ...current, readwritePort: event.target.value }))}
                            className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                            disabled={submitting}
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
                            disabled={submitting}
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
                            disabled={submitting}
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
                            disabled={submitting}
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
                            disabled={submitting}
                          />
                        </label>
                      </div>

                      <div className="rounded-lg border border-border bg-panel-soft px-3 py-3 text-[12px] text-muted">
                        <p className="font-semibold text-ink">Credential Policy</p>
                        <p className="mt-1">SQL Editor and DB Metadata use `readonly`; ticket execution uses `readwrite`. In edit mode, leaving a password blank keeps the current password only when the corresponding endpoint is unchanged.</p>
                      </div>

                      <div className="rounded-lg border border-border bg-panel-soft px-3 py-3 text-[12px] text-muted">
                        <p className="font-semibold text-ink">SSL Mode Notes</p>
                        <p className="mt-1">{activeSSLMode?.description}</p>
                      </div>

                      {endpointPasswordError ? <InlineAlert>{endpointPasswordError}</InlineAlert> : null}

                      <button
                        type="submit"
                        disabled={submitting || !isFormSubmittable(form, drawerState.mode === 'edit') || Boolean(endpointPasswordError)}
                        className="inline-flex h-10 items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : drawerState.mode === 'create' ? <Plus className="h-4 w-4" /> : <Pencil className="h-4 w-4" />}
                        {drawerState.mode === 'create' ? 'Create Connection' : 'Save Changes'}
                      </button>
                    </form>
                  </CardSection>
                  {drawerState.mode === 'edit' ? (
                    <CardSection title="Resource Bindings" icon={<ServerCog className="h-4 w-4 text-accent" />}>
                      {bindingsLoading ? (
                        <LoadingBlock message="Loading resource bindings..." className="min-h-[160px] rounded-xl border-border bg-panel" />
                      ) : (
                        <div className="grid gap-4 sm:grid-cols-3">
                          <BindingList
                            title="Direct Users"
                            items={bindings?.direct_users.map((user) => user.username) ?? []}
                            emptyLabel="No direct user bindings."
                          />
                          <BindingList
                            title="Auth Groups"
                            items={bindings?.auth_groups.map((group) => group.name || group.group_key) ?? []}
                            emptyLabel="No auth group bindings."
                          />
                          <BindingList
                            title="Effective Users"
                            items={bindings?.effective_users.map((user) => user.username) ?? []}
                            emptyLabel="No effective users."
                          />
                        </div>
                      )}
                    </CardSection>
                  ) : null}
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
        description="Delete this database connection? Tickets, SQL Editor, and export flows will no longer be able to reference it."
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

function findCredentialUsername(connection: DBConnection, role: 'readonly' | 'readwrite') {
  return connection.credentials?.find((item) => item.credential_role === role)?.username
}

function formatReadonlyConnectionTarget(connection: DBConnection) {
  return `${connection.readonly_host || connection.host}:${connection.readonly_port || connection.port}`
}

function formatReadwriteConnectionTarget(connection: DBConnection) {
  return `${connection.readwrite_host || connection.readonly_host || connection.host}:${connection.readwrite_port || connection.readonly_port || connection.port}`
}

function BindingList({ title, items, emptyLabel }: { title: string; items: string[]; emptyLabel: string }) {
  return (
    <div className="grid gap-2">
      <p className="text-[12px] font-semibold text-ink">{title}</p>
      {items.length === 0 ? (
        <p className="text-[12px] text-muted">{emptyLabel}</p>
      ) : (
        <div className="flex flex-wrap gap-2">
          {items.map((item) => (
            <span key={item} className="inline-flex rounded-full border border-border bg-panel-soft px-2.5 py-1 text-[11px] font-medium text-ink">
              {item}
            </span>
          ))}
        </div>
      )}
    </div>
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
