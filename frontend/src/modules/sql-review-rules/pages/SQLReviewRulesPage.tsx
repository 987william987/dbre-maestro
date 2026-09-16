import { useEffect, useMemo, useState } from 'react'
import { Loader2, ShieldCheck } from 'lucide-react'
import { useParams } from 'react-router-dom'
import { ApiError } from '@/shared/api/client'
import { useAuth } from '@/shared/auth/AuthContext'
import type { SQLReviewRule } from '@/shared/types/sqlReviewRule'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { Pagination } from '@/shared/ui/Pagination'
import { PageTabs } from '@/shared/ui/PageTabs'
import { Switch } from '@/shared/ui/Switch'
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
import { listSQLReviewRules, patchSQLReviewRule } from '@/modules/sql-review-rules/api'

type RuleSeverity = 'error' | 'warning'
type RuleCategory = 'engine' | 'table' | 'statement' | 'naming' | 'column' | 'schema' | 'database' | 'index' | 'system'
type DraftMap = Record<string, { enabled: boolean; threshold: string; severity: RuleSeverity }>

const RULE_METADATA: Record<string, { description: string; thresholdEditable: boolean; category: RuleCategory }> = {
  ddl_no_comment: {
    description: 'Require CREATE TABLE statements to include a table comment.',
    thresholdEditable: false,
    category: 'table',
  },
  dml_no_where: {
    description: 'Require UPDATE and DELETE statements to include a WHERE clause.',
    thresholdEditable: false,
    category: 'statement',
  },
  full_table_scan: {
    description: 'Block queries when EXPLAIN detects a full table scan.',
    thresholdEditable: false,
    category: 'statement',
  },
  high_row_count: {
    description: 'Block queries when EXPLAIN estimated rows exceed the configured threshold.',
    thresholdEditable: true,
    category: 'statement',
  },
  require_utf8mb4: {
    description: 'Require CREATE TABLE statements to use utf8mb4.',
    thresholdEditable: false,
    category: 'system',
  },
  require_innodb: {
    description: 'Require CREATE TABLE statements to use the InnoDB storage engine.',
    thresholdEditable: false,
    category: 'engine',
  },
  require_primary_key: {
    description: 'Require every newly created table to include a primary key.',
    thresholdEditable: false,
    category: 'table',
  },
  prohibit_foreign_key: {
    description: 'Keep referential integrity in the application layer instead of using foreign key constraints.',
    thresholdEditable: false,
    category: 'table',
  },
  prohibit_trigger: { description: 'Prohibit MySQL triggers.', thresholdEditable: false, category: 'system' },
  prohibit_stored_function: { description: 'Prohibit MySQL stored functions.', thresholdEditable: false, category: 'system' },
  prohibit_stored_procedure: { description: 'Prohibit MySQL stored procedures.', thresholdEditable: false, category: 'system' },
  prohibit_view: { description: 'Prohibit creating MySQL views.', thresholdEditable: false, category: 'system' },
  prohibit_event: { description: 'Prohibit MySQL scheduled events.', thresholdEditable: false, category: 'system' },
  prohibit_reserved_column_name: { description: 'Prohibit MySQL reserved words as column names.', thresholdEditable: false, category: 'naming' },
}

