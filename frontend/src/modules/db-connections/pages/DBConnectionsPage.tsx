import { useEffect, useState } from 'react'
import { CheckCircle2, DatabaseZap, Loader2, PlugZap, Plus, Trash2, XCircle } from 'lucide-react'
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

  useEffect(() => {
    void loadConnections()
  }, [])

  const healthyConnections = Object.values(testState).filter((result) => result?.ok).length

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
    <div className="flex h-full flex-col gap-3 p-3 sm:p-4">
      <section className="rounded-[22px] border border-white/85 bg-[rgba(248,250,252,0.82)] shadow-soft">
        <div className="border-b border-border/80 px-4 py-3 sm:px-5">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
            <div className="max-w-3xl">
              <div className="flex flex-wrap items-center gap-2 text-[11px] font-semibold text-muted">
                <span className="rounded-full border border-border bg-white px-2.5 py-1 text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                  Connections
                </span>
                <span>/</span>
                <span>Infrastructure Registry</span>
              </div>
              <h2 className="mt-3 text-[24px] font-bold tracking-[-0.03em] text-ink">資料庫連線管理</h2>
              <p className="mt-2 max-w-[860px] text-[13px] leading-6 text-muted">
                維護工單與匯出流程會引用的資料庫資產，讓連線資訊、健康狀態與建立動作集中在同一個控制台。
              </p>
            </div>

            <div className="grid min-w-[250px] gap-2 text-[12px] text-muted sm:grid-cols-3 lg:min-w-[360px]">
              <div className="rounded-[14px] border border-border bg-white px-3 py-2.5 shadow-soft">
                <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">Registered</p>
                <p className="mt-1 text-[20px] font-bold tracking-tight text-ink">{connections.length}</p>
              </div>
              <div className="rounded-[14px] border border-border bg-white px-3 py-2.5 shadow-soft">
                <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">Verified</p>
                <p className="mt-1 text-[20px] font-bold tracking-tight text-ink">{healthyConnections}</p>
              </div>
              <div className="rounded-[14px] border border-border bg-white px-3 py-2.5 shadow-soft">
                <div className="flex items-center justify-between">
                  <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">Readiness</p>
                  <DatabaseZap className="h-3.5 w-3.5 text-muted" />
                </div>
                <p className="mt-1 text-[12px] font-semibold text-ink">
                  {form.name && form.host && form.port && form.username && form.password ? '可建立' : '待補必要欄位'}
                </p>
              </div>
            </div>
          </div>
        </div>

        <div className="grid gap-3 px-4 py-3 sm:px-5 xl:grid-cols-[0.94fr_1.06fr]">
          <section className="rounded-[18px] border border-white/85 bg-white/92 shadow-soft">
            <div className="border-b border-border/80 px-4 py-3">
              <div className="flex items-center gap-2">
                <Plus className="h-4 w-4 text-accent" />
                <p className="text-[13px] font-semibold text-ink">新增連線</p>
              </div>
              <p className="mt-1 text-[12px] text-muted">輸入必要連線資訊後即可建立，可選擇是否附帶資料庫名稱與 SSL 模式。</p>
            </div>

            <form className="grid gap-3 px-4 py-4" onSubmit={handleCreate}>
              <input
                value={form.name}
                onChange={(event) => setForm((current) => ({ ...current, name: event.target.value }))}
                className="h-10 rounded-[12px] border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                placeholder="名稱"
                disabled={submitting}
              />
              <div className="grid gap-3 sm:grid-cols-2">
                <input
                  value={form.host}
                  onChange={(event) => setForm((current) => ({ ...current, host: event.target.value }))}
                  className="h-10 rounded-[12px] border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                  placeholder="Host"
                  disabled={submitting}
                />
                <input
                  value={form.port}
                  onChange={(event) => setForm((current) => ({ ...current, port: event.target.value }))}
                  className="h-10 rounded-[12px] border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                  placeholder="Port"
                  disabled={submitting}
                />
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                <input
                  value={form.username}
                  onChange={(event) => setForm((current) => ({ ...current, username: event.target.value }))}
                  className="h-10 rounded-[12px] border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                  placeholder="Username"
                  disabled={submitting}
                />
                <input
                  value={form.password}
                  type="password"
                  onChange={(event) => setForm((current) => ({ ...current, password: event.target.value }))}
                  className="h-10 rounded-[12px] border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                  placeholder="Password"
                  disabled={submitting}
                />
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                <input
                  value={form.databaseName}
                  onChange={(event) => setForm((current) => ({ ...current, databaseName: event.target.value }))}
                  className="h-10 rounded-[12px] border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                  placeholder="Database Name"
                  disabled={submitting}
                />
                <select
                  value={form.sslMode}
                  onChange={(event) => setForm((current) => ({ ...current, sslMode: event.target.value }))}
                  className="h-10 rounded-[12px] border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
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
                className="inline-flex h-10 items-center justify-center gap-2 rounded-[12px] bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
                新增連線
              </button>
            </form>
          </section>

          <section className="rounded-[18px] border border-white/85 bg-white/92 shadow-soft">
            <div className="border-b border-border/80 px-4 py-3">
              <div className="flex items-center gap-2">
                <PlugZap className="h-4 w-4 text-accent" />
                <p className="text-[13px] font-semibold text-ink">已註冊連線</p>
              </div>
              <p className="mt-1 text-[12px] text-muted">測試連線、檢查目標位置，必要時刪除不再使用的資產。</p>
            </div>

            {loading ? (
              <LoadingBlock message="載入連線中…" className="h-48 rounded-none border-0 bg-transparent" />
            ) : connections.length === 0 ? (
              <div className="m-4 flex h-48 items-center justify-center rounded-[18px] border border-dashed border-border bg-panel-soft text-sm text-muted">
                尚未建立任何資料庫連線。
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="min-w-full border-collapse">
                  <thead className="bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                    <tr>
                      <th className="px-3 py-3">Name</th>
                      <th className="px-3 py-3">Target</th>
                      <th className="px-3 py-3">Created</th>
                      <th className="px-3 py-3">Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {connections.map((connection) => (
                      <tr key={connection.id} className="border-t border-border text-sm text-ink transition-colors hover:bg-slate-50/70">
                        <td className="px-3 py-3 align-top">
                          <p className="text-[13px] font-semibold">{connection.name}</p>
                          <p className="mt-1 text-[12px] text-muted">{connection.db_type} / {connection.ssl_mode}</p>
                        </td>
                        <td className="px-3 py-3 align-top">
                          <p className="font-mono text-[12px]">{connection.host}:{connection.port}</p>
                          <p className="mt-1 text-[12px] text-muted">{connection.database_name || '未指定 DB 名稱'}</p>
                        </td>
                        <td className="px-3 py-3 align-top text-[12px] text-muted">{formatDateTime(connection.created_at)}</td>
                        <td className="px-3 py-3 align-top">
                          <div className="flex flex-col gap-2">
                            <div className="flex gap-2">
                              <button
                                type="button"
                                onClick={() => void handleTest(connection.id)}
                                disabled={testingId === connection.id}
                                className="inline-flex h-8 items-center justify-center gap-1 rounded-[10px] border border-border bg-panel-soft px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:opacity-50"
                              >
                                {testingId === connection.id ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
                                Test
                              </button>
                              <button
                                type="button"
                                onClick={() => setPendingDeleteId(connection.id)}
                                disabled={deletingId === connection.id}
                                className="inline-flex h-8 items-center justify-center gap-1 rounded-[10px] border border-danger/20 bg-red-50 px-3 text-[12px] font-semibold text-danger transition hover:bg-red-100 disabled:opacity-50"
                              >
                                {deletingId === connection.id ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
                                Delete
                              </button>
                            </div>
                            {testState[connection.id] ? (
                              <p className={`inline-flex items-center gap-1 text-[12px] ${testState[connection.id]?.ok ? 'text-success' : 'text-danger'}`}>
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
      </section>

      {error ? <InlineAlert>{error}</InlineAlert> : null}

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
