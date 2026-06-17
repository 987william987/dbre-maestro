import { useEffect, useMemo, useState } from 'react'
import { Loader2, ShieldCheck } from 'lucide-react'
import { useParams } from 'react-router-dom'
import { ApiError } from '@/shared/api/client'
import { useAuth } from '@/shared/auth/AuthContext'
import type { SQLReviewRule } from '@/shared/types/sqlReviewRule'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { PageIntro } from '@/shared/ui/PageIntro'
import { Pagination } from '@/shared/ui/Pagination'
import { PageTabs } from '@/shared/ui/PageTabs'
import { Switch } from '@/shared/ui/Switch'
import { useToast } from '@/shared/ui/ToastContext'
import { listSQLReviewRules, patchSQLReviewRule } from '@/modules/sql-review-rules/api'

type DraftMap = Record<string, { enabled: boolean; threshold: string }>

const RULE_METADATA: Record<string, { description: string; thresholdEditable: boolean }> = {
  ddl_no_comment: {
    description: 'Require CREATE TABLE statements to include a table comment.',
    thresholdEditable: false,
  },
  dml_no_where: {
    description: 'Require UPDATE and DELETE statements to include a WHERE clause.',
    thresholdEditable: false,
  },
  full_table_scan: {
    description: 'Block queries when EXPLAIN detects a full table scan.',
    thresholdEditable: false,
  },
  high_row_count: {
    description: 'Block queries when EXPLAIN estimated rows exceed the configured threshold.',
    thresholdEditable: true,
  },
  require_utf8mb4: {
    description: 'Require CREATE and ALTER TABLE statements to use utf8mb4.',
    thresholdEditable: false,
  },
}

const PAGE_SIZE = 20
const ENGINE_TABS = [
  { key: 'mysql', label: 'MySQL' },
  { key: 'postgresql', label: 'PostgreSQL' },
  { key: 'redis', label: 'Redis' },
] as const

