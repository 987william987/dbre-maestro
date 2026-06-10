import { useEffect, useState } from 'react'
import { Loader2, Plus, ShieldAlert, Trash2 } from 'lucide-react'
import { ApiError } from '@/shared/api/client'
import type { MaskingRule } from '@/shared/types/maskingRule'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { useToast } from '@/shared/ui/ToastContext'
import { listDBConnections } from '@/modules/db-connections/api'
import { createMaskingRule, deleteMaskingRule, listMaskingRules } from '@/modules/masking-rules/api'

type RuleForm = {
  dbConnectionId: string
  tableName: string
  columnName: string
  maskMode: 'full' | 'partial' | 'hash'
}

const EMPTY_FORM: RuleForm = {
  dbConnectionId: '',
  tableName: '',
  columnName: '',
  maskMode: 'full',
}

export function MaskingRulesPage() {
  const { pushToast } = useToast()
  const [rules, setRules] = useState<MaskingRule[]>([])
  const [connections, setConnections] = useState<Array<{ id: number; name: string }>>([])
  const [loading, setLoading] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [deletingId, setDeletingId] = useState<number | null>(null)
  const [error, setError] = useState('')
  const [form, setForm] = useState<RuleForm>(EMPTY_FORM)

  useEffect(() => {
    void Promise.all([loadRules(), loadConnections()])
  }, [])

  async function loadRules() {
    setLoading(true)
    setError('')
    try {
      const response = await listMaskingRules()
      setRules(response.rules)
    } catch (loadError) {
      setError(loadError instanceof ApiError ? loadError.message : '讀取 masking rules 失敗。')
    } finally {
      setLoading(false)
    }
  }

  async function loadConnections() {
    try {
      const response = await listDBConnections()
      setConnections(response.connections.map((item) => ({ id: item.id, name: item.name })))
    } catch {
      setConnections([])
    }
  }

  async function handleCreate(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSubmitting(true)
    setError('')
    try {
      await createMaskingRule({
        db_connection_id: form.dbConnectionId ? Number(form.dbConnectionId) : null,
        table_name: form.tableName,
        column_name: form.columnName,
        mask_mode: form.maskMode,
      })
      setForm(EMPTY_FORM)
      await loadRules()
      pushToast('Masking rule 已建立', 'success')
    } catch (submitError) {
      setError(submitError instanceof ApiError ? submitError.message : '建立 masking rule 失敗。')
    } finally {
      setSubmitting(false)
    }
  }

  async function handleDelete(id: number) {
    setDeletingId(id)
    setError('')
    try {
      await deleteMaskingRule(id)
      setRules((current) => current.filter((rule) => rule.id !== id))
      pushToast('Masking rule 已刪除', 'success')
    } catch (deleteError) {
      setError(deleteError instanceof ApiError ? deleteError.message : '刪除 masking rule 失敗。')
    } finally {
      setDeletingId(null)
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
                  Governance
                </span>
                <span>/</span>
                <span>Masking Rules</span>
              </div>
              <h2 className="mt-3 text-[24px] font-bold tracking-[-0.03em] text-ink">Masking Rules</h2>
              <p className="mt-2 text-[13px] leading-6 text-muted">
                管理查詢結果的欄位遮罩規則。首版先聚焦 list / create / delete，不在這裡展開 metadata 驅動的高級選表選欄。
              </p>
            </div>

            <div className="rounded-[14px] border border-border bg-white px-3 py-2.5 text-[12px] text-muted shadow-soft">
              <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">Rules</p>
              <p className="mt-1 text-[20px] font-bold tracking-tight text-ink">{rules.length}</p>
            </div>
          </div>
        </div>

        <form className="grid gap-3 px-4 py-3 sm:px-5 xl:grid-cols-[0.95fr_1.05fr]" onSubmit={handleCreate}>
          <div className="rounded-[18px] border border-white/85 bg-white/92 shadow-soft">
            <div className="border-b border-border/80 px-4 py-3">
              <div className="flex items-center gap-2">
                <Plus className="h-4 w-4 text-accent" />
                <p className="text-[13px] font-semibold text-ink">新增規則</p>
              </div>
            </div>

            <div className="grid gap-3 px-4 py-4">
              <select
                value={form.dbConnectionId}
                onChange={(event) => setForm((current) => ({ ...current, dbConnectionId: event.target.value }))}
                className="h-10 rounded-[12px] border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
              >
                <option value="">Global rule</option>
                {connections.map((connection) => (
                  <option key={connection.id} value={connection.id}>
                    {connection.name}
                  </option>
                ))}
              </select>
              <input
                value={form.tableName}
                onChange={(event) => setForm((current) => ({ ...current, tableName: event.target.value }))}
                className="h-10 rounded-[12px] border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                placeholder="table_name"
              />
              <input
                value={form.columnName}
                onChange={(event) => setForm((current) => ({ ...current, columnName: event.target.value }))}
                className="h-10 rounded-[12px] border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                placeholder="column_name"
              />
              <select
                value={form.maskMode}
                onChange={(event) => setForm((current) => ({ ...current, maskMode: event.target.value as RuleForm['maskMode'] }))}
                className="h-10 rounded-[12px] border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
              >
                <option value="full">full</option>
                <option value="partial">partial</option>
                <option value="hash">hash</option>
              </select>
              <button
                type="submit"
                disabled={submitting || !form.tableName.trim() || !form.columnName.trim()}
                className="inline-flex h-10 items-center justify-center gap-2 rounded-[12px] bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
                建立規則
              </button>
            </div>
          </div>

          <div className="rounded-[18px] border border-white/85 bg-white/92 shadow-soft">
            <div className="border-b border-border/80 px-4 py-3">
              <div className="flex items-center gap-2">
                <ShieldAlert className="h-4 w-4 text-accent" />
                <p className="text-[13px] font-semibold text-ink">已建立規則</p>
              </div>
            </div>

            <div className="overflow-x-auto">
              {loading ? (
                <LoadingBlock message="載入 masking rules 中…" className="m-4 min-h-[220px] rounded-[18px] border-white/80 bg-white/86" />
              ) : rules.length === 0 ? (
                <div className="flex h-[220px] items-center justify-center text-[12px] text-muted">尚未建立任何 masking rule。</div>
              ) : (
                <table className="min-w-full border-collapse">
                  <thead className="bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                    <tr>
                      <th className="px-3 py-3">Target</th>
                      <th className="px-3 py-3">Mode</th>
                      <th className="px-3 py-3">Scope</th>
                      <th className="px-3 py-3">Action</th>
                    </tr>
                  </thead>
                  <tbody>
                    {rules.map((rule) => (
                      <tr key={rule.id} className="border-t border-border text-[12px] text-ink hover:bg-slate-50/70">
                        <td className="px-3 py-3">
                          <p className="font-semibold">{rule.table_name}.{rule.column_name}</p>
                        </td>
                        <td className="px-3 py-3">
                          <span className="rounded-full border border-border bg-panel-soft px-2 py-0.5 text-[10px] font-semibold text-ink">
                            {rule.mask_mode}
                          </span>
                        </td>
                        <td className="px-3 py-3 text-muted">
                          {rule.db_connection_id ? `Connection #${rule.db_connection_id}` : 'Global'}
                        </td>
                        <td className="px-3 py-3">
                          <button
                            type="button"
                            onClick={() => void handleDelete(rule.id)}
                            disabled={deletingId === rule.id}
                            className="inline-flex h-8 items-center justify-center gap-1 rounded-[10px] border border-danger/20 bg-red-50 px-3 text-[12px] font-semibold text-danger transition hover:bg-red-100 disabled:opacity-50"
                          >
                            {deletingId === rule.id ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
                            Delete
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          </div>
        </form>
      </section>

      {error ? <InlineAlert>{error}</InlineAlert> : null}
    </div>
  )
}
