import { useEffect, useMemo, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Loader2, Pencil, Plus, ShieldAlert, ShieldCheck, Trash2, X } from 'lucide-react'
import { listDBConnections } from '@/modules/db-connections/api'
import {
  createMaskingRule,
  createMaskingWhitelist,
  deleteMaskingRule,
  deleteMaskingWhitelist,
  listMaskingRules,
  listMaskingWhitelists,
  patchMaskingRule,
  patchMaskingWhitelist,
} from '@/modules/masking-rules/api'
import { listMetadata, listMetadataColumns } from '@/modules/sql-editor/api'
import { useAuth } from '@/shared/auth/AuthContext'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import type { MaskingRule, MaskingWhitelist } from '@/shared/types/maskingRule'
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { useToast } from '@/shared/ui/ToastContext'

type ConnectionOption = {
  id: number
  name: string
}

type RuleForm = {
  columnName: string
  maskMode: 'full' | 'partial' | 'hash'
}

type WhitelistForm = {
  dbConnectionId: string
  databaseName: string
  tableName: string
  columnName: string
}

type RuleDrawerState =
  | { mode: 'create' }
  | { mode: 'edit'; rule: MaskingRule }
  | null

type WhitelistDrawerState =
  | { mode: 'create' }
  | { mode: 'edit'; entry: MaskingWhitelist }
  | null

const EMPTY_RULE_FORM: RuleForm = {
  columnName: '',
  maskMode: 'full',
}

const EMPTY_WHITELIST_FORM: WhitelistForm = {
  dbConnectionId: '',
  databaseName: '',
  tableName: '',
  columnName: '',
}

const MASK_MODE_OPTIONS: Array<{ value: RuleForm['maskMode']; label: string }> = [
  { value: 'full', label: 'full' },
  { value: 'partial', label: 'partial' },
  { value: 'hash', label: 'hash' },
]

