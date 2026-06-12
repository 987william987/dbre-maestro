import { useEffect, useMemo, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Loader2, Pencil, Plus, ServerCog, Trash2, X } from 'lucide-react'
import { createDBConnection, deleteDBConnection, listDBConnections, patchDBConnection, testDBConnection } from '@/modules/db-connections/api'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import type { DBConnection } from '@/shared/types/dbConnection'
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { useToast } from '@/shared/ui/ToastContext'

type TestState = {
  ok: boolean
  message: string
} | null

type DrawerState =
  | { mode: 'create' }
  | { mode: 'edit'; connectionId: number }
  | null

type ConnectionForm = {
  name: string
  dbType: 'mysql' | 'postgres' | 'redis'
  host: string
  port: string
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
  host: '',
  port: '3306',
  readonlyUsername: '',
  readonlyPassword: '',
  readwriteUsername: '',
  readwritePassword: '',
  sslMode: 'prefer',
}

const SSL_MODE_OPTIONS = [
  { value: 'prefer', label: 'prefer', description: '若伺服器支援就走 SSL，不支援也可退回非加密。' },
  { value: 'disable', label: 'disable', description: '完全不使用 SSL，通常只適合內網或本機開發。' },
  { value: 'require', label: 'require', description: '強制使用 SSL，目標端不支援時連線會失敗。' },
] as const

const DB_TYPE_OPTIONS = [
  { value: 'mysql', label: 'MySQL / MariaDB' },
  { value: 'postgres', label: 'PostgreSQL' },
  { value: 'redis', label: 'Redis' },
] as const