const RULE_CATEGORIES: { key: 'all' | RuleCategory; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'engine', label: 'Engine' },
  { key: 'table', label: 'Table' },
  { key: 'statement', label: 'Statement' },
  { key: 'naming', label: 'Naming' },
  { key: 'column', label: 'Column' },
  { key: 'schema', label: 'Schema' },
  { key: 'database', label: 'Database' },
  { key: 'index', label: 'Index' },
  { key: 'system', label: 'System' },
]

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
  const [selectedCategory, setSelectedCategory] = useState<'all' | RuleCategory>('all')
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
              severity: rule.severity ?? 'error',
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
    if (!canWrite) {
      return
    }
    const draft = drafts[rule.rule_name]
    if (!draft) {
      return
    }

    setSavingRuleName(rule.rule_name)
    setError('')
    try {
      const payload: { enabled?: boolean; threshold?: number | null; severity?: RuleSeverity } = {}
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
      if (draft.severity !== (rule.severity ?? 'error')) {
        payload.severity = draft.severity
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
          severity: updated.severity ?? 'error',
        },
      }))
      pushToast('SQL review rule updated.', 'success')
    } catch (saveError) {
      setError(saveError instanceof ApiError ? saveError.message : 'Failed to update the SQL review rule.')
    } finally {
      setSavingRuleName(null)
    }
  }

  const visibleRules = useMemo(
    () => rules.filter((rule) => selectedCategory === 'all' || getRuleCategory(rule) === selectedCategory),
    [rules, selectedCategory],
  )
  const pagedRules = useMemo(() => visibleRules.slice(offset, offset + PAGE_SIZE), [offset, visibleRules])
  const currentEngine = engine === 'postgresql' || engine === 'redis' ? engine : 'mysql'
  const canWrite = user?.permissions.includes('sql_review.write') ?? false

  useEffect(() => {
    if (offset > 0 && offset >= visibleRules.length) {
      setOffset(Math.max(0, Math.floor((Math.max(visibleRules.length - 1, 0)) / PAGE_SIZE) * PAGE_SIZE))
    }
  }, [offset, visibleRules.length])

  return (
    <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
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
      <DataTableSurface>
          <div className="flex min-h-11 items-center gap-1 overflow-x-auto border-b border-border px-3 py-2">
            {RULE_CATEGORIES.map((category) => {
              const count = rules.filter((rule) => category.key === 'all' || getRuleCategory(rule) === category.key).length
              if (category.key !== 'all' && count === 0) {
                return null
              }
              const selected = selectedCategory === category.key
              return (
                <button
                  key={category.key}
                  type="button"
                  aria-pressed={selected}
                  onClick={() => {
                    setSelectedCategory(category.key)
                    setOffset(0)
                  }}
                  className={`inline-flex h-8 shrink-0 items-center gap-1.5 border-b-2 px-2 text-[12px] font-semibold transition ${selected ? 'border-accent text-accent' : 'border-transparent text-muted hover:text-ink'}`}
                >
                  {category.label}
                  <span className="tabular-nums text-[11px] text-faint">{count}</span>
                </button>
              )
            })}
          </div>
          {loading ? (
            <LoadingBlock message="Loading SQL review rules..." className="m-4 min-h-[220px] rounded-xl border-border bg-panel" />
          ) : rules.length === 0 ? (
            <div className="flex h-[220px] items-center justify-center text-[12px] text-muted">No SQL review rules found.</div>
          ) : (
            <DataTableScroll>
            <DataTable>
              <DataTableHead>
                <tr>
                  <DataTableHeaderCell>Rule</DataTableHeaderCell>
                  <DataTableHeaderCell>Description</DataTableHeaderCell>
                  <DataTableHeaderCell>Severity</DataTableHeaderCell>
                  <DataTableHeaderCell>Enabled</DataTableHeaderCell>
                  <DataTableHeaderCell>Threshold</DataTableHeaderCell>
                  {canWrite ? <DataTableHeaderCell>Action</DataTableHeaderCell> : null}
                </tr>
              </DataTableHead>
              <DataTableBody>
                {pagedRules.map((rule) => {
                  const draft = drafts[rule.rule_name]
                  return (
                    <DataTableRow key={rule.rule_name}>
                      <DataTableCell>{rule.rule_name}</DataTableCell>
                      <DataTableCell>{getRuleDescription(rule)}</DataTableCell>
                      <DataTableCell>
                        <div className="inline-flex h-8 overflow-hidden rounded-md border border-border bg-white">
                          {(['error', 'warning'] as const).map((severity) => {
                            const selected = (draft?.severity ?? 'error') === severity
                            return (
                              <button
                                key={severity}
                                type="button"
                                aria-label={`${rule.rule_name} severity ${severity}`}
                                aria-pressed={selected}
                                disabled={!canWrite || savingRuleName === rule.rule_name}
                                onClick={() => setDrafts((current) => ({
                                  ...current,
                                  [rule.rule_name]: { ...current[rule.rule_name], severity },
                                }))}
                                className={`min-w-[66px] px-2 text-[11px] font-semibold capitalize transition disabled:cursor-not-allowed disabled:opacity-60 ${selected ? severity === 'error' ? 'bg-red-50 text-danger' : 'bg-amber-50 text-amber-700' : 'text-muted hover:bg-panel-soft'}`}
                              >
                                {severity}
                              </button>
                            )
                          })}
                        </div>
                      </DataTableCell>
                      <DataTableCell>
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
                      </DataTableCell>
                      <DataTableCell>
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
                      </DataTableCell>
                      {canWrite ? (
                      <DataTableCell>
                        <button
                          type="button"
                          onClick={() => void handleSave(rule)}
                          disabled={!canWrite || savingRuleName === rule.rule_name}
                          className="inline-flex h-8 items-center justify-center gap-1 rounded-md border border-border bg-panel-soft px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:opacity-50"
                        >
                          {savingRuleName === rule.rule_name ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <ShieldCheck className="h-3.5 w-3.5" />}
                          Save
                        </button>
                      </DataTableCell>
                      ) : null}
                    </DataTableRow>
                  )
                })}
              </DataTableBody>
            </DataTable>
            </DataTableScroll>
          )}
      </DataTableSurface>
      )}

      <Pagination
        offset={offset}
        pageSize={PAGE_SIZE}
        count={pagedRules.length}
        total={visibleRules.length}
        onChange={setOffset}
      />

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

function getRuleCategory(rule: SQLReviewRule): RuleCategory {
  const category = rule.category ?? RULE_METADATA[rule.rule_name]?.category
  return RULE_CATEGORIES.some((item) => item.key === category) && category !== 'all' ? category as RuleCategory : 'statement'
}