export function SQLReviewRulesPage() {
  const { engine } = useParams()
  const { user } = useAuth()
  const { pushToast } = useToast()
  const [rules, setRules] = useState<SQLReviewRule[]>([])
  const [offset, setOffset] = useState(0)
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
      setError(loadError instanceof ApiError ? loadError.message : 'Failed to load SQL review rules.')
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
      const thresholdEditable = isThresholdEditable(rule.rule_name)
      if (draft.enabled !== rule.enabled) {
        payload.enabled = draft.enabled
      }
      if (thresholdEditable) {
        const nextThreshold = draft.threshold.trim() === '' ? null : Number(draft.threshold)
        if ((rule.threshold ?? null) !== nextThreshold) {
          payload.threshold = nextThreshold
        }
      }

      if (Object.keys(payload).length === 0) {
        pushToast('No changes to update.', 'info')
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
      pushToast('SQL review rule updated.', 'success')
    } catch (saveError) {
      setError(saveError instanceof ApiError ? saveError.message : 'Failed to update the SQL review rule.')
    } finally {
      setSavingRuleName(null)
    }
  }

  const pagedRules = useMemo(() => rules.slice(offset, offset + PAGE_SIZE), [offset, rules])
  const currentEngine = engine === 'postgresql' || engine === 'redis' ? engine : 'mysql'
  const canWrite = user?.permissions.includes('sql_review.write') ?? false

  useEffect(() => {
    if (offset > 0 && offset >= rules.length) {
      setOffset(Math.max(0, Math.floor((Math.max(rules.length - 1, 0)) / PAGE_SIZE) * PAGE_SIZE))
    }
  }, [offset, rules.length])

  return (
    <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
      <PageIntro
        title="SQL Review Rules"
        description="Manage parser and validation rules by engine. The current MySQL page contains the implemented review rules; PostgreSQL and Redis are reserved for follow-up expansion."
      />

      <PageTabs
        items={ENGINE_TABS.map((tab) => ({
          key: tab.key,
          label: tab.label,
          to: `/sql-review-rules/${tab.key}`,
        }))}
      />

      {currentEngine !== 'mysql' ? (
        <div className="flex min-h-[220px] items-center justify-center rounded-xl border border-border bg-panel shadow-soft text-[13px] text-muted">
          {currentEngine === 'postgresql'
            ? 'PostgreSQL review rules will be added in a later phase.'
            : 'Redis review rules will be added after Redis ticket rules are finalized.'}
        </div>
      ) : (
      <div className="overflow-hidden overflow-x-auto rounded-xl border border-border bg-panel shadow-soft">
          {loading ? (
            <LoadingBlock message="Loading SQL review rules..." className="m-4 min-h-[220px] rounded-xl border-border bg-panel" />
          ) : rules.length === 0 ? (
            <div className="flex h-[220px] items-center justify-center text-[12px] text-muted">No SQL review rules found.</div>
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
                {pagedRules.map((rule) => {
                  const draft = drafts[rule.rule_name]
                  return (
                    <tr key={rule.rule_name} className="border-t border-border text-[12px] text-ink hover:bg-slate-50/70">
                      <td className="px-3 py-3 font-mono">{rule.rule_name}</td>
                      <td className="px-3 py-3 text-muted">{getRuleDescription(rule)}</td>
                      <td className="px-3 py-3">
                        <div className="inline-flex items-center gap-3">
                          <Switch
                            ariaLabel={`${rule.rule_name} enabled`}
                            checked={draft?.enabled ?? false}
                            disabled={!canWrite || savingRuleName === rule.rule_name}
                            onChange={(checked) =>
                              setDrafts((current) => ({
                                ...current,
                                [rule.rule_name]: {
                                  ...current[rule.rule_name],
                                  enabled: checked,
                                },
                              }))
                            }
                          />
                          <span>{draft?.enabled ? 'Enabled' : 'Disabled'}</span>
                        </div>
                      </td>
                      <td className="px-3 py-3">
                        {isThresholdEditable(rule.rule_name) ? (
                          <input
                            value={draft?.threshold ?? ''}
                            disabled={!canWrite}
                            onChange={(event) =>
                              setDrafts((current) => ({
                                ...current,
                                [rule.rule_name]: {
                                  ...current[rule.rule_name],
                                  threshold: event.target.value,
                                },
                              }))
                            }
                            className="h-9 w-[120px] rounded-md border border-border bg-white px-3 text-[12px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                            placeholder="Row limit"
                          />
                        ) : (
                          <input
                            value="N/A"
                            disabled
                            readOnly
                            aria-label={`${rule.rule_name} threshold not applicable`}
                            className="h-9 w-[120px] cursor-not-allowed rounded-md border border-border bg-panel-soft px-3 text-[12px] font-medium text-muted outline-none disabled:cursor-not-allowed disabled:opacity-100"
                          />
                        )}
                      </td>
                      <td className="px-3 py-3">
                        <button
                          type="button"
                          onClick={() => void handleSave(rule)}
                          disabled={!canWrite || savingRuleName === rule.rule_name}
                          className="inline-flex h-8 items-center justify-center gap-1 rounded-md border border-border bg-panel-soft px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:opacity-50"
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
      )}

      <Pagination
        offset={offset}
        pageSize={PAGE_SIZE}
        count={pagedRules.length}
        total={rules.length}
        onChange={setOffset}
      />

      {!canWrite ? <InlineAlert>This page is in read-only mode. `sql_review.write` is required to update rules.</InlineAlert> : null}
      {error ? <InlineAlert>{error}</InlineAlert> : null}
    </div>
  )
}

function getRuleDescription(rule: SQLReviewRule) {
  return RULE_METADATA[rule.rule_name]?.description ?? rule.description
}

function isThresholdEditable(ruleName: string) {
  return RULE_METADATA[ruleName]?.thresholdEditable ?? false
}