export function DBConnectionsPage() {
  const { pushToast } = useToast()
  const [connections, setConnections] = useState<DBConnection[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [drawerState, setDrawerState] = useState<DrawerState>(null)
  const [drawerError, setDrawerError] = useState('')
  const [drawerLoading, setDrawerLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [form, setForm] = useState<ConnectionForm>(EMPTY_FORM)
  const [selectedConnection, setSelectedConnection] = useState<DBConnection | null>(null)
  const [testingId, setTestingId] = useState<number | null>(null)
  const [deletingId, setDeletingId] = useState<number | null>(null)
  const [pendingDeleteId, setPendingDeleteId] = useState<number | null>(null)
  const [testState, setTestState] = useState<Record<number, TestState>>({})

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
      setError(loadError instanceof ApiError ? loadError.message : '讀取資料庫連線失敗。')
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
      host: connection.host,
      port: String(connection.port),
      readonlyUsername: findCredentialUsername(connection, 'readonly') ?? connection.username ?? '',
      readonlyPassword: '',
      readwriteUsername: findCredentialUsername(connection, 'readwrite') ?? '',
      readwritePassword: '',
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
    if (!drawerState) {
      return
    }

    setSubmitting(true)
    setDrawerError('')

    try {
      if (drawerState.mode === 'create') {
        await createDBConnection(toPayload(form))
        pushToast('資料庫連線已建立', 'success')
      } else if (selectedConnection) {
        await patchDBConnection(selectedConnection.id, toPayload(form))
        pushToast('資料庫連線已更新', 'success')
      }
      await loadConnections()
      closeDrawer()
    } catch (submitError) {
      setDrawerError(submitError instanceof ApiError ? submitError.message : drawerState.mode === 'create' ? '建立資料庫連線失敗。' : '更新資料庫連線失敗。')
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
      setTestState((current) => ({
        ...current,
        [id]: result.ok
          ? { ok: true, message: '連線測試成功' }
          : { ok: false, message: result.error ?? '連線測試失敗' },
      }))
      if (result.ok) {
        pushToast(`${connectionName} 連線測試成功`, 'success', { placement: 'center' })
      } else {
        pushToast(`${connectionName} 連線測試失敗：${result.error ?? '連線測試失敗'}`, 'error', { placement: 'center', durationMs: 3600 })
      }
    } catch (testError) {
      const message = testError instanceof ApiError ? testError.message : '連線測試失敗'
      setTestState((current) => ({
        ...current,
        [id]: { ok: false, message },
      }))
      pushToast(`${connectionName} 連線測試失敗：${message}`, 'error', { placement: 'center', durationMs: 3600 })
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
      pushToast('資料庫連線已刪除', 'success')
      if (drawerState?.mode === 'edit' && drawerState.connectionId === id) {
        closeDrawer()
      }
    } catch (deleteError) {
      setError(deleteError instanceof ApiError ? deleteError.message : '刪除資料庫連線失敗。')
    } finally {
      setDeletingId(null)
      setPendingDeleteId(null)
    }
  }

  const sortedConnections = useMemo(() => {
    return [...connections].sort((left, right) => {
      const leftFailed = testState[left.id]?.ok === false ? 1 : 0
      const rightFailed = testState[right.id]?.ok === false ? 1 : 0
      if (leftFailed !== rightFailed) {
        return rightFailed - leftFailed
      }
      return right.created_at.localeCompare(left.created_at)
    })
  }, [connections, testState])

  return (
    <div className="flex h-full flex-col gap-3 p-3 sm:p-4">
      <section className="rounded-xl border border-border bg-panel-soft shadow-soft">
        <div className="px-4 py-3 sm:px-5">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
            <div className="max-w-3xl">
              <h2 className="text-[24px] font-bold tracking-[-0.03em] text-ink">DB Connections</h2>
              <p className="mt-2 max-w-[860px] text-[13px] leading-6 text-muted">
                Supports MySQL, PostgreSQL, and Redis. Leave `database` empty to let the SQL Editor browse available databases automatically.
              </p>
            </div>
            <button
              type="button"
              onClick={openCreateDrawer}
              className="inline-flex h-10 shrink-0 items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition hover:bg-slate-800"
            >
              <Plus className="h-4 w-4" />
              新增連線
            </button>
          </div>
        </div>
      </section>

      <section className="overflow-hidden rounded-xl border border-border bg-panel shadow-soft">
            {loading ? (
              <LoadingBlock message="載入連線中…" className="h-48 rounded-none border-0 bg-transparent" />
            ) : sortedConnections.length === 0 ? (
              <div className="m-4 flex h-48 items-center justify-center rounded-xl border border-dashed border-border bg-panel-soft text-sm text-muted">
                尚未建立任何資料庫連線。
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="min-w-full border-collapse">
                  <thead className="bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                    <tr>
                      <th className="px-3 py-3">Name</th>
                      <th className="px-3 py-3">Type</th>
                      <th className="px-3 py-3">Target</th>
                      <th className="px-3 py-3">
                        <div className="group relative inline-flex">
                          <span>SSL</span>
                          <div className="pointer-events-none absolute left-0 top-[calc(100%+8px)] z-10 hidden w-52 rounded-md border border-border bg-white px-3 py-2 text-[11px] font-medium normal-case tracking-normal text-muted shadow-soft group-hover:block">
                            支援就走 SSL，不支援可退回非加密。
                          </div>
                        </div>
                      </th>
                      <th className="px-3 py-3">Created</th>
                      <th className="px-3 py-3">Updated</th>
                      <th className="px-3 py-3">Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {sortedConnections.map((connection) => {
                      const result = testState[connection.id]
                      const isFailed = result?.ok === false
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
                            <p className="break-all font-mono text-[12px]">{connection.host}:{connection.port}</p>
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
                                測試
                              </button>
                              <button
                                type="button"
                                onClick={() => openEditDrawer(connection)}
                                className="inline-flex h-8 items-center justify-center gap-1 rounded-md border border-border bg-panel-soft px-2.5 text-[12px] font-semibold text-ink transition hover:bg-page"
                              >
                                <Pencil className="h-3.5 w-3.5" />
                                編輯
                              </button>
                              <button
                                type="button"
                                onClick={() => setPendingDeleteId(connection.id)}
                                disabled={deletingId === connection.id}
                                className="inline-flex h-8 items-center justify-center gap-1 rounded-md border border-danger/20 bg-red-50 px-2.5 text-[12px] font-semibold text-danger transition hover:bg-red-100 disabled:opacity-50"
                              >
                                {deletingId === connection.id ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
                                刪除
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
                  {drawerState.mode === 'create' ? '新增連線' : selectedConnection?.name ?? '編輯連線'}
                </h3>
              </div>
              <button
                type="button"
                onClick={closeDrawer}
                className="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-border bg-panel-soft text-muted transition hover:bg-page hover:text-ink"
                aria-label="關閉"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
              {drawerLoading ? (
                <LoadingBlock message="載入明細中…" className="min-h-[240px] rounded-xl border-border bg-panel" />
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
                          <select
                            value={form.dbType}
                            onChange={(event) => {
                              const nextType = normalizeDBType(event.target.value)
                              setForm((current) => ({
                                ...current,
                                dbType: nextType,
                                port: current.port === DEFAULT_PORT_BY_DB_TYPE[current.dbType] || current.port === '' ? DEFAULT_PORT_BY_DB_TYPE[nextType] : current.port,
                              }))
                            }}
                            className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                            disabled={submitting}
                          >
                            {DB_TYPE_OPTIONS.map((option) => (
                              <option key={option.value} value={option.value}>
                                {option.label}
                              </option>
                            ))}
                          </select>
                        </label>
                        <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                          SSL Mode
                          <select
                            value={form.sslMode}
                            onChange={(event) => setForm((current) => ({ ...current, sslMode: normalizeSSLMode(event.target.value) }))}
                            className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                            disabled={submitting}
                          >
                            {SSL_MODE_OPTIONS.map((option) => (
                              <option key={option.value} value={option.value}>
                                {option.label}
                              </option>
                            ))}
                          </select>
                        </label>
                      </div>

                      <div className="grid gap-3 sm:grid-cols-2">
                        <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                          Host
                          <input
                            value={form.host}
                            onChange={(event) => setForm((current) => ({ ...current, host: event.target.value }))}
                            className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                            disabled={submitting}
                          />
                        </label>
                        <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                          Port
                          <input
                            value={form.port}
                            onChange={(event) => setForm((current) => ({ ...current, port: event.target.value }))}
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
                            placeholder={drawerState.mode === 'edit' ? '留空代表不更新密碼' : ''}
                            className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                            disabled={submitting}
                          />
                        </label>
                      </div>

                      {form.dbType !== 'redis' ? (
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
                              placeholder={drawerState.mode === 'edit' ? '留空代表不更新密碼' : ''}
                              className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                              disabled={submitting}
                            />
                          </label>
                        </div>
                      ) : null}

                      <div className="rounded-lg border border-border bg-panel-soft px-3 py-3 text-[12px] text-muted">
                        <p className="font-semibold text-ink">Credential 策略</p>
                        <p className="mt-1">SQL Editor / DB Metadata 走 `readonly`；DDL / DML execute 走 `readwrite`。編輯時若密碼留空，表示保留既有密碼。</p>
                      </div>

                      <div className="rounded-lg border border-border bg-panel-soft px-3 py-3 text-[12px] text-muted">
                        <p className="font-semibold text-ink">SSL Mode 說明</p>
                        <p className="mt-1">{activeSSLMode?.description}</p>
                      </div>

                      <button
                        type="submit"
                        disabled={submitting || !isFormSubmittable(form, drawerState.mode === 'edit')}
                        className="inline-flex h-10 items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : drawerState.mode === 'create' ? <Plus className="h-4 w-4" /> : <Pencil className="h-4 w-4" />}
                        {drawerState.mode === 'create' ? '建立連線' : '儲存變更'}
                      </button>
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
        title="刪除資料庫連線"
        description="確認刪除這筆資料庫連線？刪除後工單、SQL Editor 與匯出流程都不能再引用它。"
        confirmLabel="確認刪除"
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

function toPayload(form: ConnectionForm) {
  const databaseName = form.dbType === 'postgres' ? 'postgres' : null
  return {
    name: form.name.trim(),
    db_type: form.dbType,
    host: form.host.trim(),
    port: Number(form.port),
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
  if (!form.name.trim() || !form.host.trim() || !form.port.trim()) {
    return false
  }
  if (!form.readonlyUsername.trim() && form.dbType !== 'redis') {
    return false
  }
  if (!isEdit && !form.readonlyPassword) {
    return false
  }
  return Number(form.port) > 0
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
