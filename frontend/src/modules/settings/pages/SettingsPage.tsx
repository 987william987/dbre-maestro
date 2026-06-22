import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Plus, Save, Trash2 } from 'lucide-react'
import { listAuthGroups } from '@/modules/auth-groups/api'
import { getSettings, listSettingsDBConnections, patchSettings, previewWorkflowRules } from '@/modules/settings/api'
import { ApiError } from '@/shared/api/client'
import type { AuthGroupSummary } from '@/shared/types/authGroup'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { ApprovalPolicy, PlatformSettings, WorkflowRule, WorkflowRulePreview } from '@/shared/types/settings'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { PageIntro } from '@/shared/ui/PageIntro'
import { Switch } from '@/shared/ui/Switch'
import { useToast } from '@/shared/ui/ToastContext'

type SettingsForm = {
  larkAppID: string
  larkAppSecret: string
  larkAppSecretConfigured: boolean
  sqlEditorAppTimeoutSeconds: string
  sqlEditorMySQLMaxExecutionTimeMs: string
  sqlEditorPostgresStatementTimeoutMs: string
  inventoryEnabled: boolean
  inventoryRegions: string
  inventoryEngines: string
  inventoryCron: string
  objectEnabled: boolean
  objectConnectionIDs: number[]
  objectCron: string
  cronTimezone: string
  approvalPolicies: ApprovalPolicy[]
  workflowRules: WorkflowRule[]
}

const WORKFLOW_TICKET_TYPE_LABELS: Record<WorkflowRule['ticket_type'], string> = {
  ddl: 'DDL',
  dml: 'DML',
  redis_command: 'Redis Command',
  query_access: 'Query Access',
  sql_export: 'SQL Export',
  sensitive_query_access: 'Sensitive Query Access',
}

const WORKFLOW_TICKET_TYPES: Array<WorkflowRule['ticket_type']> = [
  'ddl',
  'dml',
  'redis_command',
  'query_access',
  'sql_export',
  'sensitive_query_access',
]

const WORKFLOW_REVIEW_PERMISSIONS: Record<WorkflowRule['ticket_type'], string[]> = {
  ddl: ['tickets.review'],
  dml: ['tickets.review'],
  redis_command: ['tickets.review'],
  query_access: ['tickets.review'],
  sql_export: ['sql_editor.export_review'],
  sensitive_query_access: ['sql_editor.sensitive_review'],
}

