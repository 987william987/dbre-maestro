import { useEffect, useState } from 'react'
import { CheckCircle2, Loader2, PlugZap, Plus, Trash2, XCircle } from 'lucide-react'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import type { DBConnection } from '@/shared/types/dbConnection'
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { useToast } from '@/shared/ui/ToastContext'
import { createDBConnection, deleteDBConnection, listDBConnections, testDBConnection } from '@/modules/db-connections/api'

type TestState = {
  ok: boolean
  message: string
} | null

export function DBConnectionsPage() {
  const { pushToast } = useToast()
  const [connections, setConnections] = useState<DBConnection[]>([])
  const [loading, setLoading] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [testingId, setTestingId] = useState<number | null>(null)
  const [deletingId, setDeletingId] = useState<number | null>(null)
  const [error, setError] = useState('')
  const [pendingDeleteId, setPendingDeleteId] = useState<number | null>(null)
  const [testState, setTestState] = useState<Record<number, TestState>>({})
  const [form, setForm] = useState({
    name: '',
    dbType: 'mysql',
    host: '',
    port: '3306',
    databaseName: '',
    username: '',
    password: '',
    sslMode: 'prefer',
  })

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

  useEffect(() => {
    void loadConnections()
  }, [])

  async function handleCreate(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSubmitting(true)
    setError('')

    try {
      await createDBConnection({
        name: form.name,
        db_type: form.dbType,
        host: form.host,
        port: Number(form.port),
        database_name: form.databaseName.trim() || null,
        username: form.username,
        password: form.password,
        ssl_mode: form.sslMode,
      })

      setForm({
        name: '',
        dbType: 'mysql',
        host: '',
        port: '3306',
        databaseName: '',
        username: '',
        password: '',
        sslMode: 'prefer',
      })

      await loadConnections()
      pushToast('資料庫連線已建立', 'success')
    } catch (submitError) {
      setError(submitError instanceof ApiError ? submitError.message : '建立資料庫連線失敗。')
    } finally {
      setSubmitting(false)
    }
  }

  async function handleTest(id: number) {
    setTestingId(id)
    setError('')
    try {
      const result = await testDBConnection(id)
      setTestState((current) => ({
        ...current,
        [id]: result.ok
          ? { ok: true, message: '連線測試成功' }
          : { ok: false, message: result.error ?? '連線測試失敗' },
      }))
      if (result.ok) {
        pushToast('連線測試成功', 'success')
      }
    } catch (testError) {
      setTestState((current) => ({
        ...current,
        [id]: { ok: false, message: testError instanceof ApiError ? testError.message : '連線測試失敗' },
      }))
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
    } catch (deleteError) {
      setError(deleteError instanceof ApiError ? deleteError.message : '刪除資料庫連線失敗。')
    } finally {
      setDeletingId(null)
      setPendingDeleteId(null)
    }
  }

  return (
    <div className="flex h-full flex-col gap-6 p-5 sm:p-6">
      <div className="border-b border-border pb-5">
        <p className="text-[11px] font-bold uppercase tracking-[0.2em] text-faint">DB Connections</p>
        <h2 className="mt-2 font-display text-2xl font-black tracking-tight text-ink">資料庫連線管理</h2>
        <p className="mt-1 text-sm text-muted">這裡管理目標資料庫資產，供工單與匯出功能使用。密碼不會在前端回填。</p>
      </div>

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      <div className="grid gap-6 xl:grid-cols-[0.95fr_1.05fr]">
        <section className="rounded-card border border-border bg-panel p-5">
          <div className="flex items-center gap-2">
            <Plus className="h-4 w-4 text-accent" />
            <p className="text-sm font-semibold text-ink">新增連線</p>
          </div>

          <form className="mt-4 grid gap-4" onSubmit={handleCreate}>
            <input
              value={form.name}
              onChange={(event) => setForm((current) => ({ ...current, name: event.target.value }))}
              className="h-10 rounded-control border border-border bg-panel-soft px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
              placeholder="名稱"
              disabled={submitting}
            />
            <div className="grid gap-4 sm:grid-cols-2">
              <input
                value={form.host}
                onChange={(event) => setForm((current) => ({ ...current, host: event.target.value }))}
                className="h-10 rounded-control border border-border bg-panel-soft px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                placeholder="Host"
                disabled={submitting}
              />
              <input
                value={form.port}
                onChange={(event) => setForm((current) => ({ ...current, port: event.target.value }))}
                className="h-10 rounded-control border border-border bg-panel-soft px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                placeholder="Port"
                disabled={submitting}
              />
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <input
                value={form.username}
                onChange={(event) => setForm((current) => ({ ...current, username: event.target.value }))}
                className="h-10 rounded-control border border-border bg-panel-soft px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                placeholder="Username"
                disabled={submitting}
              />
              <input
                value={form.password}
                type="password"
                onChange={(event) => setForm((current) => ({ ...current, password: event.target.value }))}
                className="h-10 rounded-control border border-border bg-panel-soft px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                placeholder="Password"
                disabled={submitting}
              />
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <input
                value={form.databaseName}
                onChange={(event) => setForm((current) => ({ ...current, databaseName: event.target.value }))}
                className="h-10 rounded-control border border-border bg-panel-soft px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                placeholder="Database Name"
                disabled={submitting}
              />
              <select
                value={form.sslMode}
                onChange={(event) => setForm((current) => ({ ...current, sslMode: event.target.value }))}
                className="h-10 rounded-control border border-border bg-panel-soft px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                disabled={submitting}
              >
                <option value="prefer">prefer</option>
                <option value="disable">disable</option>
                <option value="require">require</option>
              </select>
            </div>

            <button
              type="submit"
              disabled={submitting || !form.name || !form.host || !form.port || !form.username || !form.password}
              className="inline-flex h-10 items-center justify-center gap-2 rounded-control bg-brand px-4 text-sm font-bold text-white transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
              新增連線
            </button>
          </form>
        </section>

        <section className="rounded-card border border-border bg-panel p-5">
          <div className="flex items-center gap-2">
            <PlugZap className="h-4 w-4 text-accent" />
            <p className="text-sm font-semibold text-ink">已註冊連線</p>
          </div>

          {loading ? (
            <LoadingBlock message="載入連線中…" className="h-48 rounded-none border-0" />
          ) : connections.length === 0 ? (
            <div className="flex h-48 items-center justify-center rounded-card border border-dashed border-border bg-panel-soft text-sm text-muted">
              尚未建立任何資料庫連線。
            </div>
          ) : (
            <div className="mt-4 overflow-x-auto">
              <table className="min-w-full border-collapse">
                <thead className="bg-panel-soft text-left text-[11px] font-bold uppercase tracking-[0.16em] text-faint">
                  <tr>
                    <th className="px-3 py-3">Name</th>
                    <th className="px-3 py-3">Target</th>
                    <th className="px-3 py-3">Created</th>
                    <th className="px-3 py-3">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {connections.map((connection) => (
                    <tr key={connection.id} className="border-t border-border text-sm text-ink">
                      <td className="px-3 py-3 align-top">
                        <p className="font-semibold">{connection.name}</p>
                        <p className="mt-1 text-xs text-muted">{connection.db_type} / {connection.ssl_mode}</p>
                      </td>
                      <td className="px-3 py-3 align-top">
                        <p className="font-mono text-xs">{connection.host}:{connection.port}</p>
                        <p className="mt-1 text-xs text-muted">{connection.database_name || '未指定 DB 名稱'}</p>
                      </td>
                      <td className="px-3 py-3 align-top text-xs text-muted">{formatDateTime(connection.created_at)}</td>
                      <td className="px-3 py-3 align-top">
                        <div className="flex flex-col gap-2">
                          <div className="flex gap-2">
                            <button
                              type="button"
                              onClick={() => void handleTest(connection.id)}
                              disabled={testingId === connection.id}
                              className="inline-flex h-8 items-center justify-center gap-1 rounded-control border border-border bg-panel-soft px-3 text-xs font-semibold text-ink transition hover:bg-page disabled:opacity-50"
                            >
                              {testingId === connection.id ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
                              Test
                            </button>
                            <button
                              type="button"
                              onClick={() => setPendingDeleteId(connection.id)}
                              disabled={deletingId === connection.id}
                              className="inline-flex h-8 items-center justify-center gap-1 rounded-control border border-danger/20 bg-red-50 px-3 text-xs font-semibold text-danger transition hover:bg-red-100 disabled:opacity-50"
                            >
                              {deletingId === connection.id ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
                              Delete
                            </button>
                          </div>
                          {testState[connection.id] ? (
                            <p className={`inline-flex items-center gap-1 text-xs ${testState[connection.id]?.ok ? 'text-success' : 'text-danger'}`}>
                              {testState[connection.id]?.ok ? <CheckCircle2 className="h-3.5 w-3.5" /> : <XCircle className="h-3.5 w-3.5" />}
                              {testState[connection.id]?.message}
                            </p>
                          ) : null}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      </div>

      <ConfirmDialog
        open={pendingDeleteId !== null}
        title="刪除資料庫連線"
        description="確認刪除這筆資料庫連線？刪除後工單與匯出就不能再引用它。"
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