export function MaskingRulesPage() {
  const { user } = useAuth()
  const { pushToast } = useToast()
  const canWrite = Boolean(user?.permissions.includes('masking_rules.write'))

  const [rules, setRules] = useState<MaskingRule[]>([])
  const [whitelist, setWhitelist] = useState<MaskingWhitelist[]>([])
  const [connections, setConnections] = useState<ConnectionOption[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const [ruleDrawer, setRuleDrawer] = useState<RuleDrawerState>(null)
  const [ruleForm, setRuleForm] = useState<RuleForm>(EMPTY_RULE_FORM)
  const [ruleSubmitting, setRuleSubmitting] = useState(false)
  const [ruleDrawerError, setRuleDrawerError] = useState('')

  const [whitelistDrawer, setWhitelistDrawer] = useState<WhitelistDrawerState>(null)
  const [whitelistForm, setWhitelistForm] = useState<WhitelistForm>(EMPTY_WHITELIST_FORM)
  const [whitelistSubmitting, setWhitelistSubmitting] = useState(false)
  const [whitelistDrawerError, setWhitelistDrawerError] = useState('')
  const [databaseOptions, setDatabaseOptions] = useState<string[]>([])
  const [tableOptions, setTableOptions] = useState<string[]>([])
  const [columnOptions, setColumnOptions] = useState<string[]>([])
  const [targetLoading, setTargetLoading] = useState(false)

  const [pendingDelete, setPendingDelete] = useState<{ kind: 'rule' | 'whitelist'; id: number } | null>(null)
  const [deletingKey, setDeletingKey] = useState<string | null>(null)

  const sortedRules = useMemo(() => [...rules].sort((left, right) => left.column_name.localeCompare(right.column_name)), [rules])
  const sortedWhitelist = useMemo(
    () =>
      [...whitelist].sort((left, right) => {
        const leftKey = `${left.db_connection_id}:${left.database_name}:${left.table_name}:${left.column_name}`
        const rightKey = `${right.db_connection_id}:${right.database_name}:${right.table_name}:${right.column_name}`
        return leftKey.localeCompare(rightKey)
      }),
    [whitelist],
  )

  useEffect(() => {
    void loadPage()
  }, [])

  useEffect(() => {
    if (!whitelistDrawer || !whitelistForm.dbConnectionId) {
      setDatabaseOptions([])
      setTableOptions([])
      setColumnOptions([])
      return
    }
    void loadDatabases(Number(whitelistForm.dbConnectionId))
  }, [whitelistDrawer, whitelistForm.dbConnectionId])

  useEffect(() => {
    if (!whitelistDrawer || !whitelistForm.dbConnectionId || !whitelistForm.databaseName) {
      setTableOptions([])
      setColumnOptions([])
      return
    }
    void loadTables(Number(whitelistForm.dbConnectionId), whitelistForm.databaseName)
  }, [whitelistDrawer, whitelistForm.dbConnectionId, whitelistForm.databaseName])

  useEffect(() => {
    if (!whitelistDrawer || !whitelistForm.dbConnectionId || !whitelistForm.databaseName || !whitelistForm.tableName) {
      setColumnOptions([])
      return
    }
    void loadColumns(Number(whitelistForm.dbConnectionId), whitelistForm.databaseName, whitelistForm.tableName)
  }, [whitelistDrawer, whitelistForm.dbConnectionId, whitelistForm.databaseName, whitelistForm.tableName])

  async function loadPage() {
    setLoading(true)
    setError('')
    try {
      const [rulesResponse, whitelistResponse, connectionsResponse] = await Promise.all([
        listMaskingRules(),
        listMaskingWhitelists(),
        listDBConnections(),
      ])
      setRules(rulesResponse.rules)
      setWhitelist(whitelistResponse.whitelist)
      setConnections(
        connectionsResponse.connections
          .filter((connection) => connection.db_type === 'mysql')
          .map((connection) => ({ id: connection.id, name: connection.name })),
      )
    } catch (loadError) {
      setError(loadError instanceof ApiError ? loadError.message : '讀取 masking 設定失敗。')
    } finally {
      setLoading(false)
    }
  }

  async function loadDatabases(connectionId: number) {
    setTargetLoading(true)
    try {
      const response = await listMetadata(connectionId)
      setDatabaseOptions(response.items.map((item) => item.name))
    } catch {
      setDatabaseOptions([])
    } finally {
      setTargetLoading(false)
    }
  }

  async function loadTables(connectionId: number, databaseName: string) {
    setTargetLoading(true)
    try {
      const response = await listMetadata(connectionId, { database: databaseName })
      setTableOptions(response.items.map((item) => item.name))
    } catch {
      setTableOptions([])
    } finally {
      setTargetLoading(false)
    }
  }

  async function loadColumns(connectionId: number, databaseName: string, tableName: string) {
    setTargetLoading(true)
    try {
      const response = await listMetadataColumns(connectionId, databaseName, tableName, databaseName)
      setColumnOptions(response.columns.map((column) => column.name))
    } catch {
      setColumnOptions([])
    } finally {
      setTargetLoading(false)
    }
  }

  function openRuleDrawer(state: RuleDrawerState) {
    setRuleDrawer(state)
    setRuleDrawerError('')
    setRuleForm(
      state?.mode === 'edit'
        ? {
            columnName: state.rule.column_name,
            maskMode: state.rule.mask_mode,
          }
        : EMPTY_RULE_FORM,
    )
  }

  function closeRuleDrawer() {
    setRuleDrawer(null)
    setRuleDrawerError('')
    setRuleSubmitting(false)
    setRuleForm(EMPTY_RULE_FORM)
  }

  function openWhitelistDrawer(state: WhitelistDrawerState) {
    setWhitelistDrawer(state)
    setWhitelistDrawerError('')
    setWhitelistForm(
      state?.mode === 'edit'
        ? {
            dbConnectionId: String(state.entry.db_connection_id),
            databaseName: state.entry.database_name,
            tableName: state.entry.table_name,
            columnName: state.entry.column_name,
          }
        : EMPTY_WHITELIST_FORM,
    )
  }

  function closeWhitelistDrawer() {
    setWhitelistDrawer(null)
    setWhitelistDrawerError('')
    setWhitelistSubmitting(false)
    setWhitelistForm(EMPTY_WHITELIST_FORM)
    setDatabaseOptions([])
    setTableOptions([])
    setColumnOptions([])
  }

  async function handleRuleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!ruleDrawer) {
      return
    }

    setRuleSubmitting(true)
    setRuleDrawerError('')
    try {
      const payload = {
        column_name: ruleForm.columnName.trim(),
        mask_mode: ruleForm.maskMode,
      }
      if (ruleDrawer.mode === 'create') {
        await createMaskingRule(payload)
        pushToast(`Global masking rule 已建立: ${payload.column_name}`, 'success')
      } else {
        await patchMaskingRule(ruleDrawer.rule.id, payload)
        pushToast(`Global masking rule 已更新: ${payload.column_name}`, 'success')
      }
      await loadPage()
      closeRuleDrawer()
    } catch (submitError) {
      setRuleDrawerError(submitError instanceof ApiError ? submitError.message : '儲存 global masking rule 失敗。')
    } finally {
      setRuleSubmitting(false)
    }
  }

  async function handleWhitelistSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!whitelistDrawer) {
      return
    }

    setWhitelistSubmitting(true)
    setWhitelistDrawerError('')
    try {
      const payload = {
        db_connection_id: Number(whitelistForm.dbConnectionId),
        database_name: whitelistForm.databaseName.trim(),
        table_name: whitelistForm.tableName.trim(),
        column_name: whitelistForm.columnName.trim(),
      }
      if (whitelistDrawer.mode === 'create') {
        await createMaskingWhitelist(payload)
        pushToast(`Whitelist 已建立: ${payload.database_name}.${payload.table_name}.${payload.column_name}`, 'success')
      } else {
        await patchMaskingWhitelist(whitelistDrawer.entry.id, payload)
        pushToast(`Whitelist 已更新: ${payload.database_name}.${payload.table_name}.${payload.column_name}`, 'success')
      }
      await loadPage()
      closeWhitelistDrawer()
    } catch (submitError) {
      setWhitelistDrawerError(submitError instanceof ApiError ? submitError.message : '儲存 whitelist 失敗。')
    } finally {
      setWhitelistSubmitting(false)
    }
  }

  async function handleDelete() {
    if (!pendingDelete) {
      return
    }

    const deleteKey = `${pendingDelete.kind}:${pendingDelete.id}`
    setDeletingKey(deleteKey)
    setError('')
    try {
      if (pendingDelete.kind === 'rule') {
        await deleteMaskingRule(pendingDelete.id)
        pushToast('Global masking rule 已刪除', 'success')
      } else {
        await deleteMaskingWhitelist(pendingDelete.id)
        pushToast('Whitelist 已刪除', 'success')
      }
      await loadPage()
      setPendingDelete(null)
    } catch (deleteError) {
      setError(deleteError instanceof ApiError ? deleteError.message : '刪除失敗。')
    } finally {
      setDeletingKey(null)
    }
  }

  return (
    <div className="flex h-full flex-col gap-3 p-3 sm:p-4">
      <section className="rounded-xl border border-border bg-panel-soft shadow-soft">
        <div className="px-4 py-3 sm:px-5">
          <div className="flex flex-col gap-2 lg:flex-row lg:items-end lg:justify-between">
            <div className="max-w-3xl">
              <h2 className="text-[24px] font-bold tracking-[-0.03em] text-ink">Masking Rules</h2>
              <p className="mt-2 text-[13px] leading-6 text-muted">
                目前只支援 MySQL。Global rule 只管理欄位名稱與遮罩模式；Whitelist 則用於精準解除特定實例 / database / table / column 的誤殺。
              </p>
            </div>
            {!canWrite ? (
              <div className="rounded-lg border border-border bg-white px-3 py-2 text-[12px] text-muted shadow-soft">
                目前帳號只有 `masking_rules.read`，可查看但不可調整規則。
              </div>
            ) : null}
          </div>
        </div>
      </section>

      {error ? <InlineAlert>{error}</InlineAlert> : null}

          <SectionCard
            title="Global Masking Rules"
            description="Truly global，只接受 column_name + mask_mode。命中後會套用在所有 MySQL 查詢與匯出結果。"
            icon={<ShieldAlert className="h-4 w-4 text-accent" />}
            action={
              canWrite ? (
                <ActionButton onClick={() => openRuleDrawer({ mode: 'create' })}>
                  <Plus className="h-4 w-4" />
                  新增規則
                </ActionButton>
              ) : null
            }
          >
            {loading ? (
              <LoadingBlock message="載入 global masking rules 中…" className="m-4 min-h-[180px] rounded-xl border-border bg-panel" />
            ) : sortedRules.length === 0 ? (
              <EmptyState message="尚未建立任何 global masking rule。" />
            ) : (
              <CompactTable
                headers={['Column', 'Mode', 'Created', 'Actions']}
                rows={sortedRules.map((rule) => ({
                  key: `rule-${rule.id}`,
                  cells: [
                    <span key="column" className="font-semibold text-ink">{rule.column_name}</span>,
                    <Badge key="mode">{rule.mask_mode}</Badge>,
                    <span key="created" className="whitespace-nowrap text-muted">{formatDateTime(rule.created_at)}</span>,
                    <ActionCell
                      key="actions"
                      canWrite={canWrite}
                      onEdit={() => openRuleDrawer({ mode: 'edit', rule })}
                      onDelete={() => setPendingDelete({ kind: 'rule', id: rule.id })}
                      deleting={deletingKey === `rule:${rule.id}`}
                    />,
                  ],
                }))}
              />
            )}
          </SectionCard>

          <SectionCard
            title="Unmask Whitelist"
            description="只支援 MySQL，精準綁定 connection -> database -> table -> column，用於解除 global rule 的誤殺。"
            icon={<ShieldCheck className="h-4 w-4 text-accent" />}
            action={
              canWrite ? (
                <ActionButton onClick={() => openWhitelistDrawer({ mode: 'create' })}>
                  <Plus className="h-4 w-4" />
                  新增白名單
                </ActionButton>
              ) : null
            }
          >
            {loading ? (
              <LoadingBlock message="載入 whitelist 中…" className="m-4 min-h-[180px] rounded-xl border-border bg-panel" />
            ) : sortedWhitelist.length === 0 ? (
              <EmptyState message="尚未建立任何 whitelist。" />
            ) : (
              <CompactTable
                headers={['Connection', 'Target', 'Created', 'Actions']}
                rows={sortedWhitelist.map((entry) => ({
                  key: `whitelist-${entry.id}`,
                  cells: [
                    <span key="connection" className="text-ink">{formatConnectionName(entry.db_connection_id, connections)}</span>,
                    <span key="target" className="font-semibold text-ink">{entry.database_name}.{entry.table_name}.{entry.column_name}</span>,
                    <span key="created" className="whitespace-nowrap text-muted">{formatDateTime(entry.created_at)}</span>,
                    <ActionCell
                      key="actions"
                      canWrite={canWrite}
                      onEdit={() => openWhitelistDrawer({ mode: 'edit', entry })}
                      onDelete={() => setPendingDelete({ kind: 'whitelist', id: entry.id })}
                      deleting={deletingKey === `whitelist:${entry.id}`}
                    />,
                  ],
                }))}
              />
            )}
          </SectionCard>

      {ruleDrawer ? createPortal(
        <DrawerLayout
          eyebrow="Global Masking Rule"
          title={ruleDrawer.mode === 'create' ? '新增規則' : `編輯 ${ruleDrawer.rule.column_name}`}
          onClose={closeRuleDrawer}
        >
          <form className="grid gap-4" onSubmit={handleRuleSubmit}>
            <Field label="Column Name">
              <input
                value={ruleForm.columnName}
                onChange={(event) => setRuleForm((current) => ({ ...current, columnName: event.target.value }))}
                className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                disabled={ruleSubmitting}
              />
            </Field>
            <Field label="Mask Mode">
              <select
                value={ruleForm.maskMode}
                onChange={(event) => setRuleForm((current) => ({ ...current, maskMode: normalizeMaskMode(event.target.value) }))}
                className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                disabled={ruleSubmitting}
              >
                {MASK_MODE_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </Field>
            {ruleDrawerError ? <InlineAlert>{ruleDrawerError}</InlineAlert> : null}
            <button
              type="submit"
              disabled={ruleSubmitting || !ruleForm.columnName.trim()}
              className="inline-flex h-10 items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {ruleSubmitting ? <Loader2 className="h-4 w-4 animate-spin" /> : ruleDrawer.mode === 'create' ? <Plus className="h-4 w-4" /> : <Pencil className="h-4 w-4" />}
              {ruleDrawer.mode === 'create' ? '建立規則' : '儲存變更'}
            </button>
          </form>
        </DrawerLayout>,
        document.body,
      ) : null}

      {whitelistDrawer ? createPortal(
        <DrawerLayout
          eyebrow="Unmask Whitelist"
          title={
            whitelistDrawer.mode === 'create'
              ? '新增白名單'
              : `編輯 ${whitelistDrawer.entry.database_name}.${whitelistDrawer.entry.table_name}.${whitelistDrawer.entry.column_name}`
          }
          onClose={closeWhitelistDrawer}
        >
          <form className="grid gap-4" onSubmit={handleWhitelistSubmit}>
            <Field label="Connection">
              <select
                value={whitelistForm.dbConnectionId}
                onChange={(event) =>
                  setWhitelistForm({
                    dbConnectionId: event.target.value,
                    databaseName: '',
                    tableName: '',
                    columnName: '',
                  })
                }
                className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                disabled={whitelistSubmitting}
              >
                <option value="">選擇 MySQL connection</option>
                {connections.map((connection) => (
                  <option key={connection.id} value={connection.id}>
                    {connection.name}
                  </option>
                ))}
              </select>
            </Field>

            {whitelistForm.dbConnectionId ? (
              <div className="grid gap-3 rounded-xl border border-border bg-panel-soft/60 px-3 py-3">
                <div className="flex items-center justify-between gap-3">
                  <p className="text-[12px] font-semibold text-ink">Metadata 選取</p>
                  {targetLoading ? <Loader2 className="h-4 w-4 animate-spin text-muted" /> : null}
                </div>

                <Field label="Database">
                  <select
                    value={whitelistForm.databaseName}
                    onChange={(event) =>
                      setWhitelistForm((current) => ({
                        ...current,
                        databaseName: event.target.value,
                        tableName: '',
                        columnName: '',
                      }))
                    }
                    className="h-10 rounded-lg border border-border bg-white px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                    disabled={whitelistSubmitting}
                  >
                    <option value="">選擇 database</option>
                    {databaseOptions.map((option) => (
                      <option key={option} value={option}>
                        {option}
                      </option>
                    ))}
                  </select>
                </Field>

                {whitelistForm.databaseName ? (
                  <Field label="Table">
                    <select
                      value={whitelistForm.tableName}
                      onChange={(event) =>
                        setWhitelistForm((current) => ({
                          ...current,
                          tableName: event.target.value,
                          columnName: '',
                        }))
                      }
                      className="h-10 rounded-lg border border-border bg-white px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                      disabled={whitelistSubmitting}
                    >
                      <option value="">選擇 table</option>
                      {tableOptions.map((option) => (
                        <option key={option} value={option}>
                          {option}
                        </option>
                      ))}
                    </select>
                  </Field>
                ) : null}

                {whitelistForm.tableName ? (
                  <Field label="Column">
                    <select
                      value={whitelistForm.columnName}
                      onChange={(event) => setWhitelistForm((current) => ({ ...current, columnName: event.target.value }))}
                      className="h-10 rounded-lg border border-border bg-white px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                      disabled={whitelistSubmitting}
                    >
                      <option value="">選擇 column</option>
                      {columnOptions.map((option) => (
                        <option key={option} value={option}>
                          {option}
                        </option>
                      ))}
                    </select>
                  </Field>
                ) : null}
              </div>
            ) : null}

            <Field label="Database Name">
              <input
                value={whitelistForm.databaseName}
                onChange={(event) => setWhitelistForm((current) => ({ ...current, databaseName: event.target.value }))}
                className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                disabled={whitelistSubmitting}
              />
            </Field>
            <Field label="Table Name">
              <input
                value={whitelistForm.tableName}
                onChange={(event) => setWhitelistForm((current) => ({ ...current, tableName: event.target.value }))}
                className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                disabled={whitelistSubmitting}
              />
            </Field>
            <Field label="Column Name">
              <input
                value={whitelistForm.columnName}
                onChange={(event) => setWhitelistForm((current) => ({ ...current, columnName: event.target.value }))}
                className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                disabled={whitelistSubmitting}
              />
            </Field>
            {whitelistDrawerError ? <InlineAlert>{whitelistDrawerError}</InlineAlert> : null}
            <button
              type="submit"
              disabled={whitelistSubmitting || !isWhitelistFormSubmittable(whitelistForm)}
              className="inline-flex h-10 items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {whitelistSubmitting ? <Loader2 className="h-4 w-4 animate-spin" /> : whitelistDrawer.mode === 'create' ? <Plus className="h-4 w-4" /> : <Pencil className="h-4 w-4" />}
              {whitelistDrawer.mode === 'create' ? '建立白名單' : '儲存變更'}
            </button>
          </form>
        </DrawerLayout>,
        document.body,
      ) : null}

      <ConfirmDialog
        open={pendingDelete !== null}
        title={pendingDelete?.kind === 'rule' ? '刪除 global masking rule' : '刪除 unmask whitelist'}
        description={pendingDelete?.kind === 'rule' ? '確認刪除這筆 global rule？' : '確認刪除這筆 whitelist？'}
        confirmLabel="確認刪除"
        tone="danger"
        loading={pendingDelete !== null && deletingKey === `${pendingDelete.kind}:${pendingDelete.id}`}
        onCancel={() => setPendingDelete(null)}
        onConfirm={() => void handleDelete()}
      />
    </div>
  )
}