export function SettingsPage() {
  const { pushToast } = useToast()
  const [settings, setSettings] = useState<PlatformSettings | null>(null)
  const [form, setForm] = useState<SettingsForm | null>(null)
  const [connections, setConnections] = useState<Array<Pick<DBConnection, 'id' | 'name' | 'db_type' | 'host' | 'port'>>>([])
  const [authGroups, setAuthGroups] = useState<AuthGroupSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [workflowPreviews, setWorkflowPreviews] = useState<WorkflowRulePreview[]>([])
  const workflowIssues = form ? findWorkflowRuleIssues(form.workflowRules, authGroups) : []

  useEffect(() => {
    let active = true

    async function bootstrap() {
      setLoading(true)
      setError('')
      try {
        const [settingsResponse, connectionsResponse, authGroupsResponse] = await Promise.all([
          getSettings(),
          listSettingsDBConnections(),
          listAuthGroups(),
        ])
        if (active) {
          setSettings(settingsResponse)
          setForm(toForm(settingsResponse))
          setConnections(connectionsResponse.connections)
          setAuthGroups(authGroupsResponse.auth_groups)
        }
      } catch (loadError) {
        if (active) {
          setError(loadError instanceof ApiError ? loadError.message : 'Failed to load platform settings.')
        }
      } finally {
        if (active) {
          setLoading(false)
        }
      }
    }

    void bootstrap()

    return () => {
      active = false
    }
  }, [])

  useEffect(() => {
    if (!form || form.workflowRules.length === 0) {
      setWorkflowPreviews([])
      return
    }
    let active = true
    const timer = window.setTimeout(() => {
      previewWorkflowRules(form.workflowRules)
        .then((response) => {
          if (active) {
            setWorkflowPreviews(response.previews)
          }
        })
        .catch(() => {
          if (active) {
            setWorkflowPreviews([])
          }
        })
    }, 250)
    return () => {
      active = false
      window.clearTimeout(timer)
    }
  }, [form?.workflowRules])

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!form) {
      return
    }

    setSaving(true)
    setError('')
    try {
      const issues = findWorkflowRuleIssues(form.workflowRules, authGroups)
      if (issues.length > 0) {
        setError(`Workflow rules are incomplete: ${issues.join(', ')}`)
        return
      }
      const payload = toPayload(settings, form)
      const saved = await patchSettings(payload)
      setSettings(saved)
      setForm(toForm(saved))
      pushToast('Platform settings updated.', 'success')
    } catch (saveError) {
      setError(saveError instanceof ApiError ? saveError.message : 'Failed to update platform settings.')
    } finally {
      setSaving(false)
    }
  }

  function updateWorkflowRule(index: number, patch: Partial<WorkflowRule>) {
    setForm((current) => {
      if (!current) {
        return current
      }
      return {
        ...current,
        workflowRules: current.workflowRules.map((rule, itemIndex) => itemIndex === index ? normalizeWorkflowRulePatch({ ...rule, ...patch }) : rule),
      }
    })
  }

  function addWorkflowRule() {
    setForm((current) => current ? {
      ...current,
      workflowRules: [
        ...current.workflowRules,
        {
          rule_name: 'New Workflow Rule',
          ticket_type: 'ddl',
          db_connection_id: null,
          export_sensitivity: null,
          approval_enabled: true,
          approval_auth_groups: ['data_owner'],
          executor_auth_groups: ['dba'],
          priority: 100,
          enabled: true,
        },
      ],
    } : current)
  }

  function removeWorkflowRule(index: number) {
    setForm((current) => current ? {
      ...current,
      workflowRules: current.workflowRules.filter((_, itemIndex) => itemIndex !== index),
    } : current)
  }

  return (
    <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
      <PageIntro
        title="Platform Settings"
        description="Manage SQL Editor timeout policy and DB metadata scan settings. AWS access still relies on the runtime IAM role, and database credentials are still managed in DB Connections."
      />

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      {loading || !form ? (
        <LoadingBlock message="Loading platform settings..." className="min-h-[320px] rounded-xl border-border bg-panel" />
      ) : (
        <form onSubmit={handleSubmit} className="grid gap-3">
          <section className="rounded-xl border border-border bg-panel shadow-soft">
            <div className="border-b border-border/80 px-4 py-3">
              <p className="text-[14px] font-semibold text-ink">Lark Notifications</p>
              <p className="mt-1 text-[12px] leading-5 text-muted">Configure Lark app credentials for ticket notifications. Directed delivery uses each user&apos;s configured Lark Open ID. Leave App Secret blank to keep the existing secret.</p>
            </div>
            <div className="grid gap-4 px-4 py-4 md:grid-cols-2">
              <Field
                label="Lark App ID"
                value={form.larkAppID}
                onChange={(value) => setForm((current) => current ? { ...current, larkAppID: value } : current)}
                placeholder="cli_xxxxxxxxxxxxx"
              />
              <Field
                label={`Lark App Secret${form.larkAppSecretConfigured ? ' (Configured)' : ''}`}
                value={form.larkAppSecret}
                onChange={(value) => setForm((current) => current ? { ...current, larkAppSecret: value } : current)}
                placeholder={form.larkAppSecretConfigured ? 'Leave blank to keep existing secret' : 'Enter app secret'}
                type="password"
              />
            </div>
          </section>

          <section className="rounded-xl border border-border bg-panel shadow-soft">
            <div className="border-b border-border/80 px-4 py-3">
              <p className="text-[14px] font-semibold text-ink">SQL Editor Timeout</p>
              <p className="mt-1 text-[12px] leading-5 text-muted">These values apply only to SQL Editor `/api/query`. The app timeout caps the request lifetime, while MySQL and PostgreSQL values are applied at the session level before each query.</p>
            </div>
            <div className="grid gap-4 px-4 py-4 md:grid-cols-3">
              <Field
                label="App timeout (seconds)"
                value={form.sqlEditorAppTimeoutSeconds}
                onChange={(value) => setForm((current) => current ? { ...current, sqlEditorAppTimeoutSeconds: value } : current)}
              />
              <Field
                label="MySQL max_execution_time (ms)"
                value={form.sqlEditorMySQLMaxExecutionTimeMs}
                onChange={(value) => setForm((current) => current ? { ...current, sqlEditorMySQLMaxExecutionTimeMs: value } : current)}
              />
              <Field
                label="PostgreSQL statement_timeout (ms)"
                value={form.sqlEditorPostgresStatementTimeoutMs}
                onChange={(value) => setForm((current) => current ? { ...current, sqlEditorPostgresStatementTimeoutMs: value } : current)}
              />
            </div>
          </section>

          <section className="rounded-xl border border-border bg-panel shadow-soft">
            <div className="border-b border-border/80 px-4 py-3">
              <p className="text-[14px] font-semibold text-ink">Inventory Scan</p>
              <p className="mt-1 text-[12px] leading-5 text-muted">Pull a cloud inventory snapshot from AWS APIs on a cron schedule. Use 5-field cron syntax, for example 0 9 * * *.</p>
            </div>
            <div className="grid gap-4 px-4 py-4 md:grid-cols-2">
              <label className="flex items-center gap-2 text-[13px] font-medium text-ink">
                <Switch
                  ariaLabel="Enable inventory scanning"
                  checked={form.inventoryEnabled}
                  onChange={(checked) => setForm((current) => current ? { ...current, inventoryEnabled: checked } : current)}
                />
                Enable inventory scan
              </label>
              <Field
                label="Inventory cron"
                value={form.inventoryCron}
                onChange={(value) => setForm((current) => current ? { ...current, inventoryCron: value } : current)}
                placeholder="0 9 * * *"
              />
              <Field
                label="Regions"
                value={form.inventoryRegions}
                onChange={(value) => setForm((current) => current ? { ...current, inventoryRegions: value } : current)}
                placeholder="ap-northeast-1, ap-southeast-1"
              />
              <Field
                label="Engines"
                value={form.inventoryEngines}
                onChange={(value) => setForm((current) => current ? { ...current, inventoryEngines: value } : current)}
                placeholder="aurora-mysql, aurora-postgresql, redis"
              />
            </div>
          </section>

          <section className="rounded-xl border border-border bg-panel shadow-soft">
            <div className="border-b border-border/80 px-4 py-3">
              <p className="text-[14px] font-semibold text-ink">Object Scan</p>
              <p className="mt-1 text-[12px] leading-5 text-muted">Capture object snapshots on a cron schedule for the selected DB connections.</p>
            </div>
            <div className="grid gap-4 px-4 py-4 md:grid-cols-2">
              <label className="flex items-center gap-2 text-[13px] font-medium text-ink">
                <Switch
                  ariaLabel="Enable object scanning"
                  checked={form.objectEnabled}
                  onChange={(checked) => setForm((current) => current ? { ...current, objectEnabled: checked } : current)}
                />
                Enable object scan
              </label>
              <Field
                label="Object cron"
                value={form.objectCron}
                onChange={(value) => setForm((current) => current ? { ...current, objectCron: value } : current)}
                placeholder="0 10 * * *"
              />
              <Field
                label="Cron timezone"
                value={form.cronTimezone}
                onChange={(value) => setForm((current) => current ? { ...current, cronTimezone: value } : current)}
                placeholder="Asia/Taipei"
              />
            </div>
            <div className="border-t border-border/80 px-4 py-4">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <p className="text-[13px] font-semibold text-ink">Included DB Connections</p>
                  <p className="mt-1 text-[12px] leading-5 text-muted">Choose which connections should be included in object snapshots.</p>
                </div>
                <div className="rounded-full border border-border bg-panel-soft px-3 py-1 text-[11px] font-semibold text-muted">
                  {form.objectConnectionIDs.length} selected
                </div>
              </div>

              {connections.length === 0 ? (
                <div className="mt-4 rounded-lg border border-dashed border-border bg-panel-soft px-4 py-4 text-[12px] text-muted">
                  No DB connections are available for selection.
                </div>
              ) : (
                <div className="mt-5 grid gap-4 md:grid-cols-2 xl:grid-cols-3">
                  {connections.map((connection) => {
                    const checked = form.objectConnectionIDs.includes(connection.id)
                    return (
                      <label
                        key={connection.id}
                        className={`flex cursor-pointer items-start gap-4 rounded-xl border px-4 py-4 transition ${
                          checked ? 'border-slate-300 bg-slate-50' : 'border-border bg-white hover:bg-panel-soft'
                        }`}
                      >
                        <div className="pt-1">
                          <Switch
                            ariaLabel={`${connection.name} selected for object scan`}
                            checked={checked}
                            onChange={() =>
                              setForm((current) =>
                                current
                                  ? {
                                      ...current,
                                      objectConnectionIDs: checked
                                        ? current.objectConnectionIDs.filter((id) => id !== connection.id)
                                        : [...current.objectConnectionIDs, connection.id].sort((left, right) => left - right),
                                    }
                                  : current,
                              )
                            }
                          />
                        </div>
                        <div className="min-w-0">
                          <div className="flex flex-wrap items-center gap-2">
                            <p className="text-[13px] font-semibold leading-6 text-ink">{connection.name}</p>
                            <span className="rounded-full border border-border bg-panel-soft px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted">
                              {formatDBType(connection.db_type)}
                            </span>
                          </div>
                        </div>
                      </label>
                    )
                  })}
                </div>
              )}
            </div>
          </section>

          <section className="rounded-xl border border-border bg-panel shadow-soft">
            <div className="border-b border-border/80 px-4 py-3">
              <p className="text-[14px] font-semibold text-ink">Workflow Rules</p>
              <p className="mt-1 text-[12px] leading-5 text-muted">Route ticket approval, export approval, and execution responsibility by ticket type and DB connection.</p>
            </div>
            {workflowIssues.length > 0 ? (
              <div className="border-b border-danger/20 bg-red-50 px-4 py-3 text-[12px] font-medium leading-5 text-danger">
                {workflowIssues.join(' ')}
              </div>
            ) : null}
            <div className="divide-y divide-border/80">
              {form.workflowRules.length === 0 ? (
                <div className="px-4 py-6 text-[13px] text-muted">No workflow rules configured.</div>
              ) : form.workflowRules.map((rule, index) => (
                <WorkflowRuleEditor
                  key={`${rule.id ?? 'new'}-${index}`}
                  rule={rule}
                  index={index}
                  connections={connections}
                  authGroups={authGroups}
                  preview={workflowPreviews[index]}
                  onChange={(patch) => updateWorkflowRule(index, patch)}
                  onRemove={() => removeWorkflowRule(index)}
                />
              ))}
            </div>
            <div className="flex justify-end border-t border-border/80 px-4 py-3">
              <button
                type="button"
                onClick={addWorkflowRule}
                className="inline-flex h-9 items-center gap-2 rounded-md border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-panel-soft"
              >
                <Plus className="h-4 w-4" />
                Add Rule
              </button>
            </div>
          </section>

          <div className="flex justify-end">
            <button
              type="submit"
              disabled={saving || workflowIssues.length > 0}
              className="inline-flex h-10 items-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-60"
            >
              <Save className="h-4 w-4" />
              {saving ? 'Saving...' : 'Save Settings'}
            </button>
          </div>
        </form>
      )}
    </div>
  )
}

