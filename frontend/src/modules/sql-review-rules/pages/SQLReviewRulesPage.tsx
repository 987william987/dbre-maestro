import { useEffect, useState } from 'react'
import { Loader2, ShieldCheck } from 'lucide-react'
import { ApiError } from '@/shared/api/client'
import type { SQLReviewRule } from '@/shared/types/sqlReviewRule'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { useToast } from '@/shared/ui/ToastContext'
import { listSQLReviewRules, patchSQLReviewRule } from '@/modules/sql-review-rules/api'

type DraftMap = Record<string, { enabled: boolean; threshold: string }>

export function SQLReviewRulesPage() {
  const { pushToast } = useToast()
  const [rules, setRules] = useState<SQLReviewRule[]>([])
  const [drafts, setDrafts] = useState<DraftMap>({})
  const [loading, setLoading] = useState(true)
  const [savingRuleName, setSavingRuleName] = useState<string | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    void loadRules()
  }, [])

  async function loadRules() {
    setLoading(true)
    setError('')
    try {
      const response = await listSQLReviewRules()
      setRules(response.rules)
      setDrafts(
        Object.fromEntries(
          response.rules.map((rule) => [
            rule.rule_name,
            {
              enabled: rule.enabled,
              threshold: rule.threshold == null ? '' : String(rule.threshold),
            },
          ]),
        ),
      )
    } catch (loadError) {
      setError(loadError instanceof ApiError ? loadError.message : '讀取 SQL review rules 失敗。')
    } finally {
      setLoading(false)
    }
  }

  async function handleSave(rule: SQLReviewRule) {
    const draft = drafts[rule.rule_name]
    if (!draft) {
      return
    }

    setSavingRuleName(rule.rule_name)
    setError('')
    try {
      const payload: { enabled?: boolean; threshold?: number | null } = {}
      if (draft.enabled !== rule.enabled) {
        payload.enabled = draft.enabled
      }
      const nextThreshold = draft.threshold.trim() === '' ? null : Number(draft.threshold)
      if ((rule.threshold ?? null) !== nextThreshold) {
        payload.threshold = nextThreshold
      }

      if (Object.keys(payload).length === 0) {
        pushToast('沒有需要更新的內容', 'info')
        return
      }

      const updated = await patchSQLReviewRule(rule.rule_name, payload)
      setRules((current) => current.map((item) => (item.rule_name === rule.rule_name ? updated : item)))
      setDrafts((current) => ({
        ...current,
        [rule.rule_name]: {
          enabled: updated.enabled,
          threshold: updated.threshold == null ? '' : String(updated.threshold),
        },
      }))
      pushToast('SQL review rule 已更新', 'success')
    } catch (saveError) {
      setError(saveError instanceof ApiError ? saveError.message : '更新 SQL review rule 失敗。')
    } finally {
      setSavingRuleName(null)
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
                <span>SQL Review Rules</span>
              </div>
              <h2 className="mt-3 text-[24px] font-bold tracking-[-0.03em] text-ink">SQL Review Rules</h2>
              <p className="mt-2 text-[13px] leading-6 text-muted">
                管理 SQL review 規則的啟用狀態與 threshold。這一頁以設定清單 + inline patch 為主，不拆 detail 頁。
              </p>
            </div>

            <div className="rounded-[14px] border border-border bg-white px-3 py-2.5 text-[12px] text-muted shadow-soft">
              <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">Rules</p>
              <p className="mt-1 text-[20px] font-bold tracking-tight text-ink">{rules.length}</p>
            </div>
          </div>
        </div>

        <div className="overflow-x-auto rounded-[18px] border border-white/85 bg-white/92 shadow-soft sm:m-4">
          {loading ? (
            <LoadingBlock message="載入 SQL review rules 中…" className="m-4 min-h-[220px] rounded-[18px] border-white/80 bg-white/86" />
          ) : rules.length === 0 ? (
            <div className="flex h-[220px] items-center justify-center text-[12px] text-muted">目前沒有 SQL review rules。</div>
          ) : (
            <table className="min-w-full border-collapse">
              <thead className="bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                <tr>
                  <th className="px-3 py-3">Rule</th>
                  <th className="px-3 py-3">Description</th>
                  <th className="px-3 py-3">Enabled</th>
                  <th className="px-3 py-3">Threshold</th>
                  <th className="px-3 py-3">Action</th>
                </tr>
              </thead>
              <tbody>
                {rules.map((rule) => {
                  const draft = drafts[rule.rule_name]
                  return (
                    <tr key={rule.rule_name} className="border-t border-border text-[12px] text-ink hover:bg-slate-50/70">
                      <td className="px-3 py-3 font-mono">{rule.rule_name}</td>
                      <td className="px-3 py-3 text-muted">{rule.description}</td>
                      <td className="px-3 py-3">
                        <label className="inline-flex items-center gap-2">
                          <input
                            type="checkbox"
                            checked={draft?.enabled ?? false}
                            onChange={(event) =>
                              setDrafts((current) => ({
                                ...current,
                                [rule.rule_name]: {
                                  ...current[rule.rule_name],
                                  enabled: event.target.checked,
                                },
                              }))
                            }
                          />
                          <span>{draft?.enabled ? 'Enabled' : 'Disabled'}</span>
                        </label>
                      </td>
                      <td className="px-3 py-3">
                        <input
                          value={draft?.threshold ?? ''}
                          onChange={(event) =>
                            setDrafts((current) => ({
                              ...current,
                              [rule.rule_name]: {
                                ...current[rule.rule_name],
                                threshold: event.target.value,
                              },
                            }))
                          }
                          className="h-9 w-[120px] rounded-[10px] border border-border bg-white px-3 text-[12px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                          placeholder="threshold"
                        />
                      </td>
                      <td className="px-3 py-3">
                        <button
                          type="button"
                          onClick={() => void handleSave(rule)}
                          disabled={savingRuleName === rule.rule_name}
                          className="inline-flex h-8 items-center justify-center gap-1 rounded-[10px] border border-border bg-panel-soft px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:opacity-50"
                        >
                          {savingRuleName === rule.rule_name ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <ShieldCheck className="h-3.5 w-3.5" />}
                          Save
                        </button>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}
        </div>
      </section>

      {error ? <InlineAlert>{error}</InlineAlert> : null}
    </div>
  )
}