function normalizeMaskMode(value: string): RuleForm['maskMode'] {
  if (value === 'partial' || value === 'hash') {
    return value
  }
  return 'full'
}

function isWhitelistFormSubmittable(form: WhitelistForm) {
  return Boolean(form.dbConnectionId && form.databaseName.trim() && form.tableName.trim() && form.columnName.trim())
}

function formatConnectionName(connectionID: number, connections: ConnectionOption[]) {
  return connections.find((connection) => connection.id === connectionID)?.name ?? `Connection #${connectionID}`
}

function SectionCard({
  title,
  description,
  icon,
  action,
  children,
}: {
  title: string
  description: string
  icon: ReactNode
  action?: ReactNode
  children: ReactNode
}) {
  return (
    <section className="rounded-xl border border-border bg-panel shadow-soft">
      <div className="border-b border-border/80 px-4 py-3">
        <div className="flex items-start justify-between gap-3">
          <div>
            <div className="flex items-center gap-2">
              {icon}
              <p className="text-[13px] font-semibold text-ink">{title}</p>
            </div>
            <p className="mt-1 text-[12px] text-muted">{description}</p>
          </div>
          {action}
        </div>
      </div>
      <div className="overflow-x-auto">{children}</div>
    </section>
  )
}

function ActionButton({ onClick, children }: { onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="inline-flex h-9 items-center justify-center gap-2 rounded-lg bg-brand px-3 text-[12px] font-bold text-white shadow-soft transition hover:bg-slate-800"
    >
      {children}
    </button>
  )
}