function WorkflowRuleEditor({
  rule,
  index,
  connections,
  authGroups,
  preview,
  onChange,
  onRemove,
}: {
  rule: WorkflowRule
  index: number
  connections: Array<Pick<DBConnection, 'id' | 'name' | 'db_type' | 'host' | 'port'>>
  authGroups: AuthGroupSummary[]
  preview?: WorkflowRulePreview
  onChange: (patch: Partial<WorkflowRule>) => void
  onRemove: () => void
}) {
  const isExecutable = isExecutableTicketType(rule.ticket_type)
  const approvalGroupItems = workflowAuthGroupItems(authGroups, rule.approval_auth_groups)
  const executorGroupItems = workflowAuthGroupItems(authGroups, rule.executor_auth_groups)
  const requiredReviewPermissions = WORKFLOW_REVIEW_PERMISSIONS[rule.ticket_type] ?? []
  const hasDeprecatedReviewer = [...rule.approval_auth_groups, ...rule.executor_auth_groups].includes('reviewer')

  return (
    <div className="grid gap-4 px-4 py-4">
      <div className="grid gap-3 lg:grid-cols-[minmax(180px,1.2fr)_170px_190px_140px_auto]">
        <Field
          label="Rule name"
          value={rule.rule_name}
          onChange={(value) => onChange({ rule_name: value })}
        />
        <label className="grid gap-2 text-[12px] font-semibold text-muted">
          <span>Ticket type</span>
          <DropdownSelect
            ariaLabel={`Workflow rule ${index + 1} ticket type`}
            value={rule.ticket_type}
            onChange={(value) => onChange({ ticket_type: value as WorkflowRule['ticket_type'] })}
            options={WORKFLOW_TICKET_TYPES.map((ticketType) => ({
              value: ticketType,
              label: WORKFLOW_TICKET_TYPE_LABELS[ticketType],
            }))}
          />
        </label>
        <label className="grid gap-2 text-[12px] font-semibold text-muted">
          <span>DB connection</span>
          <DropdownSelect
            ariaLabel={`Workflow rule ${index + 1} DB connection`}
            value={rule.db_connection_id == null ? '' : String(rule.db_connection_id)}
            onChange={(value) => onChange({ db_connection_id: value === '' ? null : Number(value) })}
            options={[
              { value: '', label: 'All connections' },
              ...connections.map((connection) => ({ value: String(connection.id), label: connection.name })),
            ]}
            menuClassName="max-h-[360px] overflow-y-auto"
          />
        </label>
        <Field
          label="Priority"
          value={String(rule.priority)}
          onChange={(value) => onChange({ priority: parsePositiveInt(value, 100) })}
        />
        <div className="flex items-end justify-end">
          <button
            type="button"
            onClick={onRemove}
            className="inline-flex h-10 w-10 items-center justify-center rounded-lg border border-danger/20 bg-red-50 text-danger transition hover:bg-red-100"
            aria-label={`Remove workflow rule ${index + 1}`}
          >
            <Trash2 className="h-4 w-4" />
          </button>
        </div>
      </div>

      <div className="grid gap-4 lg:grid-cols-[220px_1fr_1fr]">
        <div className="grid content-start gap-3">
          <label className="flex items-center gap-2 text-[13px] font-semibold text-ink">
            <Switch
              ariaLabel={`${rule.rule_name} enabled`}
              checked={rule.enabled}
              onChange={(checked) => onChange({ enabled: checked })}
            />
            Enabled
          </label>
          <label className="flex items-center gap-2 text-[13px] font-semibold text-ink">
            <Switch
              ariaLabel={`${rule.rule_name} approval enabled`}
              checked={rule.approval_enabled}
              onChange={(checked) => onChange({ approval_enabled: checked })}
            />
            Approval required
          </label>
          {rule.ticket_type === 'sql_export' ? (
            <label className="grid gap-2 text-[12px] font-semibold text-muted">
              <span>Export sensitivity</span>
              <DropdownSelect
                ariaLabel={`Workflow rule ${index + 1} export sensitivity`}
                value={rule.export_sensitivity ?? 'normal'}
                onChange={(value) => onChange({ export_sensitivity: value as 'normal' | 'sensitive' })}
                options={[
                  { value: 'normal', label: 'Normal' },
                  { value: 'sensitive', label: 'Sensitive' },
                ]}
              />
            </label>
          ) : null}
          <div className="rounded-lg border border-border bg-panel-soft px-3 py-2 text-[11px] leading-5 text-muted">
            Review permission: {requiredReviewPermissions.join(' or ') || 'None'}
            {isExecutable ? <><br />Execution permission: tickets.execute</> : null}
          </div>
        </div>
        <Checklist
          title="Approval auth groups"
          emptyMessage="No auth groups available."
          items={approvalGroupItems}
          selectedIDs={rule.approval_auth_groups}
          onChange={(selectedIDs) => onChange({ approval_auth_groups: selectedIDs })}
        />
        <Checklist
          title="Executor auth groups"
          emptyMessage="No auth groups available."
          items={executorGroupItems}
          selectedIDs={rule.executor_auth_groups}
          onChange={(selectedIDs) => onChange({ executor_auth_groups: selectedIDs })}
        />
      </div>
      {hasDeprecatedReviewer ? (
        <div className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-[12px] leading-5 text-amber-800">
          The Reviewer auth group is deprecated. Move this rule to Data Owner or Security before the legacy group is removed.
        </div>
      ) : null}
      <WorkflowRulePreviewSummary preview={preview} />
    </div>
  )
}

