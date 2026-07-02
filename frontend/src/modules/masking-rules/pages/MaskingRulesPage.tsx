import { useEffect, useMemo, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Loader2, Pencil, Plus, ShieldAlert, ShieldCheck, Trash2, X } from 'lucide-react'
import { Link } from 'react-router-dom'
import {
  createMaskingRule,
  listMaskingConnections,
  listMaskingMetadata,
  listMaskingMetadataColumns,
  createMaskingWhitelist,
  createRedisSensitiveKeyPrefix,
  deleteMaskingRule,
  deleteMaskingWhitelist,
  deleteRedisSensitiveKeyPrefix,
  listMaskingRules,
  listMaskingWhitelists,
  listRedisSensitiveKeyPrefixes,
  patchMaskingRule,
  patchMaskingWhitelist,
  patchRedisSensitiveKeyPrefix,
} from '@/modules/masking-rules/api'
import { useAuth } from '@/shared/auth/AuthContext'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import type { MaskingRule, MaskingWhitelist, RedisSensitiveKeyPrefix } from '@/shared/types/maskingRule'
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
  dbType: string
}

type RuleForm = {
  columnName: string
  matchType: 'exact' | 'regex'
  maskMode: 'full' | 'partial' | 'hash' | 'email' | 'fixed' | 'numeric' | 'datetime' | 'ip'
  maskConfig: string
}

type WhitelistForm = {
  dbConnectionId: string
  databaseName: string
  schemaName: string
  tableName: string
  columnName: string
}

type RedisPrefixForm = {
  dbConnectionId: string
  redisDBIndex: string
  keyPrefix: string
  reason: string
  isActive: boolean
}

type RuleDrawerState =
  | { mode: 'create' }
  | { mode: 'edit'; rule: MaskingRule }
  | null

type WhitelistDrawerState =
  | { mode: 'create' }
  | { mode: 'edit'; entry: MaskingWhitelist }
  | null

type RedisPrefixDrawerState =
  | { mode: 'create' }
  | { mode: 'edit'; prefix: RedisSensitiveKeyPrefix }
  | null

const EMPTY_RULE_FORM: RuleForm = {
  columnName: '',
  matchType: 'exact',
  maskMode: 'full',
  maskConfig: '{}',
}

const EMPTY_WHITELIST_FORM: WhitelistForm = {
  dbConnectionId: '',
  databaseName: '',
  schemaName: '',
  tableName: '',
  columnName: '',
}

const EMPTY_REDIS_PREFIX_FORM: RedisPrefixForm = {
  dbConnectionId: '',
  redisDBIndex: '',
  keyPrefix: '',
  reason: '',
  isActive: true,
}

const MASK_MODE_OPTIONS: Array<{ value: RuleForm['maskMode']; label: string }> = [
  { value: 'full', label: 'full' },
  { value: 'partial', label: 'partial' },
  { value: 'hash', label: 'hash' },
  { value: 'email', label: 'email' },
  { value: 'fixed', label: 'fixed' },
  { value: 'numeric', label: 'numeric' },
  { value: 'datetime', label: 'datetime' },
  { value: 'ip', label: 'ip' },
]

const MATCH_TYPE_OPTIONS: Array<{ value: RuleForm['matchType']; label: string }> = [
  { value: 'exact', label: 'exact' },
  { value: 'regex', label: 'regex' },
]

const MASK_MODE_EXAMPLES: Record<RuleForm['maskMode'], string> = {
  full: '{}',
  partial: '{\n  "keep_prefix": 3,\n  "keep_suffix": 4,\n  "mask_char": "*"\n}',
  hash: '{}',
  email: '{\n  "keep_local_prefix": 1,\n  "keep_domain": true,\n  "replacement": "****"\n}',
  fixed: '{\n  "value": "[REDACTED]"\n}',
  numeric: '{\n  "operation": "round",\n  "decimals": 0\n}',
  datetime: '{\n  "granularity": "day"\n}',
  ip: '{\n  "keep_segments": 2\n}',
}

const PAGE_SIZE = 20