function CompactTable({
  headers,
  rows,
}: {
  headers: string[]
  rows: Array<{ key: string; cells: ReactNode[] }>
}) {
  return (
    <table className="min-w-full border-collapse">
      <thead className="bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
        <tr>
          {headers.map((header) => (
            <th key={header} className="px-3 py-2.5">{header}</th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => (
          <tr key={row.key} className="border-t border-border text-[12px] text-ink hover:bg-slate-50/70">
            {row.cells.map((cell, index) => (
              <td key={`${row.key}-${index}`} className="px-3 py-2 align-middle">
                {cell}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function ActionCell({
  canWrite,
  onEdit,
  onDelete,
  deleting,
}: {
  canWrite: boolean
  onEdit: () => void
  onDelete: () => void
  deleting: boolean
}) {
  if (!canWrite) {
    return <span className="text-muted">唯讀</span>
  }
  return (
    <div className="flex flex-nowrap items-center gap-1.5 whitespace-nowrap">
      <button
        type="button"
        onClick={onEdit}
        className="inline-flex h-8 items-center justify-center gap-1 rounded-md border border-border bg-panel-soft px-2.5 text-[12px] font-semibold text-ink transition hover:bg-page"
      >
        <Pencil className="h-3.5 w-3.5" />
        編輯
      </button>
      <button
        type="button"
        onClick={onDelete}
        disabled={deleting}
        className="inline-flex h-8 items-center justify-center gap-1 rounded-md border border-danger/20 bg-red-50 px-2.5 text-[12px] font-semibold text-danger transition hover:bg-red-100 disabled:opacity-50"
      >
        {deleting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
        刪除
      </button>
    </div>
  )
}

function DrawerLayout({
  eyebrow,
  title,
  onClose,
  children,
}: {
  eyebrow: string
  title: string
  onClose: () => void
  children: ReactNode
}) {
  return (
    <div className="fixed inset-0 z-[110] flex justify-end bg-slate-950/28 px-3 py-3 sm:px-4 sm:py-4">
      <div
        role="dialog"
        aria-modal="true"
        className="flex h-full w-full max-w-[620px] flex-col overflow-hidden rounded-xl border border-border bg-panel shadow-[0_22px_60px_rgba(15,23,42,0.18)]"
      >
        <div className="flex items-start justify-between border-b border-border/80 px-5 py-4">
          <div>
            <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">{eyebrow}</p>
            <h3 className="mt-1 text-[22px] font-bold tracking-[-0.03em] text-ink">{title}</h3>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-border bg-panel-soft text-muted transition hover:bg-page hover:text-ink"
            aria-label="關閉"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">{children}</div>
      </div>
    </div>
  )
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="grid gap-1.5 text-[12px] font-medium text-muted">
      {label}
      {children}
    </label>
  )
}

function Badge({ children }: { children: ReactNode }) {
  return (
    <span className="rounded-full border border-border bg-panel-soft px-2 py-0.5 text-[10px] font-semibold text-ink">
      {children}
    </span>
  )
}

function EmptyState({ message }: { message: string }) {
  return <div className="flex h-[180px] items-center justify-center text-[12px] text-muted">{message}</div>
}