function WorkflowRulePreviewSummary({ preview }: { preview?: WorkflowRulePreview }) {
  if (!preview) {
    return (
      <div className="rounded-lg border border-border bg-panel-soft px-3 py-2 text-[12px] leading-5 text-muted">
        Resolving effective workflow preview...
      </div>
    )
  }
  const reviewerNames = preview.approval_users.map((user) => user.username).join(', ') || 'None'
  const executorNames = preview.executor_users.map((user) => user.username).join(', ') || 'None'
  const conflictNames = preview.conflict_rule_names.join(', ')
  const hasIssue = Boolean(preview.resolution.error_code || preview.shadowed_by_rule_id || preview.conflict_rule_ids.length > 0)

  return (
    <div className={`rounded-lg border px-3 py-2 text-[12px] leading-5 ${hasIssue ? 'border-amber-200 bg-amber-50 text-amber-900' : 'border-emerald-200 bg-emerald-50 text-emerald-900'}`}>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
        <span className="font-semibold">{preview.effective ? 'Effective' : 'Not effective'}</span>
        <span>Reviewers: {reviewerNames}</span>
        {preview.rule.ticket_type === 'ddl' || preview.rule.ticket_type === 'dml' || preview.rule.ticket_type === 'redis_command' ? (
          <span>Executors: {executorNames}</span>
        ) : null}
      </div>
      {preview.resolution.error_message ? (
        <p className="mt-1">{preview.resolution.error_message}</p>
      ) : null}
      {preview.shadowed_by_rule_name ? (
        <p className="mt-1">Shadowed by: {preview.shadowed_by_rule_name}</p>
      ) : null}
      {conflictNames ? (
        <p className="mt-1">Conflict: {conflictNames}</p>
      ) : null}
    </div>
  )
}