export function MaskingRulesPage() {
  const { user } = useAuth()
  const { pushToast } = useToast()
  const canWrite = Boolean(user?.permissions.includes('masking_rules.write'))

  const [rules, setRules] = useState<MaskingRule[]>([])
  const [whitelist, setWhitelist] = useState<MaskingWhitelist[]>([])
  const [redisPrefixes, setRedisPrefixes] = useState<RedisSensitiveKeyPrefix[]>([])
  const [connections, setConnections] = useState<ConnectionOption[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [rulesOffset, setRulesOffset] = useState(0)
  const [whitelistOffset, setWhitelistOffset] = useState(0)
  const [redisPrefixOffset, setRedisPrefixOffset] = useState(0)

  const [ruleDrawer, setRuleDrawer] = useState<RuleDrawerState>(null)
  const [ruleForm, setRuleForm] = useState<RuleForm>(EMPTY_RULE_FORM)
  const [ruleSubmitting, setRuleSubmitting] = useState(false)
  const [ruleDrawerError, setRuleDrawerError] = useState('')

  const [whitelistDrawer, setWhitelistDrawer] = useState<WhitelistDrawerState>(null)
  const [whitelistForm, setWhitelistForm] = useState<WhitelistForm>(EMPTY_WHITELIST_FORM)
  const [whitelistSubmitting, setWhitelistSubmitting] = useState(false)
  const [whitelistDrawerError, setWhitelistDrawerError] = useState('')
  const [databaseOptions, setDatabaseOptions] = useState<string[]>([])
  const [schemaOptions, setSchemaOptions] = useState<string[]>([])
  const [tableOptions, setTableOptions] = useState<string[]>([])
  const [columnOptions, setColumnOptions] = useState<string[]>([])
  const [targetLoading, setTargetLoading] = useState(false)

  const [redisPrefixDrawer, setRedisPrefixDrawer] = useState<RedisPrefixDrawerState>(null)
  const [redisPrefixForm, setRedisPrefixForm] = useState<RedisPrefixForm>(EMPTY_REDIS_PREFIX_FORM)
  const [redisPrefixSubmitting, setRedisPrefixSubmitting] = useState(false)
  const [redisPrefixDrawerError, setRedisPrefixDrawerError] = useState('')

  const [pendingDelete, setPendingDelete] = useState<{ kind: 'rule' | 'whitelist' | 'redisPrefix'; id: number } | null>(null)
  const [deletingKey, setDeletingKey] = useState<string | null>(null)

  const sqlConnections = useMemo(
    () => connections.filter((connection) => connection.dbType === 'mysql' || connection.dbType === 'postgres' || connection.dbType === 'postgresql'),
    [connections],
  )
  const redisConnections = useMemo(() => connections.filter((connection) => connection.dbType === 'redis'), [connections])
  const selectedWhitelistConnection = useMemo(
    () => connections.find((connection) => String(connection.id) === whitelistForm.dbConnectionId) ?? null,
    [connections, whitelistForm.dbConnectionId],
  )
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
  const sortedRedisPrefixes = useMemo(
    () =>
      [...redisPrefixes].sort((left, right) => {
        const leftKey = `${left.db_connection_id}:${left.redis_db_index ?? '*'}:${left.key_prefix}`
        const rightKey = `${right.db_connection_id}:${right.redis_db_index ?? '*'}:${right.key_prefix}`
        return leftKey.localeCompare(rightKey)
      }),
    [redisPrefixes],
  )
  const pagedRules = useMemo(() => sortedRules.slice(rulesOffset, rulesOffset + PAGE_SIZE), [rulesOffset, sortedRules])
  const pagedWhitelist = useMemo(
    () => sortedWhitelist.slice(whitelistOffset, whitelistOffset + PAGE_SIZE),
    [sortedWhitelist, whitelistOffset],
  )
  const pagedRedisPrefixes = useMemo(
    () => sortedRedisPrefixes.slice(redisPrefixOffset, redisPrefixOffset + PAGE_SIZE),
    [redisPrefixOffset, sortedRedisPrefixes],
  )

  useEffect(() => {
    void loadPage()
  }, [])

  useEffect(() => {
    if (!whitelistDrawer || !whitelistForm.dbConnectionId) {
      setDatabaseOptions([])
      setSchemaOptions([])
      setTableOptions([])
      setColumnOptions([])
      return
    }
    void loadDatabases(Number(whitelistForm.dbConnectionId))
  }, [whitelistDrawer, whitelistForm.dbConnectionId])

  useEffect(() => {
    if (!whitelistDrawer || !whitelistForm.dbConnectionId || !whitelistForm.databaseName) {
      setSchemaOptions([])
      setTableOptions([])
      setColumnOptions([])
      return
    }
    if (selectedWhitelistConnection?.dbType === 'postgres' || selectedWhitelistConnection?.dbType === 'postgresql') {
      void loadSchemas(Number(whitelistForm.dbConnectionId), whitelistForm.databaseName)
      return
    }
    void loadTables(Number(whitelistForm.dbConnectionId), whitelistForm.databaseName, '')
  }, [whitelistDrawer, whitelistForm.dbConnectionId, whitelistForm.databaseName])

  useEffect(() => {
    if (
      !whitelistDrawer ||
      !whitelistForm.dbConnectionId ||
      !whitelistForm.databaseName ||
      !(selectedWhitelistConnection?.dbType === 'postgres' || selectedWhitelistConnection?.dbType === 'postgresql') ||
      !whitelistForm.schemaName
    ) {
      if (selectedWhitelistConnection?.dbType === 'postgres' || selectedWhitelistConnection?.dbType === 'postgresql') {
        setTableOptions([])
        setColumnOptions([])
      }
      return
    }
    void loadTables(Number(whitelistForm.dbConnectionId), whitelistForm.databaseName, whitelistForm.schemaName)
  }, [whitelistDrawer, whitelistForm.dbConnectionId, whitelistForm.databaseName, whitelistForm.schemaName])

  useEffect(() => {
    if (!whitelistDrawer || !whitelistForm.dbConnectionId || !whitelistForm.databaseName || !whitelistForm.tableName) {
      setColumnOptions([])
      return
    }
    const schemaName = selectedWhitelistConnection?.dbType === 'postgres' || selectedWhitelistConnection?.dbType === 'postgresql'
      ? whitelistForm.schemaName
      : whitelistForm.databaseName
    if (!schemaName) {
      setColumnOptions([])
      return
    }
    void loadColumns(Number(whitelistForm.dbConnectionId), whitelistForm.databaseName, schemaName, whitelistForm.tableName)
  }, [whitelistDrawer, whitelistForm.dbConnectionId, whitelistForm.databaseName, whitelistForm.schemaName, whitelistForm.tableName])

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

  useEffect(() => {
    if (redisPrefixOffset > 0 && redisPrefixOffset >= sortedRedisPrefixes.length) {
      setRedisPrefixOffset(Math.max(0, Math.floor((Math.max(sortedRedisPrefixes.length - 1, 0)) / PAGE_SIZE) * PAGE_SIZE))
    }
  }, [redisPrefixOffset, sortedRedisPrefixes.length])

  async function loadPage() {
    setLoading(true)
    setError('')
    try {
      const [rulesResponse, whitelistResponse, redisPrefixResponse, connectionsResponse] = await Promise.all([
        listMaskingRules(),
        listMaskingWhitelists(),
        listRedisSensitiveKeyPrefixes(),
        listMaskingConnections(),
      ])
      setRules(rulesResponse.rules)
      setWhitelist(whitelistResponse.whitelist)
      setRedisPrefixes(redisPrefixResponse.prefixes)
      setConnections(
        connectionsResponse.connections.map((connection) => ({ id: connection.id, name: connection.name, dbType: connection.db_type })),
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
      const response = await listMaskingMetadata(connectionId)
      setDatabaseOptions(response.items.map((item) => item.name))
    } catch {
      setDatabaseOptions([])
    } finally {
      setTargetLoading(false)
    }
  }

  async function loadSchemas(connectionId: number, databaseName: string) {
    setTargetLoading(true)
    try {
      const response = await listMaskingMetadata(connectionId, { database: databaseName })
      setSchemaOptions(response.items.map((item) => item.name))
    } catch {
      setSchemaOptions([])
    } finally {
      setTargetLoading(false)
    }
  }

  async function loadTables(connectionId: number, databaseName: string, schemaName: string) {
    setTargetLoading(true)
    try {
      const response = await listMaskingMetadata(connectionId, schemaName ? { database: databaseName, schema: schemaName } : { database: databaseName })
      setTableOptions(response.items.map((item) => item.name))
    } catch {
      setTableOptions([])
    } finally {
      setTargetLoading(false)
    }
  }

  async function loadColumns(connectionId: number, databaseName: string, schemaName: string, tableName: string) {
    setTargetLoading(true)
    try {
      const response = await listMaskingMetadataColumns(connectionId, schemaName, tableName, databaseName)
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
            matchType: state.rule.match_type,
            maskMode: state.rule.mask_mode,
            maskConfig: JSON.stringify(state.rule.mask_config ?? {}, null, 2),
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
            schemaName: state.entry.schema_name ?? '',
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
    setSchemaOptions([])
    setTableOptions([])
    setColumnOptions([])
  }

  function openRedisPrefixDrawer(state: RedisPrefixDrawerState) {
    setRedisPrefixDrawer(state)
    setRedisPrefixDrawerError('')
    setRedisPrefixForm(
      state?.mode === 'edit'
        ? {
            dbConnectionId: String(state.prefix.db_connection_id),
            redisDBIndex: state.prefix.redis_db_index === null || state.prefix.redis_db_index === undefined ? '' : String(state.prefix.redis_db_index),
            keyPrefix: state.prefix.key_prefix,
            reason: state.prefix.reason ?? '',
            isActive: state.prefix.is_active,
          }
        : EMPTY_REDIS_PREFIX_FORM,
    )
  }

  function closeRedisPrefixDrawer() {
    setRedisPrefixDrawer(null)
    setRedisPrefixDrawerError('')
    setRedisPrefixSubmitting(false)
    setRedisPrefixForm(EMPTY_REDIS_PREFIX_FORM)
  }

  async function handleRuleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!ruleDrawer) {
      return
    }

    setRuleSubmitting(true)
    setRuleDrawerError('')
    try {
      const parsedMaskConfig = parseMaskConfig(ruleForm.maskConfig)
      const payload = {
        column_name: ruleForm.columnName.trim(),
        match_type: ruleForm.matchType,
        mask_mode: ruleForm.maskMode,
        mask_config: parsedMaskConfig,
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
      setRuleDrawerError(submitError instanceof ApiError ? submitError.message : submitError instanceof Error ? submitError.message : 'Failed to save the global masking rule.')
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
        schema_name: whitelistForm.schemaName.trim(),
        table_name: whitelistForm.tableName.trim(),
        column_name: whitelistForm.columnName.trim(),
      }
      if (whitelistDrawer.mode === 'create') {
        await createMaskingWhitelist(payload)
        pushToast(`Whitelist created: ${formatWhitelistTarget(payload)}`, 'success')
      } else {
        await patchMaskingWhitelist(whitelistDrawer.entry.id, payload)
        pushToast(`Whitelist updated: ${formatWhitelistTarget(payload)}`, 'success')
      }
      await loadPage()
      closeWhitelistDrawer()
    } catch (submitError) {
      setWhitelistDrawerError(submitError instanceof ApiError ? submitError.message : 'Failed to save the whitelist entry.')
    } finally {
      setWhitelistSubmitting(false)
    }
  }

  async function handleRedisPrefixSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!redisPrefixDrawer) {
      return
    }

    setRedisPrefixSubmitting(true)
    setRedisPrefixDrawerError('')
    try {
      const payload = {
        db_connection_id: Number(redisPrefixForm.dbConnectionId),
        redis_db_index: redisPrefixForm.redisDBIndex.trim() === '' ? null : Number(redisPrefixForm.redisDBIndex),
        key_prefix: redisPrefixForm.keyPrefix.trim(),
        reason: redisPrefixForm.reason.trim() || null,
        is_active: redisPrefixForm.isActive,
      }
      if (redisPrefixDrawer.mode === 'create') {
        await createRedisSensitiveKeyPrefix(payload)
        pushToast(`Redis sensitive prefix created: ${payload.key_prefix}`, 'success')
      } else {
        await patchRedisSensitiveKeyPrefix(redisPrefixDrawer.prefix.id, payload)
        pushToast(`Redis sensitive prefix updated: ${payload.key_prefix}`, 'success')
      }
      await loadPage()
      closeRedisPrefixDrawer()
    } catch (submitError) {
      setRedisPrefixDrawerError(submitError instanceof ApiError ? submitError.message : 'Failed to save the Redis sensitive key prefix.')
    } finally {
      setRedisPrefixSubmitting(false)
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
      } else if (pendingDelete.kind === 'whitelist') {
        await deleteMaskingWhitelist(pendingDelete.id)
        pushToast('Whitelist deleted', 'success')
      } else {
        await deleteRedisSensitiveKeyPrefix(pendingDelete.id)
        pushToast('Redis sensitive prefix deleted', 'success')
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
        description="SQL masking supports MySQL and PostgreSQL column results. Redis is managed separately with sensitive key prefixes: key names and metadata may be visible, but value/content reads are blocked for matching prefixes."
        actions={
          <>
            <Link
              to="/masking-rules/dsl-guide"
              className="inline-flex h-10 items-center justify-center rounded-lg border border-border bg-white px-4 text-[13px] font-semibold text-ink transition hover:bg-panel-soft"
            >
              DSL Guide
            </Link>
            {!canWrite ? (
              <div className="rounded-lg border border-border bg-white px-3 py-2 text-[12px] text-muted shadow-soft">
                This account only has `masking_rules.read`. You can view rules but cannot modify them.
              </div>
            ) : null}
          </>
        }
      />

      {error ? <InlineAlert>{error}</InlineAlert> : null}

          <SectionCard
            title="Global Masking Rules"
            description="SQL column masking for MySQL and PostgreSQL. Each rule stores `column pattern`, `match_type`, `mask_mode`, and JSON `mask_config`, so DBA can map field patterns to masking behavior without code changes."
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
                headers={['Pattern', 'Match', 'Mode', 'Config', 'Created', 'Actions']}
                rows={pagedRules.map((rule) => ({
                  key: `rule-${rule.id}`,
                  cells: [
                    <span key="column" className="font-semibold text-ink">{rule.column_name}</span>,
                    <Badge key="match">{rule.match_type}</Badge>,
                    <Badge key="mode">{rule.mask_mode}</Badge>,
                    <code key="config" className="max-w-[360px] whitespace-pre-wrap break-all text-[11px] text-muted">
                      {JSON.stringify(rule.mask_config ?? {}, null, 2)}
                    </code>,
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
            description="MySQL and PostgreSQL. Each whitelist entry is bound to a concrete database / schema / table / column target so you can precisely exempt false positives from global SQL masking."
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
                    <span key="target" className="font-semibold text-ink">{formatWhitelistTarget(entry)}</span>,
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

          <SectionCard
            title="Redis Sensitive Key Prefixes"
            description="Redis policy is managed by key prefix. Matching key names and metadata remain visible, while value/content commands are blocked before execution."
            icon={<ShieldAlert className="h-4 w-4 text-accent" />}
            action={
              canWrite ? (
                <ActionButton onClick={() => openRedisPrefixDrawer({ mode: 'create' })}>
                  <Plus className="h-4 w-4" />
                  New Prefix
                </ActionButton>
              ) : null
            }
          >
            {loading ? (
              <LoadingBlock message="Loading Redis sensitive prefixes..." className="m-4 min-h-[180px] rounded-xl border-border bg-panel" />
            ) : sortedRedisPrefixes.length === 0 ? (
              <EmptyState message="No Redis sensitive key prefixes yet." />
            ) : (
              <CompactTable
                headers={['Connection', 'DB', 'Prefix', 'Status', 'Reason', 'Created', 'Actions']}
                rows={pagedRedisPrefixes.map((prefix) => ({
                  key: `redis-prefix-${prefix.id}`,
                  cells: [
                    <span key="connection" className="text-ink">{formatConnectionName(prefix.db_connection_id, connections)}</span>,
                    <Badge key="db">{prefix.redis_db_index === null || prefix.redis_db_index === undefined ? 'All' : prefix.redis_db_index}</Badge>,
                    <span key="prefix" className="font-mono text-[12px] font-semibold text-ink">{prefix.key_prefix}</span>,
                    <Badge key="status">{prefix.is_active ? 'active' : 'inactive'}</Badge>,
                    <span key="reason" className="max-w-[320px] truncate text-muted">{prefix.reason || '-'}</span>,
                    <span key="created" className="whitespace-nowrap text-muted">{formatDateTime(prefix.created_at)}</span>,
                    <ActionCell
                      key="actions"
                      canWrite={canWrite}
                      onEdit={() => openRedisPrefixDrawer({ mode: 'edit', prefix })}
                      onDelete={() => setPendingDelete({ kind: 'redisPrefix', id: prefix.id })}
                      deleting={deletingKey === `redisPrefix:${prefix.id}`}
                    />,
                  ],
                }))}
              />
            )}
            <Pagination
              offset={redisPrefixOffset}
              pageSize={PAGE_SIZE}
              count={pagedRedisPrefixes.length}
              total={sortedRedisPrefixes.length}
              onChange={setRedisPrefixOffset}
            />
          </SectionCard>

      {ruleDrawer ? createPortal(
        <DrawerLayout
          eyebrow="Global Masking Rule"
          title={ruleDrawer.mode === 'create' ? 'New Rule' : `Edit ${ruleDrawer.rule.column_name}`}
          onClose={closeRuleDrawer}
        >
          <form className="grid gap-4" onSubmit={handleRuleSubmit}>
            <Field label="Column Pattern">
              <input
                value={ruleForm.columnName}
                onChange={(event) => setRuleForm((current) => ({ ...current, columnName: event.target.value }))}
                className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                disabled={ruleSubmitting}
              />
            </Field>
            <Field label="Match Type">
              <DropdownSelect
                ariaLabel="Match Type"
                value={ruleForm.matchType}
                onChange={(value) => setRuleForm((current) => ({ ...current, matchType: normalizeMatchType(value) }))}
                disabled={ruleSubmitting}
                options={MATCH_TYPE_OPTIONS.map((option) => ({ value: option.value, label: option.label }))}
              />
            </Field>
            <Field label="Mask Mode">
              <DropdownSelect
                ariaLabel="Mask Mode"
                value={ruleForm.maskMode}
                onChange={(value) =>
                  setRuleForm((current) => ({
                    ...current,
                    maskMode: normalizeMaskMode(value),
                    maskConfig: current.maskConfig.trim() === '' || current.maskConfig === '{}' || current.maskConfig === MASK_MODE_EXAMPLES[current.maskMode]
                      ? MASK_MODE_EXAMPLES[normalizeMaskMode(value)]
                      : current.maskConfig,
                  }))}
                disabled={ruleSubmitting}
                options={MASK_MODE_OPTIONS.map((option) => ({ value: option.value, label: option.label }))}
              />
            </Field>
            <Field label="Mask Config JSON">
              <textarea
                value={ruleForm.maskConfig}
                onChange={(event) => setRuleForm((current) => ({ ...current, maskConfig: event.target.value }))}
                className="min-h-40 rounded-lg border border-border bg-panel-soft px-3 py-3 font-mono text-[12px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                disabled={ruleSubmitting}
                placeholder={MASK_MODE_EXAMPLES[ruleForm.maskMode]}
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
              : `Edit ${formatWhitelistTarget(whitelistDrawer.entry)}`
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
                    schemaName: '',
                    tableName: '',
                    columnName: '',
                  })
                }
                disabled={whitelistSubmitting}
                options={[
                  { value: '', label: 'Select SQL connection' },
                  ...sqlConnections.map((connection) => ({ value: String(connection.id), label: connection.name })),
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
                        schemaName: '',
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

                {whitelistForm.databaseName && (selectedWhitelistConnection?.dbType === 'postgres' || selectedWhitelistConnection?.dbType === 'postgresql') ? (
                  <Field label="Schema">
                    <DropdownSelect
                      ariaLabel="Schema"
                      value={whitelistForm.schemaName}
                      onChange={(value) =>
                        setWhitelistForm((current) => ({
                          ...current,
                          schemaName: value,
                          tableName: '',
                          columnName: '',
                        }))
                      }
                      disabled={whitelistSubmitting}
                      options={[
                        { value: '', label: 'Select schema' },
                        ...schemaOptions.map((option) => ({ value: option, label: option })),
                      ]}
                    />
                  </Field>
                ) : null}

                {whitelistForm.databaseName && (!(selectedWhitelistConnection?.dbType === 'postgres' || selectedWhitelistConnection?.dbType === 'postgresql') || whitelistForm.schemaName) ? (
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
            {(selectedWhitelistConnection?.dbType === 'postgres' || selectedWhitelistConnection?.dbType === 'postgresql') ? (
              <Field label="Schema Name">
                <input
                  value={whitelistForm.schemaName}
                  onChange={(event) => setWhitelistForm((current) => ({ ...current, schemaName: event.target.value }))}
                  className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                  disabled={whitelistSubmitting}
                />
              </Field>
            ) : null}
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
              disabled={whitelistSubmitting || !isWhitelistFormSubmittable(whitelistForm, selectedWhitelistConnection)}
              className="inline-flex h-10 items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {whitelistSubmitting ? <Loader2 className="h-4 w-4 animate-spin" /> : whitelistDrawer.mode === 'create' ? <Plus className="h-4 w-4" /> : <Pencil className="h-4 w-4" />}
              {whitelistDrawer.mode === 'create' ? 'Create Whitelist' : 'Save Changes'}
            </button>
          </form>
        </DrawerLayout>,
        document.body,
      ) : null}

      {redisPrefixDrawer ? createPortal(
        <DrawerLayout
          eyebrow="Redis Sensitive Prefix"
          title={redisPrefixDrawer.mode === 'create' ? 'New Redis Prefix' : `Edit ${redisPrefixDrawer.prefix.key_prefix}`}
          onClose={closeRedisPrefixDrawer}
        >
          <form className="grid gap-4" onSubmit={handleRedisPrefixSubmit}>
            <Field label="Redis Connection">
              <DropdownSelect
                ariaLabel="Redis Connection"
                value={redisPrefixForm.dbConnectionId}
                onChange={(value) => setRedisPrefixForm((current) => ({ ...current, dbConnectionId: value }))}
                disabled={redisPrefixSubmitting}
                options={[
                  { value: '', label: 'Select Redis connection' },
                  ...redisConnections.map((connection) => ({ value: String(connection.id), label: connection.name })),
                ]}
              />
            </Field>
            <Field label="Redis DB Index">
              <input
                value={redisPrefixForm.redisDBIndex}
                onChange={(event) => setRedisPrefixForm((current) => ({ ...current, redisDBIndex: event.target.value }))}
                className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                disabled={redisPrefixSubmitting}
                inputMode="numeric"
                placeholder="Blank = all DB indexes"
              />
            </Field>
            <Field label="Key Prefix">
              <input
                value={redisPrefixForm.keyPrefix}
                onChange={(event) => setRedisPrefixForm((current) => ({ ...current, keyPrefix: event.target.value }))}
                className="h-10 rounded-lg border border-border bg-panel-soft px-3 font-mono text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                disabled={redisPrefixSubmitting}
                placeholder="session:"
              />
            </Field>
            <Field label="Reason">
              <input
                value={redisPrefixForm.reason}
                onChange={(event) => setRedisPrefixForm((current) => ({ ...current, reason: event.target.value }))}
                className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                disabled={redisPrefixSubmitting}
              />
            </Field>
            <label className="flex items-center gap-2 text-[12px] font-medium text-ink">
              <input
                type="checkbox"
                checked={redisPrefixForm.isActive}
                onChange={(event) => setRedisPrefixForm((current) => ({ ...current, isActive: event.target.checked }))}
                disabled={redisPrefixSubmitting}
                className="h-4 w-4 rounded border-border text-brand focus:ring-accent"
              />
              Active
            </label>
            {redisPrefixDrawerError ? <InlineAlert>{redisPrefixDrawerError}</InlineAlert> : null}
            <button
              type="submit"
              disabled={redisPrefixSubmitting || !isRedisPrefixFormSubmittable(redisPrefixForm)}
              className="inline-flex h-10 items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {redisPrefixSubmitting ? <Loader2 className="h-4 w-4 animate-spin" /> : redisPrefixDrawer.mode === 'create' ? <Plus className="h-4 w-4" /> : <Pencil className="h-4 w-4" />}
              {redisPrefixDrawer.mode === 'create' ? 'Create Prefix' : 'Save Changes'}
            </button>
          </form>
        </DrawerLayout>,
        document.body,
      ) : null}

      <ConfirmDialog
        open={pendingDelete !== null}
        title={deleteDialogTitle(pendingDelete?.kind)}
        description={deleteDialogDescription(pendingDelete?.kind)}
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
  if (value === 'partial' || value === 'hash' || value === 'email' || value === 'fixed' || value === 'numeric' || value === 'datetime' || value === 'ip') {
    return value
  }
  return 'full'
}

function normalizeMatchType(value: string): RuleForm['matchType'] {
  return value === 'regex' ? 'regex' : 'exact'
}

function parseMaskConfig(value: string) {
  const trimmed = value.trim()
  if (trimmed === '') {
    return {}
  }

  const parsed = JSON.parse(trimmed) as unknown
  if (parsed === null || Array.isArray(parsed) || typeof parsed !== 'object') {
    throw new Error('Mask Config JSON must be an object.')
  }
  return parsed as Record<string, unknown>
}

function isWhitelistFormSubmittable(form: WhitelistForm, connection?: ConnectionOption | null) {
  const isPostgres = connection?.dbType === 'postgres' || connection?.dbType === 'postgresql'
  return Boolean(
    form.dbConnectionId &&
    form.databaseName.trim() &&
    (!isPostgres || form.schemaName.trim()) &&
    form.tableName.trim() &&
    form.columnName.trim(),
  )
}

function isRedisPrefixFormSubmittable(form: RedisPrefixForm) {
  if (!form.dbConnectionId || !form.keyPrefix.trim()) {
    return false
  }
  if (form.redisDBIndex.trim() === '') {
    return true
  }
  const dbIndex = Number(form.redisDBIndex)
  return Number.isInteger(dbIndex) && dbIndex >= 0 && dbIndex <= 15
}

function formatConnectionName(connectionID: number, connections: ConnectionOption[]) {
  return connections.find((connection) => connection.id === connectionID)?.name ?? `Connection #${connectionID}`
}

function formatWhitelistTarget(entry: { database_name: string; schema_name?: string; table_name: string; column_name: string }) {
  const schemaName = entry.schema_name?.trim()
  return [entry.database_name, schemaName, entry.table_name, entry.column_name].filter(Boolean).join('.')
}

function deleteDialogTitle(kind?: 'rule' | 'whitelist' | 'redisPrefix') {
  switch (kind) {
    case 'rule':
      return 'Delete Global Masking Rule'
    case 'whitelist':
      return 'Delete Unmask Whitelist'
    case 'redisPrefix':
      return 'Delete Redis Sensitive Prefix'
    default:
      return 'Delete'
  }
}

function deleteDialogDescription(kind?: 'rule' | 'whitelist' | 'redisPrefix') {
  switch (kind) {
    case 'rule':
      return 'Delete this global masking rule?'
    case 'whitelist':
      return 'Delete this whitelist entry?'
    case 'redisPrefix':
      return 'Delete this Redis sensitive key prefix?'
    default:
      return 'Delete this item?'
  }
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
