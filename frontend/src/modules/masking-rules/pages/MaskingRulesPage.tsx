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
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { PageIntro } from '@/shared/ui/PageIntro'
import { Pagination } from '@/shared/ui/Pagination'
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

const PAGE_SIZE = 20

export function MaskingRulesPage() {
  const { user } = useAuth()
  const { pushToast } = useToast()
  const canWrite = Boolean(user?.permissions.includes('masking_rules.write'))

  const [rules, setRules] = useState<MaskingRule[]>([])
  const [whitelist, setWhitelist] = useState<MaskingWhitelist[]>([])
  const [connections, setConnections] = useState<ConnectionOption[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [rulesOffset, setRulesOffset] = useState(0)
  const [whitelistOffset, setWhitelistOffset] = useState(0)

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
  const pagedRules = useMemo(() => sortedRules.slice(rulesOffset, rulesOffset + PAGE_SIZE), [rulesOffset, sortedRules])
  const pagedWhitelist = useMemo(
    () => sortedWhitelist.slice(whitelistOffset, whitelistOffset + PAGE_SIZE),
    [sortedWhitelist, whitelistOffset],
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

  useEffect(() => {
    if (rulesOffset > 0 && rulesOffset >= sortedRules.length) {
      setRulesOffset(Math.max(0, Math.floor((Math.max(sortedRules.length - 1, 0)) / PAGE_SIZE) * PAGE_SIZE))
    }
  }, [rulesOffset, sortedRules.length])

  useEffect(() => {
    if (whitelistOffset > 0 && whitelistOffset >= sortedWhitelist.length) {
      setWhitelistOffset(Math.max(0, Math.floor((Math.max(sortedWhitelist.length - 1, 0)) / PAGE_SIZE) * PAGE_SIZE))
    }
  }, [sortedWhitelist.length, whitelistOffset])

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
      setError(loadError instanceof ApiError ? loadError.message : 'Failed to load masking settings.')
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
        pushToast(`Global masking rule created: ${payload.column_name}`, 'success')
      } else {
        await patchMaskingRule(ruleDrawer.rule.id, payload)
        pushToast(`Global masking rule updated: ${payload.column_name}`, 'success')
      }
      await loadPage()
      closeRuleDrawer()
    } catch (submitError) {
      setRuleDrawerError(submitError instanceof ApiError ? submitError.message : 'Failed to save the global masking rule.')
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
        pushToast(`Whitelist created: ${payload.database_name}.${payload.table_name}.${payload.column_name}`, 'success')
      } else {
        await patchMaskingWhitelist(whitelistDrawer.entry.id, payload)
        pushToast(`Whitelist updated: ${payload.database_name}.${payload.table_name}.${payload.column_name}`, 'success')
      }
      await loadPage()
      closeWhitelistDrawer()
    } catch (submitError) {
      setWhitelistDrawerError(submitError instanceof ApiError ? submitError.message : 'Failed to save the whitelist entry.')
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
        pushToast('Global masking rule deleted', 'success')
      } else {
        await deleteMaskingWhitelist(pendingDelete.id)
        pushToast('Whitelist deleted', 'success')
      }
      await loadPage()
      setPendingDelete(null)
    } catch (deleteError) {
      setError(deleteError instanceof ApiError ? deleteError.message : 'Delete failed.')
    } finally {
      setDeletingKey(null)
    }
  }

  return (
    <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
      <PageIntro
        title="Masking Rules"
        description="Currently only MySQL is supported. Global rules manage column names and mask modes, while the whitelist is used to precisely exempt specific instance / database / table / column targets from false positives."
        actions={
          !canWrite ? (
            <div className="rounded-lg border border-border bg-white px-3 py-2 text-[12px] text-muted shadow-soft">
              This account only has `masking_rules.read`. You can view rules but cannot modify them.
            </div>
          ) : null
        }
      />

      {error ? <InlineAlert>{error}</InlineAlert> : null}

          <SectionCard
            title="Global Masking Rules"
            description="These are truly global rules. Each rule only stores `column_name` and `mask_mode`, and applies to all MySQL query and export results when matched."
            icon={<ShieldAlert className="h-4 w-4 text-accent" />}
            action={
              canWrite ? (
                <ActionButton onClick={() => openRuleDrawer({ mode: 'create' })}>
                  <Plus className="h-4 w-4" />
                  New Rule
                </ActionButton>
              ) : null
            }
          >
            {loading ? (
              <LoadingBlock message="Loading global masking rules..." className="m-4 min-h-[180px] rounded-xl border-border bg-panel" />
            ) : sortedRules.length === 0 ? (
              <EmptyState message="No global masking rules yet." />
            ) : (
              <CompactTable
                headers={['Column', 'Mode', 'Created', 'Actions']}
                rows={pagedRules.map((rule) => ({
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
            <Pagination
              offset={rulesOffset}
              pageSize={PAGE_SIZE}
              count={pagedRules.length}
              total={sortedRules.length}
              onChange={setRulesOffset}
            />
          </SectionCard>

          <SectionCard
            title="Unmask Whitelist"
            description="MySQL only. Each whitelist entry is bound to `connection -> database -> table -> column` so you can precisely exempt a target from an over-matched global rule."
            icon={<ShieldCheck className="h-4 w-4 text-accent" />}
            action={
              canWrite ? (
                <ActionButton onClick={() => openWhitelistDrawer({ mode: 'create' })}>
                  <Plus className="h-4 w-4" />
                  New Whitelist
                </ActionButton>
              ) : null
            }
          >
            {loading ? (
              <LoadingBlock message="Loading whitelist..." className="m-4 min-h-[180px] rounded-xl border-border bg-panel" />
            ) : sortedWhitelist.length === 0 ? (
              <EmptyState message="No whitelist entries yet." />
            ) : (
              <CompactTable
                headers={['Connection', 'Target', 'Created', 'Actions']}
                rows={pagedWhitelist.map((entry) => ({
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
            <Pagination
              offset={whitelistOffset}
              pageSize={PAGE_SIZE}
              count={pagedWhitelist.length}
              total={sortedWhitelist.length}
              onChange={setWhitelistOffset}
            />
          </SectionCard>

      {ruleDrawer ? createPortal(
        <DrawerLayout
          eyebrow="Global Masking Rule"
          title={ruleDrawer.mode === 'create' ? 'New Rule' : `Edit ${ruleDrawer.rule.column_name}`}
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
              <DropdownSelect
                ariaLabel="Mask Mode"
                value={ruleForm.maskMode}
                onChange={(value) => setRuleForm((current) => ({ ...current, maskMode: normalizeMaskMode(value) }))}
                disabled={ruleSubmitting}
                options={MASK_MODE_OPTIONS.map((option) => ({ value: option.value, label: option.label }))}
              />
            </Field>
            {ruleDrawerError ? <InlineAlert>{ruleDrawerError}</InlineAlert> : null}
            <button
              type="submit"
              disabled={ruleSubmitting || !ruleForm.columnName.trim()}
              className="inline-flex h-10 items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {ruleSubmitting ? <Loader2 className="h-4 w-4 animate-spin" /> : ruleDrawer.mode === 'create' ? <Plus className="h-4 w-4" /> : <Pencil className="h-4 w-4" />}
              {ruleDrawer.mode === 'create' ? 'Create Rule' : 'Save Changes'}
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
              ? 'New Whitelist'
              : `Edit ${whitelistDrawer.entry.database_name}.${whitelistDrawer.entry.table_name}.${whitelistDrawer.entry.column_name}`
          }
          onClose={closeWhitelistDrawer}
        >
          <form className="grid gap-4" onSubmit={handleWhitelistSubmit}>
            <Field label="Connection">
              <DropdownSelect
                ariaLabel="Connection"
                value={whitelistForm.dbConnectionId}
                onChange={(value) =>
                  setWhitelistForm({
                    dbConnectionId: value,
                    databaseName: '',
                    tableName: '',
                    columnName: '',
                  })
                }
                disabled={whitelistSubmitting}
                options={[
                  { value: '', label: 'Select MySQL connection' },
                  ...connections.map((connection) => ({ value: String(connection.id), label: connection.name })),
                ]}
              />
            </Field>

            {whitelistForm.dbConnectionId ? (
              <div className="grid gap-3 rounded-xl border border-border bg-panel-soft/60 px-3 py-3">
                <div className="flex items-center justify-between gap-3">
                  <p className="text-[12px] font-semibold text-ink">Metadata Selection</p>
                  {targetLoading ? <Loader2 className="h-4 w-4 animate-spin text-muted" /> : null}
                </div>

                <Field label="Database">
                  <DropdownSelect
                    ariaLabel="Database"
                    value={whitelistForm.databaseName}
                    onChange={(value) =>
                      setWhitelistForm((current) => ({
                        ...current,
                        databaseName: value,
                        tableName: '',
                        columnName: '',
                      }))
                    }
                    disabled={whitelistSubmitting}
                    options={[
                      { value: '', label: 'Select database' },
                      ...databaseOptions.map((option) => ({ value: option, label: option })),
                    ]}
                  />
                </Field>

                {whitelistForm.databaseName ? (
                  <Field label="Table">
                    <DropdownSelect
                      ariaLabel="Table"
                      value={whitelistForm.tableName}
                      onChange={(value) =>
                        setWhitelistForm((current) => ({
                          ...current,
                          tableName: value,
                          columnName: '',
                        }))
                      }
                      disabled={whitelistSubmitting}
                      options={[
                        { value: '', label: 'Select table' },
                        ...tableOptions.map((option) => ({ value: option, label: option })),
                      ]}
                    />
                  </Field>
                ) : null}

                {whitelistForm.tableName ? (
                  <Field label="Column">
                    <DropdownSelect
                      ariaLabel="Column"
                      value={whitelistForm.columnName}
                      onChange={(value) => setWhitelistForm((current) => ({ ...current, columnName: value }))}
                      disabled={whitelistSubmitting}
                      options={[
                        { value: '', label: 'Select column' },
                        ...columnOptions.map((option) => ({ value: option, label: option })),
                      ]}
                    />
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
              {whitelistDrawer.mode === 'create' ? 'Create Whitelist' : 'Save Changes'}
            </button>
          </form>
        </DrawerLayout>,
        document.body,
      ) : null}

      <ConfirmDialog
        open={pendingDelete !== null}
        title={pendingDelete?.kind === 'rule' ? 'Delete Global Masking Rule' : 'Delete Unmask Whitelist'}
        description={pendingDelete?.kind === 'rule' ? 'Delete this global masking rule?' : 'Delete this whitelist entry?'}
        confirmLabel="Confirm Delete"
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
    return <span className="text-muted">Read only</span>
  }
  return (
    <div className="flex flex-nowrap items-center gap-1.5 whitespace-nowrap">
      <button
        type="button"
        onClick={onEdit}
        className="inline-flex h-8 items-center justify-center gap-1 rounded-md border border-border bg-panel-soft px-2.5 text-[12px] font-semibold text-ink transition hover:bg-page"
      >
        <Pencil className="h-3.5 w-3.5" />
        Edit
      </button>
      <button
        type="button"
        onClick={onDelete}
        disabled={deleting}
        className="inline-flex h-8 items-center justify-center gap-1 rounded-md border border-danger/20 bg-red-50 px-2.5 text-[12px] font-semibold text-danger transition hover:bg-red-100 disabled:opacity-50"
      >
        {deleting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
        Delete
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
            aria-label="Close"
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