function workflowAuthGroupItems(authGroups: AuthGroupSummary[], selectedGroups: string[]) {
  return authGroups
    .filter((group) => group.name !== 'reviewer' || selectedGroups.includes(group.name))
    .map((group) => ({
      id: group.name,
      label: group.name === 'reviewer' ? `${group.label} (Deprecated)` : group.label,
    }))
}

function findWorkflowRuleIssues(rules: WorkflowRule[], authGroups: AuthGroupSummary[]) {
  const availableGroups = new Set(authGroups.map((group) => group.name))
  const issues: string[] = []
  for (const [index, rule] of rules.entries()) {
    if (!rule.enabled) {
      continue
    }
    const label = rule.rule_name.trim() || `Rule ${index + 1}`
    if (!rule.rule_name.trim()) {
      issues.push(`${label}: rule name is required.`)
    }
    if (rule.ticket_type === 'sql_export' && rule.export_sensitivity !== 'normal' && rule.export_sensitivity !== 'sensitive') {
      issues.push(`${label}: SQL Export requires export sensitivity.`)
    }
    if (rule.approval_enabled && rule.approval_auth_groups.length === 0) {
      issues.push(`${label}: approval auth groups are required when approval is enabled.`)
    }
    if (isExecutableTicketType(rule.ticket_type) && rule.executor_auth_groups.length === 0) {
      issues.push(`${label}: executor auth groups are required for executable tickets.`)
    }
    for (const group of [...rule.approval_auth_groups, ...rule.executor_auth_groups]) {
      if (!availableGroups.has(group)) {
        issues.push(`${label}: auth group ${group} does not exist.`)
      }
    }
  }
  return issues
}

function isExecutableTicketType(ticketType: WorkflowRule['ticket_type']) {
  return ticketType === 'ddl' || ticketType === 'dml' || ticketType === 'redis_command'
}

function normalizeWorkflowRulePatch(rule: WorkflowRule): WorkflowRule {
  const nextRule = { ...rule }
  if (nextRule.ticket_type === 'sql_export') {
    nextRule.export_sensitivity = nextRule.export_sensitivity === 'sensitive' ? 'sensitive' : 'normal'
  } else {
    nextRule.export_sensitivity = null
  }
  if (!isExecutableTicketType(nextRule.ticket_type)) {
    nextRule.executor_auth_groups = []
  }
  return nextRule
}

function Field({
  label,
  value,
  onChange,
  placeholder,
  type = 'text',
}: {
  label: string
  value: string
  onChange: (value: string) => void
  placeholder?: string
  type?: string
}) {
  return (
    <label className="grid gap-2 text-[12px] font-semibold text-muted">
      <span>{label}</span>
      <input
        type={type}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        className="h-10 rounded-lg border border-border bg-white px-3 text-[13px] text-ink outline-none transition focus:border-slate-400"
      />
    </label>
  )
}

function Checklist<T extends string | number>({
  title,
  emptyMessage,
  items,
  selectedIDs,
  onChange,
}: {
  title: string
  emptyMessage: string
  items: Array<{ id: T; label: string }>
  selectedIDs: T[]
  onChange: (selectedIDs: T[]) => void
}) {
  return (
    <div className="grid gap-2">
      <p className="text-[12px] font-semibold text-muted">{title}</p>
      {items.length === 0 ? (
        <p className="rounded-lg border border-dashed border-border bg-panel-soft px-3 py-2 text-[12px] text-muted">{emptyMessage}</p>
      ) : (
        <div className="grid max-h-40 gap-2 overflow-y-auto rounded-lg border border-border bg-white p-2">
          {items.map((item) => {
            const checked = selectedIDs.includes(item.id)
            return (
              <label key={String(item.id)} className="flex items-center gap-2 rounded-md px-2 py-1.5 text-[12px] text-ink hover:bg-panel-soft">
                <input
                  type="checkbox"
                  checked={checked}
                  onChange={() =>
                    onChange(
                      checked
                        ? selectedIDs.filter((selectedID) => selectedID !== item.id)
                        : [...selectedIDs, item.id],
                    )
                  }
                />
                <span className="truncate">{item.label}</span>
              </label>
            )
          })}
        </div>
      )}
    </div>
  )
}

function toForm(settings: PlatformSettings): SettingsForm {
  return {
    larkAppID: settings.lark_app_id,
    larkAppSecret: '',
    larkAppSecretConfigured: settings.lark_app_secret_configured,
    sqlEditorAppTimeoutSeconds: String(settings.sql_editor_app_timeout_seconds),
    sqlEditorMySQLMaxExecutionTimeMs: String(settings.sql_editor_mysql_max_execution_time_ms),
    sqlEditorPostgresStatementTimeoutMs: String(settings.sql_editor_postgres_statement_timeout_ms),
    inventoryEnabled: settings.db_metadata_inventory_enabled,
    inventoryRegions: settings.db_metadata_inventory_regions.join(', '),
    inventoryEngines: settings.db_metadata_inventory_engines.join(', '),
    inventoryCron: settings.db_metadata_inventory_cron,
    objectEnabled: settings.db_metadata_object_enabled,
    objectConnectionIDs: settings.db_metadata_object_enabled_connection_ids,
    objectCron: settings.db_metadata_object_cron,
    cronTimezone: settings.db_metadata_cron_timezone,
    approvalPolicies: settings.approval_policies,
    workflowRules: settings.workflow_rules,
  }
}

function toPayload(current: PlatformSettings | null, form: SettingsForm): PlatformSettings {
  return {
    sensitive_export_reviewer_user_ids: current?.sensitive_export_reviewer_user_ids ?? [],
    sensitive_query_access_reviewer_user_ids: current?.sensitive_query_access_reviewer_user_ids ?? [],
    require_non_sensitive_export_review: current?.require_non_sensitive_export_review ?? true,
    lark_app_id: form.larkAppID.trim(),
    lark_app_secret: form.larkAppSecret,
    lark_app_secret_configured: form.larkAppSecretConfigured,
    sql_editor_app_timeout_seconds: parsePositiveInt(form.sqlEditorAppTimeoutSeconds, 30),
    sql_editor_mysql_max_execution_time_ms: parsePositiveInt(form.sqlEditorMySQLMaxExecutionTimeMs, 25000),
    sql_editor_postgres_statement_timeout_ms: parsePositiveInt(form.sqlEditorPostgresStatementTimeoutMs, 25000),
    db_metadata_inventory_enabled: form.inventoryEnabled,
    db_metadata_inventory_regions: splitCSV(form.inventoryRegions),
    db_metadata_inventory_engines: splitCSV(form.inventoryEngines),
    db_metadata_inventory_cron: form.inventoryCron.trim(),
    db_metadata_inventory_sync_interval_minutes: current?.db_metadata_inventory_sync_interval_minutes ?? 5,
    db_metadata_object_enabled: form.objectEnabled,
    db_metadata_object_enabled_connection_ids: form.objectConnectionIDs,
    db_metadata_object_cron: form.objectCron.trim(),
    db_metadata_object_sync_interval_minutes: current?.db_metadata_object_sync_interval_minutes ?? 60,
    db_metadata_cron_timezone: form.cronTimezone.trim(),
    approval_policies: form.approvalPolicies,
    workflow_rules: form.workflowRules,
  }
}

function splitCSV(value: string) {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function parsePositiveInt(value: string, fallback: number) {
  const parsed = Number.parseInt(value, 10)
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return fallback
  }
  return parsed
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
