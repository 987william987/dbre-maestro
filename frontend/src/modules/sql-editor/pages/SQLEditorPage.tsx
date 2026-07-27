import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import CodeMirror from '@uiw/react-codemirror'
import { javascript } from '@codemirror/lang-javascript'
import { MySQL, PostgreSQL, StandardSQL, sql, type SQLNamespace } from '@codemirror/lang-sql'
import { autocompletion, type Completion, type CompletionSource } from '@codemirror/autocomplete'
import { Prec } from '@codemirror/state'
import { keymap } from '@codemirror/view'
import { format as formatSQL, type SqlLanguage } from 'sql-formatter'
import {
  Database,
  Download,
  Filter,
  FileClock,
  Folder,
  FolderTree,
  History,
  Layers3,
  Play,
  Plus,
  RefreshCw,
  Star,
  StarOff,
  Table2,
  Trash2,
  X,
  Workflow,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { ApiError } from '@/shared/api/client'
import { useAuth } from '@/shared/auth/AuthContext'
import { formatDateTime } from '@/shared/lib/format'
import { useDebouncedValue } from '@/shared/lib/useDebouncedValue'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { MetadataColumn, MetadataDefinition, MetadataItem, QueryHistoryEntry, QueryResult, SavedQuery } from '@/shared/types/sqlEditor'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { AttentionPulse } from '@/shared/ui/AttentionPulse'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { Pagination } from '@/shared/ui/Pagination'
import { useToast } from '@/shared/ui/ToastContext'
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog'
import { DataTable, DataTableBody, DataTableCell, DataTableHead, DataTableHeaderCell, DataTableRow } from '@/shared/ui/DataTable'
import { SearchInput } from '@/shared/ui/SearchInput'
import { useNavigate } from 'react-router-dom'
import { createExportRequest } from '@/modules/exports/api'
import {
  createSavedQuery,
  createSensitiveAccessTicket,
  deleteSavedQuery,
  executeQuery,
  getQueryConstraints,
  listQueryConnections,
  listMetadata,
  listMetadataColumns,
  listMetadataDefinition,
  listMetadataSearchIndex,
  listQueryHistory,
  listSavedQueries,
} from '@/modules/sql-editor/api'
import {
  getSQLEditorWorkspaceSnapshot,
  saveSQLEditorWorkspaceSnapshot,
} from '@/modules/sql-editor/workspaceMemory'

type EditorTab = {
  id: string
  title: string
  connectionId: number | null
  database: string
  schema: string
  selectedTable: MetadataItem | null
  metadataError: string
  explorerNodes: AssetTreeNode[]
  searchTreeNodes: AssetTreeNode[]
  explorerSearch: string
  searchingAssets: boolean
  searchIndexStatus: 'idle' | 'loading' | 'ready' | 'error' | 'stale'
  searchIndexItems: MetadataItem[]
  searchIndexTruncated: boolean
  searchIndexError: string
  assetPickerOpen: boolean
  assetPickerSearch: string
  resultView: 'result' | 'vertical' | 'object-meta' | 'history' | 'saved'
  objectMetaTab: 'columns' | 'definition'
  columns: MetadataColumn[]
  definition: MetadataDefinition | null
  columnsLoading: boolean
  definitionLoading: boolean
  columnFilterOpen: boolean
  visibleColumnIndexes: number[] | null
  selectedSQL: string
  sensitiveAccessDuration: number
  resultPage: number
  sql: string
  executedSQL: string | null
  executedConnectionId: number | null
  executedDatabase: string
  executedSchema: string
  result: QueryResult | null
  error: string
  lastRunAt: string | null
}

type AssetTreeNode = {
  id: string
  kind: 'connection' | 'database' | 'schema' | 'table' | 'redis_db'
  connectionId: number
  label: string
  database?: string
  schema?: string
  meta?: string
  active: boolean
  selectable: boolean
  expanded: boolean
  loaded: boolean
  loading: boolean
  item?: MetadataItem
  children: AssetTreeNode[]
}

type QueryRequestConfirmState = {
  kind: 'export' | 'sensitive-access'
  tabID: string
  connectionId: number
  connectionName: string
  connectionType: string
  database: string
  schema: string
  contextSchema: string
  tableName: string
  sql: string
  queryContextToken: string
  sensitiveAccessDuration: number
}

type SensitiveAccessDurationDialogState = {
  tabID: string
  value: string
  error: string
}

type SaveQueryDialogState = {
  tabID: string
  label: string
  error: string
  connectionId: number
  database: string
  schema: string
  redisDbIndex?: number
  sql: string
}

type SQLEditorWorkspaceDraft = {
  tabs: EditorTab[]
  activeTabId: string
  editorHeights: Record<string, string>
}

const DEFAULT_SQL = 'SELECT 1;'
const HISTORY_LIMIT = 20
const SAVED_QUERY_LIMIT = 20
const SAVED_QUERY_LABEL_MAX_LENGTH = 255
const MAX_EDITOR_TABS = 10
const SEARCH_INDEX_MIN_KEYWORD_LENGTH = 3
const EDITOR_BASE_VISIBLE_LINES = 12
const EDITOR_MAX_HEIGHT = 840
type QueryConstraints = {
  default_limit: number
  max_limit: number
  app_timeout_seconds: number
  mysql_max_execution_time_ms: number
  postgres_statement_timeout_ms: number
}

const DEFAULT_QUERY_CONSTRAINTS = {
  default_limit: 200,
  max_limit: 1000,
  app_timeout_seconds: 30,
  mysql_max_execution_time_ms: 25000,
  postgres_statement_timeout_ms: 25000,
} satisfies QueryConstraints
const EDITOR_LINE_HEIGHT = 18.2
const EDITOR_VERTICAL_PADDING = 8
const EDITOR_MIN_HEIGHT = EDITOR_VERTICAL_PADDING + EDITOR_BASE_VISIBLE_LINES * EDITOR_LINE_HEIGHT
const RESULT_PAGE_SIZE = 50
const METADATA_ERROR_MESSAGE = 'Metadata is temporarily unavailable. Please try again later.'
const DEFAULT_SENSITIVE_ACCESS_DURATION_MINUTES = 10
const MAX_SENSITIVE_ACCESS_DURATION_MINUTES = 3 * 24 * 60
const SENSITIVE_ACCESS_DURATION_PRESETS = [
  { label: '30m', minutes: 30 },
  { label: '2h', minutes: 120 },
  { label: '8h', minutes: 480 },
  { label: '1d', minutes: 1440 },
  { label: '3d', minutes: 4320 },
] as const
const SQL_EDITOR_PROFILE_ENABLED = import.meta.env.DEV
const REDIS_EDITOR_EXTENSIONS = [javascript()]
const SQL_EDITOR_BASIC_SETUP = {
  lineNumbers: true,
  foldGutter: false,
  highlightActiveLine: false,
}

function shouldAutofillTableQuery(sqlText: string) {
  const trimmed = sqlText.trim()
  return trimmed === '' || trimmed === DEFAULT_SQL
}

function buildTableSelectSQL(tableName: string) {
  return `SELECT * FROM ${tableName};`
}

type SQLFormatProfile = {
  id: number
  tabID: string
  startedAt: number
  sourceLength: number
  selectedLength: number
  targetLength: number
  nextLength: number
  formatterMs: number
  stateScheduleMs?: number
  layoutMeasureMs?: number
  rafMeasureMs?: number
  totalMs?: number
  expectedSQL: string
}

function logSQLFormatProfile(stage: string, profile: Record<string, unknown>) {
  if (!SQL_EDITOR_PROFILE_ENABLED) {
    return
  }
  console.info('[SQL Editor][Format Profile]', stage, profile)
}

function parsePixelValue(value: string): number {
  const parsed = Number.parseFloat(value)
  return Number.isFinite(parsed) ? parsed : 0
}

function buildQueryPayload(tab: EditorTab, sqlText: string, connection: DBConnection | null) {
  return {
    db_connection_id: tab.connectionId!,
    sql: sqlText,
    database: tab.database || undefined,
    schema: queryContextSchemaName(connection, tab.schema) || undefined,
    redis_db_index: connection?.db_type === 'redis' && tab.database ? Number(tab.database) : undefined,
  }
}

function queryContextSchemaName(connection: DBConnection | null, schema: string) {
  return connection?.db_type === 'postgres' ? schema.trim() : ''
}

function buildExplainSQL(sqlText: string) {
  const trimmed = sqlText.trim().replace(/;+\s*$/, '')
  if (!trimmed) {
    return ''
  }
  if (/^EXPLAIN\b/i.test(trimmed)) {
    return `${trimmed};`
  }
  if (trimmed.includes(';')) {
    throw new Error('Explain currently supports only a single SQL statement.')
  }
  return `EXPLAIN ${trimmed};`
}

function getSQLFormatterDialect(connection: DBConnection | null): SqlLanguage {
  if (connection?.db_type === 'postgres') {
    return 'postgresql'
  }
  if (connection?.db_type === 'mysql') {
    return 'mysql'
  }
  return 'sql'
}

function replaceSelectedSQL(sourceSQL: string, selectedSQL: string, replacement: string) {
  const trimmedSelected = selectedSQL.trim()
  if (!trimmedSelected) {
    return replacement
  }

  const index = sourceSQL.indexOf(selectedSQL)
  if (index >= 0) {
    return `${sourceSQL.slice(0, index)}${replacement}${sourceSQL.slice(index + selectedSQL.length)}`
  }

  const trimmedIndex = sourceSQL.indexOf(trimmedSelected)
  if (trimmedIndex >= 0) {
    return `${sourceSQL.slice(0, trimmedIndex)}${replacement}${sourceSQL.slice(trimmedIndex + trimmedSelected.length)}`
  }

  return replacement
}

function hasTabPatchChanges(tab: EditorTab, patch: Partial<EditorTab>) {
  const entries = Object.entries(patch) as Array<[keyof EditorTab, EditorTab[keyof EditorTab]]>
  return entries.some(([key, value]) => !Object.is(tab[key], value))
}

function measureEditorHeight(container: HTMLDivElement | null, sqlText: string): number {
  if (!container) {
    return EDITOR_MIN_HEIGHT
  }

  const content = container.querySelector('.cm-content')
  const line = container.querySelector('.cm-line')

  if (!(content instanceof HTMLElement)) {
    return EDITOR_MIN_HEIGHT
  }

  const contentStyles = window.getComputedStyle(content)
  const lineStyles = window.getComputedStyle(line instanceof HTMLElement ? line : content)
  const measuredLineHeight = parsePixelValue(lineStyles.lineHeight) || EDITOR_LINE_HEIGHT
  const verticalPadding = parsePixelValue(contentStyles.paddingTop) + parsePixelValue(contentStyles.paddingBottom)
  const minimumHeight = verticalPadding + measuredLineHeight * EDITOR_BASE_VISIBLE_LINES
  const contentLineCount = Math.max(1, sqlText.split('\n').length)
  const contentHeight = verticalPadding + measuredLineHeight * contentLineCount

  return Math.min(EDITOR_MAX_HEIGHT, Math.max(minimumHeight, contentHeight))
}

function formatMetadataError(error: unknown): string {
  if (error instanceof ApiError && error.status < 500) {
    return error.message
  }
  return METADATA_ERROR_MESSAGE
}

function isQueryAccessDeniedMessage(message: string) {
  return /query access/i.test(message)
}

function buildQueryAccessTicketURL(params: {
  connectionId: number
  database?: string
  tableName?: string
}) {
  const searchParams = new URLSearchParams({
    ticket_type: 'query_access',
    db_connection_id: String(params.connectionId),
  })
  if (params.database?.trim()) {
    searchParams.set('database_name', params.database.trim())
  }
  if (params.tableName?.trim()) {
    searchParams.set('table_name', params.tableName.trim())
  }
  return `/tickets/new?${searchParams.toString()}`
}

function formatResultMetaLine(params: {
  resultView: 'result' | 'vertical' | 'object-meta' | 'history' | 'saved'
  result: QueryResult | null
  selectedTable: MetadataItem | null
  detailHint: string
  historyCount: number
  savedCount: number
  currentPage: number
  totalPages: number
}): string {
  const { resultView, result, selectedTable, detailHint, historyCount, savedCount, currentPage, totalPages } = params

  if ((resultView === 'result' || resultView === 'vertical') && result) {
    const parts = [`${result.row_count} rows`, `${result.duration_ms} ms`]
    if (totalPages > 1) {
      parts.push(`Page ${currentPage} / ${totalPages}`)
    }
    return parts.join(' / ')
  }
  if (resultView === 'object-meta') {
    return selectedTable ? `${selectedTable.schema}.${selectedTable.name}` : detailHint || 'Select a table to inspect its structure'
  }
  if (resultView === 'history') {
    return `${historyCount} entries`
  }
  return `${savedCount} entries`
}

function validateSensitiveAccessDurationInput(rawValue: string) {
  const value = rawValue.trim()
  if (!value) {
    return { error: `Enter a duration between 1 and ${MAX_SENSITIVE_ACCESS_DURATION_MINUTES} minutes.` }
  }
  if (!/^\d+$/.test(value)) {
    return { error: 'Duration must be a whole number of minutes.' }
  }

  const minutes = Number(value)
  if (!Number.isInteger(minutes) || minutes < 1 || minutes > MAX_SENSITIVE_ACCESS_DURATION_MINUTES) {
    return { error: `Duration must be between 1 and ${MAX_SENSITIVE_ACCESS_DURATION_MINUTES} minutes.` }
  }

  return { minutes }
}

function formatSensitiveAccessDuration(minutes: number) {
  if (minutes < 60) {
    return `${minutes} minute${minutes === 1 ? '' : 's'}`
  }

  const days = Math.floor(minutes / 1440)
  const hours = Math.floor((minutes % 1440) / 60)
  const remainingMinutes = minutes % 60
  const parts: string[] = []

  if (days > 0) {
    parts.push(`${days} day${days === 1 ? '' : 's'}`)
  }
  if (hours > 0) {
    parts.push(`${hours} hour${hours === 1 ? '' : 's'}`)
  }
  if (remainingMinutes > 0) {
    parts.push(`${remainingMinutes} minute${remainingMinutes === 1 ? '' : 's'}`)
  }

  return parts.join(' ')
}

function formatSensitiveAccessExpiry(minutes: number) {
  const expiresAt = new Date(Date.now() + minutes * 60 * 1000)
  return formatDateTime(expiresAt.toISOString(), true)
}

function formatHistoryContext(entry: QueryHistoryEntry) {
  const parts = [entry.db_connection_name]
  if (entry.redis_db_index !== undefined && entry.redis_db_index !== null) {
    parts.push(`DB ${entry.redis_db_index}`)
  } else {
    if (entry.database_name?.trim()) {
      parts.push(entry.database_name.trim())
    }
    if (entry.schema_name?.trim()) {
      parts.push(entry.schema_name.trim())
    }
  }
  return parts.join(' / ')
}

function createTab(seed = 1): EditorTab {
  return {
    id: `tab-${Date.now()}-${seed}`,
    title: `Query ${seed}`,
    connectionId: null,
    database: '',
    schema: '',
    selectedTable: null,
    metadataError: '',
    explorerNodes: [],
    searchTreeNodes: [],
    explorerSearch: '',
    searchingAssets: false,
    searchIndexStatus: 'idle',
    searchIndexItems: [],
    searchIndexTruncated: false,
    searchIndexError: '',
    assetPickerOpen: false,
    assetPickerSearch: '',
    resultView: 'result',
    objectMetaTab: 'columns',
    columns: [],
    definition: null,
    columnsLoading: false,
    definitionLoading: false,
    columnFilterOpen: false,
    visibleColumnIndexes: null,
    selectedSQL: '',
    sensitiveAccessDuration: DEFAULT_SENSITIVE_ACCESS_DURATION_MINUTES,
    resultPage: 1,
    sql: DEFAULT_SQL,
    executedSQL: null,
    executedConnectionId: null,
    executedDatabase: '',
    executedSchema: '',
    result: null,
    error: '',
    lastRunAt: null,
  }
}

function sanitizeAssetTreeForWorkspaceMemory(nodes: AssetTreeNode[]): AssetTreeNode[] {
  return nodes.map((node) => ({
    ...node,
    loading: false,
    children: sanitizeAssetTreeForWorkspaceMemory(node.children),
  }))
}

function sanitizeTabForWorkspaceMemory(tab: EditorTab): EditorTab {
  return {
    ...tab,
    metadataError: '',
    explorerNodes: sanitizeAssetTreeForWorkspaceMemory(tab.explorerNodes),
    searchTreeNodes: sanitizeAssetTreeForWorkspaceMemory(tab.searchTreeNodes),
    searchingAssets: false,
    searchIndexStatus: tab.searchIndexStatus === 'ready' ? 'ready' : 'idle',
    searchIndexError: '',
    assetPickerOpen: false,
    resultView: tab.resultView === 'vertical' ? 'result' : tab.resultView,
    columns: [],
    definition: null,
    columnsLoading: false,
    definitionLoading: false,
    columnFilterOpen: false,
    visibleColumnIndexes: null,
    executedSQL: null,
    executedConnectionId: null,
    executedDatabase: '',
    executedSchema: '',
    result: null,
    error: '',
    lastRunAt: null,
  }
}

function workspaceDraftFromSnapshot(ownerKey: string): SQLEditorWorkspaceDraft | null {
  const snapshot = getSQLEditorWorkspaceSnapshot<EditorTab>(ownerKey)
  if (!snapshot || snapshot.tabs.length === 0) {
    return null
  }
  const tabs = snapshot.tabs.map(sanitizeTabForWorkspaceMemory)
  const activeTabId = tabs.some((tab) => tab.id === snapshot.activeTabId)
    ? snapshot.activeTabId
    : tabs[0].id
  return {
    tabs,
    activeTabId,
    editorHeights: { ...snapshot.editorHeights },
  }
}

function getNextTabSeed(tabs: EditorTab[]) {
  const usedSeeds = new Set<number>()
  tabs.forEach((tab) => {
    const match = /^Query (\d+)$/.exec(tab.title)
    if (match) {
      usedSeeds.add(Number(match[1]))
    }
  })

  for (let seed = 1; seed <= tabs.length + 1; seed += 1) {
    if (!usedSeeds.has(seed)) {
      return seed
    }
  }

  return tabs.length + 1
}

function createConnectionNode(connection: DBConnection, activeConnectionId: number | null): AssetTreeNode {
  return {
    id: `connection-${connection.id}`,
    kind: 'connection',
    connectionId: connection.id,
    label: connection.name,
    meta: connection.db_type.toUpperCase(),
    active: connection.id === activeConnectionId,
    selectable: true,
    expanded: connection.id === activeConnectionId,
    loaded: false,
    loading: false,
    children: [],
  }
}

function formatConnectionBadge(connection: DBConnection) {
  return connection.db_type.toUpperCase()
}

function formatConnectionGroupLabel(dbType: string) {
  switch (dbType) {
    case 'mysql':
      return 'MySQL'
    case 'postgres':
      return 'PgSQL'
    case 'redis':
      return 'Redis'
    default:
      return dbType.toUpperCase()
  }
}

function getConnectionGroupOrder(dbType: string) {
  switch (dbType) {
    case 'mysql':
      return 1
    case 'redis':
      return 2
    case 'postgres':
      return 3
    default:
      return 99
  }
}

function effectiveTimeoutSeconds(constraints: QueryConstraints, connection: DBConnection | null) {
  const appTimeoutSeconds = constraints.app_timeout_seconds
  if (connection?.db_type === 'mysql') {
    return Math.min(appTimeoutSeconds, Math.floor(constraints.mysql_max_execution_time_ms / 1000))
  }
  if (connection?.db_type === 'postgres') {
    return Math.min(appTimeoutSeconds, Math.floor(constraints.postgres_statement_timeout_ms / 1000))
  }
  return appTimeoutSeconds
}

function getCodeMirrorSQLDialect(connection: DBConnection | null) {
  if (connection?.db_type === 'mysql') {
    return MySQL
  }
  if (connection?.db_type === 'postgres') {
    return PostgreSQL
  }
  return StandardSQL
}

function collectLoadedTables(nodes: AssetTreeNode[]): MetadataItem[] {
  return nodes.flatMap((node) => [
    ...(node.kind === 'table' && node.item ? [node.item] : []),
    ...collectLoadedTables(node.children),
  ])
}

function collectCompletionTables(params: {
  explorerNodes: AssetTreeNode[]
  searchTreeNodes: AssetTreeNode[]
  selectedTable: MetadataItem | null
}): MetadataItem[] {
  const tablesByKey = new Map<string, MetadataItem>()
  for (const table of [...collectLoadedTables(params.explorerNodes), ...collectLoadedTables(params.searchTreeNodes)]) {
    const key = `${table.database ?? ''}\u0000${table.schema ?? ''}\u0000${table.name}`
    tablesByKey.set(key, table)
  }
  if (params.selectedTable) {
    const key = `${params.selectedTable.database ?? ''}\u0000${params.selectedTable.schema ?? ''}\u0000${params.selectedTable.name}`
    tablesByKey.set(key, params.selectedTable)
  }
  return [...tablesByKey.values()]
}

function columnCompletion(column: MetadataColumn): Completion {
  return {
    label: column.name,
    type: 'property',
    detail: column.column_type || column.data_type,
    info: column.comment || undefined,
  }
}

function columnCompletionSchema(columns: MetadataColumn[]): SQLNamespace {
  return columns.map(columnCompletion)
}

function tableCompletion(table: MetadataItem): Completion {
  return {
    label: table.name,
    type: 'type',
    detail: table.schema || table.database,
    info: table.comment || undefined,
  }
}

function tableNamespace(table: MetadataItem, selectedTable: MetadataItem | null, columns: MetadataColumn[]): SQLNamespace {
  if (selectedTable?.name === table.name && selectedTable.schema === table.schema) {
    return columnCompletionSchema(columns)
  }
  return []
}

function isSQLNamespaceRecord(namespace: SQLNamespace | undefined): namespace is Record<string, SQLNamespace> {
  return !!namespace && !Array.isArray(namespace) && !('self' in namespace)
}

function buildSQLCompletionSchema(params: {
  connection: DBConnection | null
  explorerNodes: AssetTreeNode[]
  searchTreeNodes: AssetTreeNode[]
  selectedTable: MetadataItem | null
  columns: MetadataColumn[]
}): SQLNamespace | undefined {
  const { connection, explorerNodes, searchTreeNodes, selectedTable, columns } = params
  if (!connection || connection.db_type === 'redis') {
    return undefined
  }

  const tables = collectCompletionTables({ explorerNodes, searchTreeNodes, selectedTable })

  if (tables.length === 0) {
    return undefined
  }

  if (connection.db_type === 'postgres') {
    const schemaNamespace: Record<string, SQLNamespace> = {}
    for (const table of tables) {
      const schemaName = table.schema || 'public'
      const currentSchema = schemaNamespace[schemaName]
      const children: Record<string, SQLNamespace> =
        isSQLNamespaceRecord(currentSchema)
          ? { ...currentSchema }
          : {}
      children[table.name] = tableNamespace(table, selectedTable, columns)
      schemaNamespace[schemaName] = children
    }
    return schemaNamespace
  }

  const schema: Record<string, SQLNamespace> = {}
  for (const table of tables) {
    schema[table.name] = tableNamespace(table, selectedTable, columns)
  }
  return schema
}

function isTableCompletionContext(sourceBeforeCursor: string) {
  const statementStart = Math.max(sourceBeforeCursor.lastIndexOf(';') + 1, sourceBeforeCursor.length - 240)
  const tail = sourceBeforeCursor.slice(statementStart).toLowerCase()
  const compactTail = tail.replace(/`[^`]*$|"[^"]*$|'[^']*$/g, '')

  return (
    /(?:^|[\s(,])(?:from|join|update|into|describe|desc)\s+[`"[\]\w$.-]*$/.test(compactTail) ||
    /(?:^|[\s(,])(?:alter|drop|truncate)\s+table\s+[`"[\]\w$.-]*$/.test(compactTail) ||
    /(?:^|[\s(,])from\s+.*,\s*[`"[\]\w$.-]*$/.test(compactTail)
  )
}

function buildFocusedSQLCompletionSource(params: {
  columns: MetadataColumn[]
  tables: MetadataItem[]
}): CompletionSource {
  const columnOptions = params.columns.map(columnCompletion)
  const tableOptions = params.tables.map(tableCompletion)

  return (context) => {
    const sourceBeforeCursor = context.state.sliceDoc(0, context.pos)
    const tableContext = isTableCompletionContext(sourceBeforeCursor)
    const token = context.matchBefore(tableContext ? /[`"[\]\w$.-]*/ : /[\w$]*/)
    if (!token || (token.from === token.to && !context.explicit)) {
      return null
    }

    return {
      from: token.from,
      options: tableContext ? tableOptions : columnOptions,
      validFor: tableContext ? /^[`"[\]\w$.-]*$/ : /^[\w$]*$/,
    }
  }
}

function matchesAssetKeyword(node: {
  label: string
  meta?: string
  database?: string
  schema?: string
}, keyword: string) {
  const loweredKeyword = keyword.trim().toLowerCase()
  if (!loweredKeyword) {
    return true
  }

  return (
    node.label.toLowerCase().includes(loweredKeyword) ||
    (node.meta ? node.meta.toLowerCase().includes(loweredKeyword) : false) ||
    (node.database ? node.database.toLowerCase().includes(loweredKeyword) : false) ||
    (node.schema ? node.schema.toLowerCase().includes(loweredKeyword) : false)
  )
}

function matchesMetadataItemKeyword(item: MetadataItem, keyword: string) {
  return matchesAssetKeyword({
    label: item.name,
    database: item.database,
    schema: item.schema,
  }, keyword)
}

function buildSearchTreeFromIndex(
  connection: DBConnection,
  items: MetadataItem[],
  keyword: string,
  selectedDatabase: string,
  selectedSchema: string,
  selectedTable: MetadataItem | null,
): AssetTreeNode[] {
  const rootNode = createConnectionNode(connection, connection.id)
  rootNode.expanded = true
  rootNode.loaded = true

  if (connection.db_type === 'redis') {
    rootNode.children = items
      .filter((item) => item.kind === 'redis_db')
      .map((item) => ({
        id: `redis-db-${connection.id}-${item.name}`,
        kind: 'redis_db' as const,
        connectionId: connection.id,
        label: `DB ${item.name}`,
        database: item.name,
        schema: item.name,
        active: false,
        selectable: true,
        expanded: false,
        loaded: true,
        loading: false,
        item,
        children: [],
      }))
      .filter((node) => matchesAssetKeyword(node, keyword))
    return syncAssetTreeActiveStates([rootNode], connection.id, selectedDatabase, selectedSchema, selectedTable)
  }

  const databaseNodes = new Map<string, AssetTreeNode>()
  const schemaNodes = new Map<string, AssetTreeNode>()
  const ensureDatabaseNode = (database: string, item?: MetadataItem) => {
    const key = database
    const existing = databaseNodes.get(key)
    if (existing) {
      return existing
    }
    const node: AssetTreeNode = {
      id: `database-${connection.id}-${database}`,
      kind: 'database',
      connectionId: connection.id,
      label: database,
      database,
      schema: item?.schema,
      active: false,
      selectable: true,
      expanded: true,
      loaded: true,
      loading: false,
      item,
      children: [],
    }
    databaseNodes.set(key, node)
    return node
  }
  const ensureSchemaNode = (database: string, schema: string, item?: MetadataItem) => {
    const key = `${database}\u0000${schema}`
    const existing = schemaNodes.get(key)
    if (existing) {
      return existing
    }
    const databaseNode = ensureDatabaseNode(database)
    const node: AssetTreeNode = {
      id: `schema-${connection.id}-${database}-${schema}`,
      kind: 'schema',
      connectionId: connection.id,
      label: schema,
      database,
      schema,
      active: false,
      selectable: true,
      expanded: true,
      loaded: true,
      loading: false,
      item,
      children: [],
    }
    schemaNodes.set(key, node)
    databaseNode.children.push(node)
    return node
  }

  for (const item of items) {
    const matched = matchesMetadataItemKeyword(item, keyword)
    if (item.kind === 'database') {
      if (matched) {
        ensureDatabaseNode(item.name, item)
      }
      continue
    }
    if (item.kind === 'schema') {
      const database = item.database || ''
      if (database && matched) {
        ensureSchemaNode(database, item.schema || item.name, item)
      }
      continue
    }
    if (item.kind !== 'table' || !matched) {
      continue
    }
    const database = item.database || item.schema || ''
    const schema = item.schema || database
    if (!database) {
      continue
    }
    const tableNode: AssetTreeNode = {
      id: `table-${connection.id}-${database}-${schema}-${item.name}`,
      kind: 'table',
      connectionId: connection.id,
      label: item.name,
      database,
      schema,
      active: false,
      selectable: true,
      expanded: false,
      loaded: true,
      loading: false,
      item,
      children: [],
    }
    if (connection.db_type === 'postgres') {
      ensureSchemaNode(database, schema).children.push(tableNode)
    } else {
      ensureDatabaseNode(database).children.push(tableNode)
    }
  }

  rootNode.children = [...databaseNodes.values()].filter((node) => {
    if (node.children.length > 0) {
      return true
    }
    return matchesAssetKeyword(node, keyword)
  })
  return syncAssetTreeActiveStates([rootNode], connection.id, selectedDatabase, selectedSchema, selectedTable)
}

async function buildConnectionRootNode(connection: DBConnection): Promise<AssetTreeNode> {
  const rootNode = createConnectionNode(connection, connection.id)
  rootNode.expanded = true
  rootNode.loaded = true

  if (connection.db_type === 'redis') {
    const response = await listMetadata(connection.id)
    rootNode.children = response.items.map((item) => ({
      id: `redis-db-${connection.id}-${item.name}`,
      kind: 'redis_db' as const,
      connectionId: connection.id,
      label: `DB ${item.name}`,
      database: item.name,
      schema: item.name,
      active: false,
      selectable: true,
      expanded: false,
      loaded: true,
      loading: false,
      item,
      children: [],
    }))
    return rootNode
  }

  const response = await listMetadata(connection.id)
  rootNode.children = response.items.map((item) => ({
    id: `database-${connection.id}-${item.name}`,
    kind: 'database' as const,
    connectionId: connection.id,
    label: item.name,
    database: item.name,
    schema: item.schema,
    active: false,
    selectable: true,
    expanded: false,
    loaded: false,
    loading: false,
    item,
    children: [],
  }))
  return rootNode
}

async function buildConnectionRootNodeForContext(connection: DBConnection, database: string, schema: string): Promise<AssetTreeNode> {
  const rootNode = await buildConnectionRootNode(connection)
  if (!database) {
    return rootNode
  }

  const databaseIndex = rootNode.children.findIndex((node) => node.kind === 'database' && node.label === database)
  if (databaseIndex === -1) {
    return rootNode
  }

  const databaseNode = rootNode.children[databaseIndex]
  databaseNode.expanded = true
  databaseNode.loading = true

  const databaseResponse = await listMetadata(connection.id, { database })
  if (connection.db_type !== 'postgres') {
    databaseNode.children = databaseResponse.items.map((item) => ({
      id: `table-${connection.id}-${database}-${item.schema}-${item.name}`,
      kind: 'table' as const,
      connectionId: connection.id,
      label: item.name,
      database,
      schema: item.schema,
      active: false,
      selectable: true,
      expanded: false,
      loaded: true,
      loading: false,
      item,
      children: [],
    }))
    databaseNode.loaded = true
    databaseNode.loading = false
    return rootNode
  }

  databaseNode.children = databaseResponse.items.map((item) => ({
    id: `schema-${connection.id}-${database}-${item.name}`,
    kind: 'schema' as const,
    connectionId: connection.id,
    label: item.name,
    database,
    schema: item.name,
    active: false,
    selectable: true,
    expanded: item.name === schema,
    loaded: false,
    loading: false,
    item,
    children: [],
  }))
  databaseNode.loaded = true
  databaseNode.loading = false

  if (!schema) {
    return rootNode
  }

  const schemaIndex = databaseNode.children.findIndex((node) => node.kind === 'schema' && node.label === schema)
  if (schemaIndex === -1) {
    return rootNode
  }

  const schemaNode = databaseNode.children[schemaIndex]
  schemaNode.expanded = true
  schemaNode.loading = true
  const tableResponse = await listMetadata(connection.id, { database, schema })
  schemaNode.children = tableResponse.items.map((item) => ({
    id: `table-${connection.id}-${database}-${item.schema}-${item.name}`,
    kind: 'table' as const,
    connectionId: connection.id,
    label: item.name,
    database,
    schema: item.schema,
    active: false,
    selectable: true,
    expanded: false,
    loaded: true,
    loading: false,
    item,
    children: [],
  }))
  schemaNode.loaded = true
  schemaNode.loading = false
  return rootNode
}

function syncAssetTreeActiveStates(
  nodes: AssetTreeNode[],
  activeConnectionId: number | null,
  selectedDatabase: string,
  selectedSchema: string,
  selectedTable: MetadataItem | null,
): AssetTreeNode[] {
  return nodes.map((node) => {
    const active =
      (node.kind === 'connection' && node.connectionId === activeConnectionId) ||
      (node.kind === 'database' && node.connectionId === activeConnectionId && !!selectedDatabase && node.label === selectedDatabase) ||
      (node.kind === 'schema' && node.connectionId === activeConnectionId && !!selectedSchema && node.label === selectedSchema) ||
      (node.kind === 'table' &&
        node.connectionId === activeConnectionId &&
        selectedTable?.name === node.label &&
        selectedTable?.schema === node.schema) ||
      (node.kind === 'redis_db' && node.connectionId === activeConnectionId && !!selectedDatabase && node.label === `DB ${selectedDatabase}`)

    return {
      ...node,
      active,
      expanded: node.kind === 'connection' && node.connectionId === activeConnectionId ? true : node.expanded,
      children: syncAssetTreeActiveStates(node.children, activeConnectionId, selectedDatabase, selectedSchema, selectedTable),
    }
  })
}

function updateAssetTreeNode(
  nodes: AssetTreeNode[],
  nodeID: string,
  updater: (node: AssetTreeNode) => AssetTreeNode,
): AssetTreeNode[] {
  return nodes.map((node) => {
    if (node.id === nodeID) {
      return updater(node)
    }
    if (node.children.length === 0) {
      return node
    }
    return { ...node, children: updateAssetTreeNode(node.children, nodeID, updater) }
  })
}

function filterAssetTree(nodes: AssetTreeNode[], searchTerm: string): AssetTreeNode[] {
  const keyword = searchTerm.trim().toLowerCase()
  if (!keyword) {
    return nodes
  }

  return nodes.flatMap((node) => {
    const selfMatched = matchesAssetKeyword(node, keyword)

    const filteredChildren = filterAssetTree(node.children, keyword)
    if (!selfMatched && filteredChildren.length === 0) {
      return []
    }

    return [
      {
        ...node,
        expanded: keyword ? true : node.expanded,
        children: filteredChildren,
      },
    ]
  })
}

function AssetTree({
  nodes,
  onSelect,
  onToggle,
}: {
  nodes: AssetTreeNode[]
  onSelect: (node: AssetTreeNode) => void
  onToggle: (node: AssetTreeNode) => void
}) {
  function iconForNode(node: AssetTreeNode) {
    if (node.kind === 'connection') {
      return <Workflow className="h-3.5 w-3.5" />
    }
    if (node.kind === 'schema') {
      return <Folder className="h-3.5 w-3.5" />
    }
    if (node.kind === 'table') {
      return <Table2 className="h-3.5 w-3.5" />
    }
    if (node.kind === 'redis_db') {
      return <Layers3 className="h-3.5 w-3.5" />
    }
    return <Database className="h-3.5 w-3.5" />
  }

  function renderNode(node: AssetTreeNode, depth = 0) {
    const hasChildren = node.children.length > 0
    const canExpand = node.kind !== 'table' && node.kind !== 'redis_db'
    const paddingLeft = 8 + depth * 14
    const handleNodeClick = () => {
      if (canExpand) {
        onSelect(node)
        onToggle(node)
        return
      }
      onSelect(node)
    }

    return (
      <div key={node.id}>
        <button
          type="button"
          onClick={handleNodeClick}
          className={`group flex min-w-max items-center rounded-md border border-transparent pr-2 text-[12px] leading-5 ${
            node.active ? 'bg-panel-soft text-ink' : 'text-muted hover:bg-panel-soft hover:text-ink'
          }`}
          style={{ paddingLeft }}
        >
          <span className="flex h-7 items-center gap-2 text-left">
            <span className="flex h-4 w-4 items-center justify-center text-faint group-hover:text-muted">{iconForNode(node)}</span>
            <span className="whitespace-nowrap font-medium">{node.label}</span>
            {node.loading ? <span className="whitespace-nowrap text-[10px] font-semibold text-faint">Loading…</span> : null}
          </span>
        </button>
        {node.expanded && hasChildren ? <div className="mt-0.5 space-y-0.5">{node.children.map((child) => renderNode(child, depth + 1))}</div> : null}
      </div>
    )
  }

  return <div translate="no" className="min-w-max space-y-0.5 pr-2">{nodes.map((node) => renderNode(node))}</div>
}

export function SQLEditorPage() {
  const editorContainerRef = useRef<HTMLDivElement | null>(null)
  const formatProfileRef = useRef<SQLFormatProfile | null>(null)
  const formatProfileIDRef = useRef(0)
  const navigate = useNavigate()
  const { user } = useAuth()
  const { pushToast } = useToast()
  const workspaceOwnerKey = user ? String(user.id) : ''
  const initialWorkspaceRef = useRef<SQLEditorWorkspaceDraft | null>(null)
  if (!initialWorkspaceRef.current) {
    initialWorkspaceRef.current = workspaceDraftFromSnapshot(workspaceOwnerKey) ?? {
      tabs: [createTab()],
      activeTabId: '',
      editorHeights: {},
    }
    if (!initialWorkspaceRef.current.activeTabId) {
      initialWorkspaceRef.current.activeTabId = initialWorkspaceRef.current.tabs[0].id
    }
  }
  const hasSensitiveOverride = Boolean(user?.permissions.includes('global.sensitive'))
  const canQuery = Boolean(user?.permissions.includes('sql_editor.query'))
  const canExport = Boolean(user?.permissions.includes('sql_editor.export'))
  const canApplySensitiveAccess = Boolean(user?.permissions.includes('sql_editor.sensitive_apply'))
  const canApplyTicket = Boolean(user?.permissions.includes('tickets.apply'))
  const accessibleConnectionIDs = user?.dbConnectionIds ?? []
  const [connections, setConnections] = useState<DBConnection[]>([])
  const [connectionsLoading, setConnectionsLoading] = useState(true)
  const [connectionsError, setConnectionsError] = useState('')
  const [tabs, setTabs] = useState<EditorTab[]>(() => initialWorkspaceRef.current!.tabs)
  const [activeTabId, setActiveTabId] = useState<string>(() => initialWorkspaceRef.current!.activeTabId)
  const [history, setHistory] = useState<QueryHistoryEntry[]>([])
  const [savedQueries, setSavedQueries] = useState<SavedQuery[]>([])
  const [queryConstraints, setQueryConstraints] = useState(DEFAULT_QUERY_CONSTRAINTS)
  const [runningTabIDs, setRunningTabIDs] = useState<string[]>([])
  const [exportingTabIDs, setExportingTabIDs] = useState<string[]>([])
  const [sensitiveAccessTabIDs, setSensitiveAccessTabIDs] = useState<string[]>([])
  const [savedQueryToDelete, setSavedQueryToDelete] = useState<SavedQuery | null>(null)
  const [saveQueryDialog, setSaveQueryDialog] = useState<SaveQueryDialogState | null>(null)
  const [requestConfirmState, setRequestConfirmState] = useState<QueryRequestConfirmState | null>(null)
  const [exportReason, setExportReason] = useState('')
  const [sensitiveAccessDurationDialog, setSensitiveAccessDurationDialog] = useState<SensitiveAccessDurationDialogState | null>(null)
  const [editorHeights, setEditorHeights] = useState<Record<string, string>>(() => initialWorkspaceRef.current!.editorHeights)
  const [queryAccessAttentionKeys, setQueryAccessAttentionKeys] = useState<Record<string, number>>({})
  const workspaceOwnerKeyRef = useRef(workspaceOwnerKey)

  useEffect(() => {
    if (!workspaceOwnerKey) {
      return
    }
    if (workspaceOwnerKeyRef.current !== workspaceOwnerKey) {
      workspaceOwnerKeyRef.current = workspaceOwnerKey
      const nextDraft = workspaceDraftFromSnapshot(workspaceOwnerKey) ?? {
        tabs: [createTab()],
        activeTabId: '',
        editorHeights: {},
      }
      const nextActiveTabId = nextDraft.activeTabId || nextDraft.tabs[0].id
      setTabs(nextDraft.tabs)
      setActiveTabId(nextActiveTabId)
      setEditorHeights(nextDraft.editorHeights)
      setRunningTabIDs([])
      setExportingTabIDs([])
      setSensitiveAccessTabIDs([])
      setSavedQueryToDelete(null)
      setSaveQueryDialog(null)
      setRequestConfirmState(null)
      setExportReason('')
      setSensitiveAccessDurationDialog(null)
      setQueryAccessAttentionKeys({})
      return
    }
    saveSQLEditorWorkspaceSnapshot({
      ownerKey: workspaceOwnerKey,
      tabs: tabs.map(sanitizeTabForWorkspaceMemory),
      activeTabId,
      editorHeights,
    })
  }, [activeTabId, editorHeights, tabs, workspaceOwnerKey])

  useEffect(() => {
    let active = true

    async function loadConnections() {
      if (!canQuery) {
        setConnections([])
        setConnectionsLoading(false)
        setConnectionsError('')
        return
      }
      setConnectionsLoading(true)
      setConnectionsError('')
      try {
        const response = await listQueryConnections()
        if (!active) {
          return
        }

        setConnections(response.connections)
      } catch (error) {
        if (active) {
          setConnectionsError(error instanceof ApiError ? error.message : 'Failed to load database connections.')
        }
      } finally {
        if (active) {
          setConnectionsLoading(false)
        }
      }
    }

    void loadConnections()

    return () => {
      active = false
    }
  }, [canQuery])

  useEffect(() => {
    let active = true
    if (!canQuery) {
      setHistory([])
      setSavedQueries([])
      return () => {
        active = false
      }
    }

    async function loadQueryConstraints() {
      try {
        const response = await getQueryConstraints()
        if (active) {
          setQueryConstraints(response)
        }
      } catch {
        if (active) {
          setQueryConstraints(DEFAULT_QUERY_CONSTRAINTS)
        }
      }
    }

    void loadQueryConstraints()

    return () => {
      active = false
    }
  }, [canQuery, pushToast])

  useEffect(() => {
    let active = true
    if (!canQuery) {
      setHistory([])
      setSavedQueries([])
      return () => {
        active = false
      }
    }

    async function loadHistory() {
      try {
        const response = await listQueryHistory(HISTORY_LIMIT)
        if (active) {
          setHistory(response.history)
        }
      } catch (error) {
        if (active) {
          pushToast(error instanceof ApiError ? error.message : 'Failed to load query history.', 'error')
        }
      }
    }

    async function loadSavedQueries() {
      try {
        const response = await listSavedQueries()
        if (active) {
          setSavedQueries(response.saved_queries)
        }
      } catch (error) {
        if (active) {
          pushToast(error instanceof ApiError ? error.message : 'Failed to load saved queries.', 'error')
        }
      }
    }

    void loadHistory()
    void loadSavedQueries()

    return () => {
      active = false
    }
  }, [canQuery, pushToast])

  const activeTab = useMemo(
    () => tabs.find((tab) => tab.id === activeTabId) ?? tabs[0] ?? null,
    [activeTabId, tabs],
  )
  const accessibleConnections = useMemo(() => {
    if (!accessibleConnectionIDs.length) {
      return [] as DBConnection[]
    }

    const accessibleIDSet = new Set(accessibleConnectionIDs)
    return connections.filter((connection) => accessibleIDSet.has(connection.id))
  }, [accessibleConnectionIDs, connections])
  const activeConnection = useMemo(
    () => accessibleConnections.find((connection) => connection.id === activeTab?.connectionId) ?? null,
    [activeTab?.connectionId, accessibleConnections],
  )
  const activeDatabase = activeTab?.database ?? ''
  const activeSchema = activeTab?.schema ?? ''
  const activeSelectedTable = activeTab?.selectedTable ?? null
  const activeExplorerNodes = activeTab?.explorerNodes ?? []
  const activeSearchTreeNodes = activeTab?.searchTreeNodes ?? []
  const activeExplorerSearch = activeTab?.explorerSearch ?? ''
  const activeSearchingAssets = activeTab?.searchingAssets ?? false
  const activeSearchIndexStatus = activeTab?.searchIndexStatus ?? 'idle'
  const activeSearchIndexError = activeTab?.searchIndexError ?? ''
  const activeSearchIndexTruncated = activeTab?.searchIndexTruncated ?? false
  const debouncedExplorerSearch = useDebouncedValue(activeExplorerSearch, 350)
  const activeAssetPickerOpen = activeTab?.assetPickerOpen ?? false
  const activeAssetPickerSearch = activeTab?.assetPickerSearch ?? ''
  const activeResultView = activeTab?.resultView ?? 'result'
  const activeObjectMetaTab = activeTab?.objectMetaTab ?? 'columns'
  const activeColumns = activeTab?.columns ?? []
  const activeDefinition = activeTab?.definition ?? null
  const activeColumnsLoading = activeTab?.columnsLoading ?? false
  const activeDefinitionLoading = activeTab?.definitionLoading ?? false
  const activeColumnFilterOpen = activeTab?.columnFilterOpen ?? false
  const activeVisibleColumnIndexes = activeTab?.visibleColumnIndexes ?? null
  const activeSelectedSQL = activeTab?.selectedSQL ?? ''
  const activeExecutionSQL = activeSelectedSQL.trim() || activeTab?.sql.trim() || ''
  const activeContextSchema = queryContextSchemaName(activeConnection, activeSchema)
  const activeResultMatchesSQL = Boolean(
    activeTab?.result?.query_context_token &&
    activeTab.executedSQL &&
    activeTab.executedSQL === activeExecutionSQL &&
    activeTab.executedConnectionId === activeTab.connectionId &&
    activeTab.executedDatabase === activeDatabase &&
    activeTab.executedSchema === activeContextSchema,
  )
  const activeResultHasSensitiveColumns = (activeTab?.result?.sensitive_column_indexes?.length ?? 0) > 0
  const activeSensitiveAccessDuration = activeTab?.sensitiveAccessDuration ?? DEFAULT_SENSITIVE_ACCESS_DURATION_MINUTES
  const activeResultPage = activeTab?.resultPage ?? 1
  const filteredExplorerNodes = useMemo(
    () => (activeExplorerSearch.trim() ? activeSearchTreeNodes : filterAssetTree(activeExplorerNodes, activeExplorerSearch)),
    [activeExplorerNodes, activeExplorerSearch, activeSearchTreeNodes],
  )
  const renderedExplorerNodes = useMemo(() => {
    if (filteredExplorerNodes.length === 1 && filteredExplorerNodes[0].kind === 'connection') {
      return filteredExplorerNodes[0].children
    }
    return filteredExplorerNodes
  }, [filteredExplorerNodes])
  const activeExplorerRootLoading = activeExplorerNodes.length === 1 && activeExplorerNodes[0].kind === 'connection' && activeExplorerNodes[0].loading
  const filteredConnections = useMemo(() => {
    const keyword = activeAssetPickerSearch.trim().toLowerCase()
    if (!keyword) {
      return accessibleConnections
    }
    return accessibleConnections.filter((connection) =>
      connection.name.toLowerCase().includes(keyword) ||
      connection.db_type.toLowerCase().includes(keyword) ||
      (connection.database_name ?? '').toLowerCase().includes(keyword),
    )
  }, [accessibleConnections, activeAssetPickerSearch])
  const groupedAssetPickerConnections = useMemo(() => {
    const groups = new Map<string, DBConnection[]>()
    filteredConnections.forEach((connection) => {
      groups.set(connection.db_type, [...(groups.get(connection.db_type) ?? []), connection])
    })

    return Array.from(groups.entries())
      .sort(([leftType], [rightType]) => {
        const orderDiff = getConnectionGroupOrder(leftType) - getConnectionGroupOrder(rightType)
        return orderDiff || formatConnectionGroupLabel(leftType).localeCompare(formatConnectionGroupLabel(rightType))
      })
      .map(([dbType, groupConnections]) => ({
        dbType,
        label: formatConnectionGroupLabel(dbType),
        connections: [...groupConnections].sort((left, right) => left.name.localeCompare(right.name)),
      }))
  }, [filteredConnections])
  const activeTabRunning = activeTab ? runningTabIDs.includes(activeTab.id) : false
  const activeTabExporting = activeTab ? exportingTabIDs.includes(activeTab.id) : false
  const activeTabCreatingSensitiveAccess = activeTab ? sensitiveAccessTabIDs.includes(activeTab.id) : false
  const activeQueryAccessAttentionKey = activeTab ? queryAccessAttentionKeys[activeTab.id] : undefined
  const activeEditorHeight = activeTab ? (editorHeights[activeTab.id] ?? `${EDITOR_MIN_HEIGHT}px`) : `${EDITOR_MIN_HEIGHT}px`
  const queryConstraintBadges = useMemo(() => {
    return {
      limit: queryConstraints.default_limit,
      timeoutSeconds: effectiveTimeoutSeconds(queryConstraints, activeConnection),
    }
  }, [activeConnection, queryConstraints])
  const requestConfirmLoading = requestConfirmState
    ? requestConfirmState.kind === 'export'
      ? exportingTabIDs.includes(requestConfirmState.tabID)
      : sensitiveAccessTabIDs.includes(requestConfirmState.tabID)
    : false

  useEffect(() => {
    if (connectionsLoading) {
      return
    }
    if (accessibleConnectionIDs.length > 0 && connections.length === 0) {
      return
    }

    if (accessibleConnections.length === 0) {
      setTabs((currentTabs) => {
        let changed = false
        const nextTabs = currentTabs.map((tab) => {
          if (tab.connectionId === null) {
            return tab
          }
          changed = true
          return { ...tab, connectionId: null }
        })
        return changed ? nextTabs : currentTabs
      })
      return
    }

    const accessibleIDSet = new Set(accessibleConnections.map((connection) => connection.id))
    setTabs((currentTabs) => {
      let changed = false
      const nextTabs = currentTabs.map((tab) => {
        if (tab.connectionId !== null && !accessibleIDSet.has(tab.connectionId)) {
          changed = true
          return { ...tab, connectionId: null }
        }
        return tab
      })
      return changed ? nextTabs : currentTabs
    })
  }, [accessibleConnectionIDs.length, accessibleConnections, connections.length, connectionsLoading])

  useEffect(() => {
    if (!activeConnection) {
      return
    }

    const nextExplorerNodes = updateAssetTreeNode(
      [createConnectionNode(activeConnection, activeConnection.id)],
      `connection-${activeConnection.id}`,
      (node) => node,
    )

    updateActiveTabExplorerNodes((current) => {
      const existing = current[0]
      if (existing && existing.connectionId === activeConnection.id) {
        return current
      }
      return nextExplorerNodes
    })
    if (activeTab?.searchTreeNodes.length) {
      updateActiveTab({ searchTreeNodes: [] })
    }
  }, [activeConnection, activeTab?.id])

  useEffect(() => {
    const keyword = debouncedExplorerSearch.trim()
    if (!keyword || !activeConnection) {
      if (activeTab?.searchTreeNodes.length || activeTab?.searchingAssets) {
        updateActiveTab({ searchTreeNodes: [], searchingAssets: false })
      }
      return
    }
    if (keyword.length < SEARCH_INDEX_MIN_KEYWORD_LENGTH) {
      if (activeTab?.searchTreeNodes.length || activeTab?.searchingAssets) {
        updateActiveTab({ searchTreeNodes: [], searchingAssets: false })
      }
      return
    }
    if (activeTab?.searchIndexStatus !== 'ready') {
      if (activeTab?.searchTreeNodes.length || !activeTab?.searchingAssets) {
        updateActiveTab({ searchTreeNodes: [], searchingAssets: activeTab?.searchIndexStatus === 'loading' })
      }
      return
    }

    const nodes = buildSearchTreeFromIndex(
      activeConnection,
      activeTab.searchIndexItems,
      keyword,
      activeDatabase,
      activeSchema,
      activeSelectedTable,
    )
    updateActiveTab({ searchTreeNodes: nodes, searchingAssets: false })
  }, [activeConnection, activeDatabase, activeSchema, activeSelectedTable, activeTab?.id, activeTab?.searchIndexItems, activeTab?.searchIndexStatus, activeTab?.searchTreeNodes.length, activeTab?.searchingAssets, debouncedExplorerSearch])

  useEffect(() => {
    if (!activeExplorerSearch.trim()) {
      return
    }

    updateActiveTabSearchTreeNodes((current) =>
      syncAssetTreeActiveStates(current, activeTab?.connectionId ?? null, activeDatabase, activeSchema, activeSelectedTable),
    )
  }, [activeDatabase, activeExplorerSearch, activeSchema, activeSelectedTable, activeTab?.connectionId])

  useEffect(() => {
    if (!activeTab || !activeConnection || activeTab.searchIndexStatus !== 'idle') {
      return
    }
    loadSearchIndexForTab(activeTab.id, activeConnection)
  }, [activeConnection, activeTab?.id, activeTab?.searchIndexStatus])

  useEffect(() => {
    updateActiveTabExplorerNodes((current) =>
      syncAssetTreeActiveStates(current, activeTab?.connectionId ?? null, activeDatabase, activeSchema, activeSelectedTable),
    )
  }, [activeDatabase, activeSchema, activeSelectedTable, activeTab?.connectionId, activeTab?.id])

  useEffect(() => {
    if (!activeTab?.connectionId || !activeSelectedTable || activeConnection?.db_type === 'redis') {
      return
    }

    let active = true
    const connectionId = activeTab.connectionId
    const schema = activeSelectedTable.schema
    const table = activeSelectedTable.name

    if (!schema) {
      updateActiveTab({ columns: [], definition: null })
      return
    }
    const schemaName = schema

    async function loadObjectMeta() {
      updateActiveTab({
        columnsLoading: true,
        definitionLoading: true,
        metadataError: '',
      })
      try {
        const [columnsResponse, definitionResponse] = await Promise.all([
          listMetadataColumns(connectionId, schemaName, table, activeDatabase || undefined),
          listMetadataDefinition(connectionId, schemaName, table, activeDatabase || undefined),
        ])
        if (active) {
          updateTabByID(activeTab.id, {
            columns: columnsResponse.columns,
            definition: definitionResponse,
          })
        }
      } catch (error) {
        if (active) {
          updateTabByID(activeTab.id, {
            metadataError: formatMetadataError(error),
            columns: [],
            definition: null,
          })
        }
      } finally {
        if (active) {
          updateTabByID(activeTab.id, {
            columnsLoading: false,
            definitionLoading: false,
          })
        }
      }
    }

    void loadObjectMeta()

    return () => {
      active = false
    }
  }, [activeConnection?.db_type, activeDatabase, activeSelectedTable, activeTab?.connectionId, activeTab?.id])

  const updateTabByID = useCallback((tabID: string, patch: Partial<EditorTab>) => {
    setTabs((currentTabs) => currentTabs.map((tab) => {
      if (tab.id !== tabID || !hasTabPatchChanges(tab, patch)) {
        return tab
      }
      return { ...tab, ...patch }
    }))
  }, [])

  const updateActiveTab = useCallback((patch: Partial<EditorTab>) => {
    if (!activeTab) {
      return
    }

    updateTabByID(activeTab.id, patch)
  }, [activeTab, updateTabByID])

  const updateActiveTabExplorerNodes = useCallback((updater: (nodes: AssetTreeNode[]) => AssetTreeNode[]) => {
    if (!activeTab) {
      return
    }
    setTabs((currentTabs) => currentTabs.map((tab) => (
      tab.id === activeTab.id
        ? (() => {
            const nextNodes = updater(tab.explorerNodes)
            return nextNodes === tab.explorerNodes ? tab : { ...tab, explorerNodes: nextNodes }
          })()
        : tab
    )))
  }, [activeTab])

  const updateActiveTabSearchTreeNodes = useCallback((updater: (nodes: AssetTreeNode[]) => AssetTreeNode[]) => {
    if (!activeTab) {
      return
    }
    setTabs((currentTabs) => currentTabs.map((tab) => (
      tab.id === activeTab.id
        ? (() => {
            const nextNodes = updater(tab.searchTreeNodes)
            return nextNodes === tab.searchTreeNodes ? tab : { ...tab, searchTreeNodes: nextNodes }
          })()
        : tab
    )))
  }, [activeTab])

  function loadSearchIndexForTab(tabID: string, connection: DBConnection) {
    updateTabByID(tabID, {
      searchIndexStatus: 'loading',
      searchIndexError: '',
      searchIndexTruncated: false,
    })

    void listMetadataSearchIndex(connection.id)
      .then((response) => {
        updateTabByID(tabID, {
          searchIndexStatus: 'ready',
          searchIndexItems: response.items,
          searchIndexTruncated: response.truncated,
          searchIndexError: '',
        })
      })
      .catch((error) => {
        updateTabByID(tabID, {
          searchIndexStatus: 'error',
          searchIndexItems: [],
          searchIndexTruncated: false,
          searchIndexError: error instanceof ApiError ? error.message : 'Object search index is temporarily unavailable.',
        })
      })
  }

  async function loadNodeChildren(node: AssetTreeNode) {
    const connection = connections.find((item) => item.id === node.connectionId)
    if (!connection) {
      return
    }

    updateActiveTabExplorerNodes((current) =>
      updateAssetTreeNode(current, node.id, (target) => ({ ...target, loading: true, expanded: true })),
    )
    updateActiveTab({ metadataError: '' })

    try {
      let children: AssetTreeNode[] = []

      if (node.kind === 'connection') {
        if (connection.db_type === 'redis') {
          const response = await listMetadata(connection.id)
          children = response.items.map((item) => ({
            id: `redis-db-${connection.id}-${item.name}`,
            kind: 'redis_db',
            connectionId: connection.id,
            label: `DB ${item.name}`,
            database: item.name,
            schema: item.name,
            active: false,
            selectable: true,
            expanded: false,
            loaded: true,
            loading: false,
            item,
            children: [],
          }))
        } else {
          const response = await listMetadata(connection.id)
          children = response.items.map((item) => ({
            id: `database-${connection.id}-${item.name}`,
            kind: 'database',
            connectionId: connection.id,
            label: item.name,
            database: item.name,
            schema: item.schema,
            active: false,
            selectable: true,
            expanded: false,
            loaded: false,
            loading: false,
            item,
            children: [],
          }))
        }
      } else if (node.kind === 'database') {
        const response = await listMetadata(connection.id, { database: node.database || node.label })
        if (connection.db_type === 'postgres') {
          children = response.items.map((item) => ({
            id: `schema-${connection.id}-${node.label}-${item.name}`,
            kind: 'schema',
            connectionId: connection.id,
            label: item.name,
            database: node.database || node.label,
            schema: item.name,
            active: false,
            selectable: true,
            expanded: false,
            loaded: false,
            loading: false,
            item,
            children: [],
          }))
        } else {
          children = response.items.map((item) => ({
            id: `table-${connection.id}-${node.label}-${item.schema}-${item.name}`,
            kind: 'table',
            connectionId: connection.id,
            label: item.name,
            database: node.database || node.label,
            schema: item.schema,
            active: false,
            selectable: true,
            expanded: false,
            loaded: true,
            loading: false,
            item,
            children: [],
          }))
        }
      } else if (node.kind === 'schema') {
        const response = await listMetadata(connection.id, {
          database: node.database,
          schema: node.schema || node.label,
        })
        children = response.items.map((item) => ({
          id: `table-${connection.id}-${node.database}-${item.schema}-${item.name}`,
          kind: 'table',
          connectionId: connection.id,
          label: item.name,
          database: node.database,
          schema: item.schema,
          active: false,
          selectable: true,
          expanded: false,
          loaded: true,
          loading: false,
          item,
          children: [],
        }))
      }

      updateActiveTabExplorerNodes((current) =>
        syncAssetTreeActiveStates(
          updateAssetTreeNode(current, node.id, (target) => ({
            ...target,
            children,
            expanded: true,
            loaded: true,
            loading: false,
          })),
          activeTab?.connectionId ?? null,
          activeDatabase,
          activeSchema,
          activeSelectedTable,
        ),
      )
    } catch (error) {
      updateActiveTab({ metadataError: formatMetadataError(error) })
      updateActiveTabExplorerNodes((current) =>
        updateAssetTreeNode(current, node.id, (target) => ({ ...target, loading: false, expanded: true })),
      )
    }
  }

  function handleAddTab() {
    if (tabs.length >= MAX_EDITOR_TABS) {
      pushToast(`You can open up to ${MAX_EDITOR_TABS} query workspaces.`, 'info')
      return
    }
    const nextTab = createTab(getNextTabSeed(tabs))
    setTabs((current) => [...current, nextTab])
    setEditorHeights((current) => ({
      ...current,
      [nextTab.id]: `${measureEditorHeight(editorContainerRef.current, nextTab.sql)}px`,
    }))
    setActiveTabId(nextTab.id)
  }

  function handleSelectTab(tab: EditorTab) {
    if (tab.id === activeTabId) {
      return
    }
    setEditorHeights((current) => {
      const nextValue = `${measureEditorHeight(editorContainerRef.current, tab.sql)}px`
      if (current[tab.id] === nextValue) {
        return current
      }
      return {
        ...current,
        [tab.id]: nextValue,
      }
    })
    setActiveTabId(tab.id)
  }

  function handleCloseTab(id: string) {
    setTabs((currentTabs) => {
      if (currentTabs.length === 1) {
        const replacement = createTab()
        setActiveTabId(replacement.id)
        return [replacement]
      }

      const nextTabs = currentTabs.filter((tab) => tab.id !== id)
      if (activeTabId === id) {
        setActiveTabId(nextTabs[Math.max(0, currentTabs.findIndex((tab) => tab.id === id) - 1)].id)
      }
      return nextTabs
    })
  }

  async function executeEditorSQL(mode: 'run' | 'explain') {
    if (!canQuery) {
      updateActiveTab({ error: 'SQL query permission is required to run queries.' })
      return
    }
    const sqlToExecute = activeExecutionSQL
    if (!activeTab?.connectionId || !sqlToExecute) {
      updateActiveTab({ error: 'Select a database connection and enter a query first.' })
      return
    }

    const tabID = activeTab.id
    const tabSnapshot = activeTab
    const connectionSnapshot = activeConnection

    setRunningTabIDs((current) => (current.includes(tabID) ? current : [...current, tabID]))
    updateTabByID(tabID, { error: '' })

    try {
      const finalSQL = mode === 'explain' ? buildExplainSQL(sqlToExecute) : sqlToExecute
      const result = await executeQuery(buildQueryPayload(tabSnapshot, finalSQL, connectionSnapshot ?? null))

      updateTabByID(tabID, {
        result,
        executedSQL: finalSQL,
        executedConnectionId: tabSnapshot.connectionId,
        executedDatabase: tabSnapshot.database,
        executedSchema: queryContextSchemaName(connectionSnapshot ?? null, tabSnapshot.schema),
        error: '',
        lastRunAt: new Date().toISOString(),
        resultView: 'result',
      })
      void listQueryHistory(HISTORY_LIMIT).then((response) => setHistory(response.history)).catch(() => undefined)
      pushToast(mode === 'explain' ? 'Explain completed.' : 'Query completed.', 'success')
    } catch (error) {
      const message = error instanceof ApiError || error instanceof Error ? error.message : 'Query execution failed.'
      updateTabByID(tabID, {
        error: message,
        result: null,
        executedSQL: null,
        executedConnectionId: null,
        executedDatabase: '',
        executedSchema: '',
      })
      if (tabSnapshot.connectionId && isQueryAccessDeniedMessage(message)) {
        setQueryAccessAttentionKeys((current) => ({
          ...current,
          [tabID]: (current[tabID] ?? 0) + 1,
        }))
      }
    } finally {
      setRunningTabIDs((current) => current.filter((id) => id !== tabID))
    }
  }

  async function handleRunQuery() {
    await executeEditorSQL('run')
  }

  async function handleExplainQuery() {
    await executeEditorSQL('explain')
  }

  function openQueryAccessTicket() {
    if (!canApplyTicket) {
      return
    }
    if (!activeTab?.connectionId) {
      pushToast('Select a database connection first.', 'info')
      return
    }

    navigate(buildQueryAccessTicketURL({
      connectionId: activeTab.connectionId,
      database: activeTab.database || undefined,
      tableName: activeTab.selectedTable?.name || undefined,
    }))
  }

  function handleFormatSQL() {
    if (!activeTab) {
      return
    }

    const tabID = activeTab.id
    const sourceSQL = activeTab.sql
    const selectedSQL = activeSelectedSQL.trim()
    const targetSQL = selectedSQL || sourceSQL.trim()
    if (!targetSQL) {
      return
    }

    try {
      const startedAt = performance.now()
      const formatterStartedAt = performance.now()
      const formatted = formatSQL(targetSQL, {
        language: getSQLFormatterDialect(activeConnection ?? null),
        keywordCase: 'upper',
      }).trimEnd()
      const formatterMs = performance.now() - formatterStartedAt
      const nextSQL = selectedSQL
        ? replaceSelectedSQL(sourceSQL, activeSelectedSQL, formatted)
        : formatted

      if (nextSQL === sourceSQL) {
        logSQLFormatProfile('noop', {
          tabID,
          sourceLength: sourceSQL.length,
          selectedLength: activeSelectedSQL.length,
          targetLength: targetSQL.length,
          formatterMs,
        })
        return
      }

      const profile: SQLFormatProfile = {
        id: ++formatProfileIDRef.current,
        tabID,
        startedAt,
        sourceLength: sourceSQL.length,
        selectedLength: activeSelectedSQL.length,
        targetLength: targetSQL.length,
        nextLength: nextSQL.length,
        formatterMs,
        stateScheduleMs: performance.now() - startedAt,
        expectedSQL: nextSQL,
      }
      formatProfileRef.current = profile
      logSQLFormatProfile('scheduled', {
        id: profile.id,
        tabID,
        sourceLength: profile.sourceLength,
        selectedLength: profile.selectedLength,
        targetLength: profile.targetLength,
        nextLength: profile.nextLength,
        formatterMs: Number(profile.formatterMs.toFixed(2)),
        stateScheduleMs: Number((profile.stateScheduleMs ?? 0).toFixed(2)),
      })

      updateTabByID(tabID, { sql: nextSQL, error: '', selectedSQL: '' })
    } catch (error) {
      const message = error instanceof Error ? error.message : 'SQL formatting failed.'
      pushToast(message, 'error')
    }
  }

  function buildRequestConfirmState(
    kind: QueryRequestConfirmState['kind'],
    options?: {
      tabID?: string
      sensitiveAccessDuration?: number
    },
  ): QueryRequestConfirmState | null {
    const sourceTab = tabs.find((tab) => tab.id === (options?.tabID ?? activeTab?.id)) ?? null
    const sourceConnection = sourceTab
      ? accessibleConnections.find((connection) => connection.id === sourceTab.connectionId) ?? null
      : null
    const sourceSQL = sourceTab?.selectedSQL.trim() || sourceTab?.sql.trim() || ''

    if (!sourceTab?.connectionId || !sourceConnection || !sourceSQL) {
      return null
    }
    const queryContextToken = sourceTab.result?.query_context_token ?? ''
    const contextSchema = queryContextSchemaName(sourceConnection, sourceTab.schema)
    if (
      !queryContextToken ||
      sourceTab.executedSQL !== sourceSQL ||
      sourceTab.executedConnectionId !== sourceTab.connectionId ||
      sourceTab.executedDatabase !== sourceTab.database ||
      sourceTab.executedSchema !== contextSchema
    ) {
      return null
    }

    return {
      kind,
      tabID: sourceTab.id,
      connectionId: sourceTab.connectionId,
      connectionName: sourceConnection.name,
      connectionType: sourceConnection.db_type,
      database: sourceTab.database,
      schema: sourceTab.schema,
      contextSchema,
      tableName: sourceTab.selectedTable?.name ?? '',
      sql: sourceSQL,
      queryContextToken,
      sensitiveAccessDuration: options?.sensitiveAccessDuration ?? sourceTab.sensitiveAccessDuration,
    }
  }

  function openExportConfirm() {
    if (!canExport) {
      return
    }
    const state = buildRequestConfirmState('export')
    if (!state) {
      pushToast('Run this SQL successfully before exporting.', 'info', { placement: 'center' })
      return
    }
    setRequestConfirmState(state)
  }

  function openSensitiveAccessConfirm() {
    if (!canApplySensitiveAccess) {
      return
    }
    if (activeConnection?.db_type !== 'mysql') {
      pushToast('Sensitive Access currently supports MySQL only.', 'info', { placement: 'center' })
      return
    }

    if (!activeTab) {
      return
    }

    setSensitiveAccessDurationDialog({
      tabID: activeTab.id,
      value: String(activeSensitiveAccessDuration),
      error: '',
    })
  }

  function handleSensitiveAccessDurationContinue() {
    if (!sensitiveAccessDurationDialog) {
      return
    }

    const validation = validateSensitiveAccessDurationInput(sensitiveAccessDurationDialog.value)
    if ('error' in validation) {
      const errorMessage = validation.error ?? 'Invalid duration.'
      setSensitiveAccessDurationDialog((current) => (current ? { ...current, error: errorMessage } : current))
      return
    }

    updateTabByID(sensitiveAccessDurationDialog.tabID, { sensitiveAccessDuration: validation.minutes })
    const state = buildRequestConfirmState('sensitive-access', {
      tabID: sensitiveAccessDurationDialog.tabID,
      sensitiveAccessDuration: validation.minutes,
    })
    if (!state) {
      pushToast('Run this SQL successfully before creating a Sensitive Access request.', 'info', { placement: 'center' })
      setSensitiveAccessDurationDialog(null)
      return
    }

    setSensitiveAccessDurationDialog(null)
    setRequestConfirmState(state)
  }

  async function handleConfirmRequest() {
    if (!requestConfirmState) {
      return
    }

    const {
      kind,
      tabID,
      connectionId,
      sql,
      database,
      contextSchema,
      queryContextToken,
      sensitiveAccessDuration,
    } = requestConfirmState
    const sourceTab = tabs.find((tab) => tab.id === tabID) ?? null
    if (
      !queryContextToken ||
      sourceTab?.executedSQL !== sql ||
      sourceTab.executedConnectionId !== connectionId ||
      sourceTab.executedDatabase !== database ||
      sourceTab.executedSchema !== contextSchema
    ) {
      pushToast('Run this SQL successfully before creating the request.', 'info', { placement: 'center' })
      setRequestConfirmState(null)
      return
    }

    const reason = exportReason.trim()
    if (!reason) {
      pushToast(kind === 'export' ? 'Enter an export reason before submitting.' : 'Enter an access reason before submitting.', 'info', { placement: 'center' })
      return
    }

    if (kind === 'export') {
      setExportingTabIDs((current) => (current.includes(tabID) ? current : [...current, tabID]))
      try {
        const response = await createExportRequest({
          db_connection_id: connectionId,
          sql_content: sql,
          database_name: database || undefined,
          schema_name: contextSchema || undefined,
          query_context_token: queryContextToken,
          reason,
        })
        pushToast(`Export ticket ${response.ticket_no} created.`, 'success', { placement: 'center' })
        setRequestConfirmState(null)
        setExportReason('')
      } catch (error) {
        pushToast(error instanceof ApiError ? error.message : 'Failed to create export request.', 'error')
      } finally {
        setExportingTabIDs((current) => current.filter((id) => id !== tabID))
      }
      return
    }

    setSensitiveAccessTabIDs((current) => (current.includes(tabID) ? current : [...current, tabID]))
    try {
      const response = await createSensitiveAccessTicket({
        db_connection_id: connectionId,
        sql_content: sql,
        database_name: database || undefined,
        schema_name: contextSchema || undefined,
        approved_duration_minutes: sensitiveAccessDuration,
        query_context_token: queryContextToken,
        reason,
      })
      pushToast(`Sensitive Access ticket ${response.ticket_no} created.`, 'success', { placement: 'center' })
      setRequestConfirmState(null)
      setExportReason('')
    } catch (error) {
      pushToast(error instanceof ApiError ? error.message : 'Failed to create Sensitive Access ticket.', 'error')
    } finally {
      setSensitiveAccessTabIDs((current) => current.filter((id) => id !== tabID))
    }
  }

  function findSavedQueryBySignature(params: {
    connectionId: number
    sql: string
    database: string
    schema: string
    redisDbIndex?: number
  }) {
    return savedQueries.find((item) =>
      item.db_connection_id === params.connectionId &&
      item.sql_content === params.sql &&
      (item.database_name ?? '') === params.database &&
      (item.schema_name ?? '') === params.schema &&
      (item.redis_db_index ?? null) === (params.redisDbIndex ?? null),
    )
  }

  function openSaveQueryDialog() {
    const sourceSQL = activeExecutionSQL
    if (!canQuery || !activeTab?.connectionId || !sourceSQL) {
      return
    }

    const redisDbIndex = activeConnection?.db_type === 'redis' && activeDatabase ? Number(activeDatabase) : undefined
    if (findSavedQueryBySignature({
      connectionId: activeTab.connectionId,
      sql: sourceSQL,
      database: activeDatabase,
      schema: activeSchema,
      redisDbIndex,
    })) {
      pushToast('This SQL is already in your saved queries.', 'info')
      return
    }

    if (savedQueries.length >= SAVED_QUERY_LIMIT) {
      pushToast('You can save up to 20 queries.', 'error')
      return
    }

    setSaveQueryDialog({
      tabID: activeTab.id,
      label: '',
      error: '',
      connectionId: activeTab.connectionId,
      database: activeDatabase,
      schema: activeSchema,
      redisDbIndex,
      sql: sourceSQL,
    })
  }

  async function handleConfirmSaveQuery() {
    if (!saveQueryDialog) {
      return
    }
    const label = saveQueryDialog.label.trim()
    if (!label) {
      setSaveQueryDialog((current) => current ? { ...current, error: 'Enter a saved query alias.' } : current)
      return
    }
    if (label.length > SAVED_QUERY_LABEL_MAX_LENGTH) {
      setSaveQueryDialog((current) => current ? { ...current, error: `Alias must be ${SAVED_QUERY_LABEL_MAX_LENGTH} characters or fewer.` } : current)
      return
    }

    if (findSavedQueryBySignature({
      connectionId: saveQueryDialog.connectionId,
      sql: saveQueryDialog.sql,
      database: saveQueryDialog.database,
      schema: saveQueryDialog.schema,
      redisDbIndex: saveQueryDialog.redisDbIndex,
    })) {
      pushToast('This SQL is already in your saved queries.', 'info')
      setSaveQueryDialog(null)
      return
    }

    try {
      const created = await createSavedQuery({
        label,
        db_connection_id: saveQueryDialog.connectionId,
        database: saveQueryDialog.database || undefined,
        schema: saveQueryDialog.schema || undefined,
        redis_db_index: saveQueryDialog.redisDbIndex,
        sql_content: saveQueryDialog.sql,
      })
      setSavedQueries((current) => [created, ...current].slice(0, SAVED_QUERY_LIMIT))
      setSaveQueryDialog(null)
    } catch (error) {
      pushToast(error instanceof ApiError ? error.message : 'Failed to save query.', 'error')
      return
    }
    pushToast('Saved query added.', 'success')
  }

  const applySavedQuery = useCallback((entry: { connectionId: number; sql: string; label: string; database?: string | null; schema?: string | null; redisDbIndex?: number | null; preserveTitle?: boolean }) => {
    if (!activeTab) {
      return
    }

    const tabID = activeTab.id
    const connection = accessibleConnections.find((item) => item.id === entry.connectionId) ?? null
    const nextDatabase = entry.redisDbIndex !== undefined && entry.redisDbIndex !== null ? String(entry.redisDbIndex) : entry.database ?? ''
    const nextSchema = entry.redisDbIndex !== undefined && entry.redisDbIndex !== null ? '' : entry.schema ?? ''
    const loadingRootNode = connection
      ? {
        ...createConnectionNode(connection, connection.id),
        expanded: true,
        loading: true,
      }
      : null

    setActiveTabId(activeTab.id)
    updateActiveTab({
      connectionId: entry.connectionId,
      database: nextDatabase,
      schema: nextSchema,
      selectedTable: null,
      sql: entry.sql,
      ...(entry.preserveTitle ? {} : { title: entry.label }),
      result: null,
      error: '',
      columns: [],
      definition: null,
      objectMetaTab: 'columns',
      resultView: 'result',
      explorerSearch: '',
      searchTreeNodes: [],
      metadataError: connection ? '' : 'Selected query connection is no longer available.',
      ...(loadingRootNode ? { explorerNodes: [loadingRootNode] } : {}),
    })

    if (!connection) {
      return
    }

    void buildConnectionRootNodeForContext(connection, nextDatabase, nextSchema)
      .then((rootNode) => {
        updateTabByID(tabID, {
          explorerNodes: syncAssetTreeActiveStates([rootNode], connection.id, nextDatabase, nextSchema, null),
          metadataError: '',
        })
      })
      .catch((error) => {
        updateTabByID(tabID, {
          explorerNodes: [{ ...createConnectionNode(connection, connection.id), expanded: true, loading: false, loaded: true }],
          metadataError: formatMetadataError(error),
        })
      })
  }, [accessibleConnections, activeTab, updateActiveTab, updateTabByID])

  const activeRedisDbIndex = activeConnection?.db_type === 'redis' && activeDatabase ? Number(activeDatabase) : undefined
  const isFavorited = !!(activeTab && activeExecutionSQL && findSavedQueryBySignature({
    connectionId: activeTab.connectionId ?? 0,
    sql: activeExecutionSQL,
    database: activeDatabase,
    schema: activeSchema,
    redisDbIndex: activeRedisDbIndex,
  }))
  const sqlCompletionSchema = useMemo(
    () => buildSQLCompletionSchema({
      connection: activeConnection,
      explorerNodes: activeExplorerNodes,
      searchTreeNodes: activeSearchTreeNodes,
      selectedTable: activeSelectedTable,
      columns: activeColumns,
    }),
    [activeConnection, activeColumns, activeExplorerNodes, activeSearchTreeNodes, activeSelectedTable],
  )
  const sqlCompletionTables = useMemo(
    () => collectCompletionTables({
      explorerNodes: activeExplorerNodes,
      searchTreeNodes: activeSearchTreeNodes,
      selectedTable: activeSelectedTable,
    }),
    [activeExplorerNodes, activeSearchTreeNodes, activeSelectedTable],
  )
  const sqlEditorSupport = useMemo(
    () => {
      const dialect = getCodeMirrorSQLDialect(activeConnection)
      if (activeSelectedTable && activeColumns.length > 0) {
        return [
          dialect.extension,
          autocompletion({
            override: [buildFocusedSQLCompletionSource({ columns: activeColumns, tables: sqlCompletionTables })],
          }),
        ]
      }

      return sql({
        dialect,
        schema: sqlCompletionSchema,
        defaultSchema: activeConnection?.db_type === 'postgres' ? activeSchema || activeSelectedTable?.schema : undefined,
        defaultTable: activeSelectedTable?.name,
      })
    },
    [activeColumns, activeConnection, activeSchema, activeSelectedTable, sqlCompletionSchema, sqlCompletionTables],
  )
  const editorExtensions = useMemo(
    () => [
      ...(activeConnection?.db_type === 'redis' ? REDIS_EDITOR_EXTENSIONS : [sqlEditorSupport]),
      Prec.highest(
        keymap.of([
          {
            key: 'Mod-Enter',
            run: () => {
              if (activeTabRunning || !activeTab?.connectionId || !(activeSelectedSQL.trim() || activeTab.sql.trim())) {
                return true
              }
              void handleRunQuery()
              return true
            },
          },
        ]),
      ),
    ],
    [activeConnection?.db_type, activeSelectedSQL, activeTab, activeTabRunning, sqlEditorSupport],
  )
  const handleEditorChange = useCallback((value: string) => {
    updateActiveTab({ sql: value })
  }, [updateActiveTab])
  const handleEditorStatistics = useCallback((stats: { selectedText: boolean; selectionCode: string }) => {
    updateActiveTab({ selectedSQL: stats.selectedText ? stats.selectionCode : '' })
  }, [updateActiveTab])
  const visibleResultColumnIndexes = useMemo(() => {
    if (!activeTab?.result) {
      return []
    }
    if (!activeVisibleColumnIndexes || activeVisibleColumnIndexes.length === 0) {
      return activeTab.result.columns.map((_, index) => index)
    }
    return activeVisibleColumnIndexes.filter((index) => index >= 0 && index < activeTab.result!.columns.length)
  }, [activeTab?.result, activeVisibleColumnIndexes])
  const sensitiveColumnIndexSet = useMemo(
    () => new Set(activeTab?.result?.sensitive_column_indexes ?? []),
    [activeTab?.result?.sensitive_column_indexes],
  )
  const totalResultPages = useMemo(() => {
    if (!activeTab?.result) {
      return 1
    }
    return Math.max(1, Math.ceil(activeTab.result.rows.length / RESULT_PAGE_SIZE))
  }, [activeTab?.result])
  const pagedResultRows = useMemo(() => {
    if (!activeTab?.result) {
      return []
    }
    const start = (activeResultPage - 1) * RESULT_PAGE_SIZE
    return activeTab.result.rows.slice(start, start + RESULT_PAGE_SIZE)
  }, [activeResultPage, activeTab?.result])
  const detailHint = metadataHint(activeConnection?.db_type, activeSchema)
  const resultMetaLine = useMemo(() => formatResultMetaLine({
    resultView: activeResultView,
    result: activeTab?.result ?? null,
    selectedTable: activeSelectedTable,
    detailHint,
    historyCount: history.length,
    savedCount: savedQueries.length,
    currentPage: activeResultPage,
    totalPages: totalResultPages,
  }), [activeResultPage, activeResultView, activeSelectedTable, activeTab?.result, detailHint, history.length, savedQueries.length, totalResultPages])

  function handleSelectNode(node: AssetTreeNode) {
    if (activeExplorerSearch.trim() && activeConnection && node.kind !== 'connection' && node.kind !== 'redis_db') {
      const nextDatabase = node.database || (node.kind === 'database' ? node.label : activeDatabase)
      const nextSchema = node.kind === 'schema' || node.kind === 'table' ? node.schema || '' : ''
      void buildConnectionRootNodeForContext(activeConnection, nextDatabase, nextSchema)
        .then((rootNode) => {
          updateActiveTabExplorerNodes(() =>
            syncAssetTreeActiveStates([rootNode], activeConnection.id, nextDatabase, nextSchema, node.kind === 'table' ? node.item ?? null : null),
          )
        })
        .catch((error) => {
          updateActiveTab({ metadataError: formatMetadataError(error) })
        })
    }

    if (node.kind === 'connection') {
      if (activeTab?.connectionId !== node.connectionId) {
        updateActiveTab({
          connectionId: node.connectionId,
          database: '',
          schema: '',
          selectedTable: null,
          columns: [],
          definition: null,
          objectMetaTab: 'columns',
          result: null,
          error: '',
        })
      }
      return
    }

    if (node.kind === 'database') {
      updateActiveTab({
        database: node.database || node.label,
        schema: '',
        selectedTable: null,
        columns: [],
        definition: null,
        objectMetaTab: 'columns',
      })
      return
    }

    if (node.kind === 'schema') {
      updateActiveTab({
        database: node.database || activeDatabase,
        schema: node.schema || node.label,
        selectedTable: null,
        columns: [],
        definition: null,
        objectMetaTab: 'columns',
      })
      return
    }

    if (node.kind === 'table') {
      const tableItem = node.item ?? null
      updateActiveTab({
        database: node.database || activeDatabase,
        schema: node.schema || '',
        selectedTable: tableItem,
        objectMetaTab: 'columns',
        resultView: 'object-meta',
        ...(tableItem && activeTab && shouldAutofillTableQuery(activeTab.sql)
          ? { sql: buildTableSelectSQL(tableItem.name), selectedSQL: '' }
          : {}),
      })
      return
    }

    updateActiveTab({
      database: node.database || node.label.replace('DB ', ''),
      schema: '',
      selectedTable: null,
      columns: [],
      definition: null,
      objectMetaTab: 'columns',
    })
  }

  function handleSelectConnection(connection: DBConnection) {
    const tabID = activeTab?.id
    const loadingRootNode = {
      ...createConnectionNode(connection, connection.id),
      expanded: true,
      loading: true,
    }

    updateActiveTab({
      connectionId: connection.id,
      database: '',
      schema: '',
      selectedTable: null,
      columns: [],
      definition: null,
      objectMetaTab: 'columns',
      result: null,
      error: '',
      explorerSearch: '',
      assetPickerOpen: false,
      assetPickerSearch: '',
      metadataError: '',
      explorerNodes: [loadingRootNode],
      searchTreeNodes: [],
      searchIndexStatus: 'idle',
      searchIndexItems: [],
      searchIndexTruncated: false,
      searchIndexError: '',
    })

    if (tabID) {
      loadSearchIndexForTab(tabID, connection)
    }

    void buildConnectionRootNode(connection)
      .then((rootNode) => {
        if (!tabID) {
          return
        }
        updateTabByID(tabID, {
          explorerNodes: syncAssetTreeActiveStates([rootNode], connection.id, '', '', null),
          metadataError: '',
        })
      })
      .catch((error) => {
        if (!tabID) {
          return
        }
        updateTabByID(tabID, {
          explorerNodes: [{ ...loadingRootNode, loading: false, loaded: true }],
          metadataError: formatMetadataError(error),
        })
      })
  }

  function handleReloadAssets() {
    if (!activeTab || !activeConnection || activeSearchIndexStatus === 'loading' || activeExplorerRootLoading) {
      return
    }
    const tabID = activeTab.id
    const loadingRootNode = {
      ...createConnectionNode(activeConnection, activeConnection.id),
      expanded: true,
      loading: true,
    }
    updateActiveTab({
      metadataError: '',
      explorerNodes: [loadingRootNode],
      searchTreeNodes: [],
      searchIndexStatus: 'idle',
      searchIndexItems: [],
      searchIndexTruncated: false,
      searchIndexError: '',
    })
    loadSearchIndexForTab(tabID, activeConnection)
    void buildConnectionRootNode(activeConnection)
      .then((rootNode) => {
        updateTabByID(tabID, {
          explorerNodes: syncAssetTreeActiveStates([rootNode], activeConnection.id, activeDatabase, activeSchema, activeSelectedTable),
          metadataError: '',
        })
      })
      .catch((error) => {
        updateTabByID(tabID, {
          explorerNodes: [{ ...loadingRootNode, loading: false, loaded: true }],
          metadataError: formatMetadataError(error),
        })
      })
  }

  async function handleReloadConnections() {
    if (!canQuery || connectionsLoading) {
      return
    }

    setConnectionsLoading(true)
    setConnectionsError('')
    try {
      const response = await listQueryConnections()
      setConnections(response.connections)
    } catch (error) {
      setConnectionsError(error instanceof ApiError ? error.message : 'Failed to load database connections.')
    } finally {
      setConnectionsLoading(false)
    }
  }

  async function handleToggleNode(node: AssetTreeNode) {
    if (node.kind === 'table' || node.kind === 'redis_db') {
      return
    }

    if (node.kind === 'connection' && activeTab?.connectionId !== node.connectionId) {
      handleSelectNode(node)
    }

    if (!node.loaded) {
      await loadNodeChildren(node)
      return
    }

    const toggleNode = (current: AssetTreeNode[]) =>
      updateAssetTreeNode(current, node.id, (target) => ({ ...target, expanded: !target.expanded }))

    if (activeExplorerSearch.trim()) {
      updateActiveTabSearchTreeNodes(toggleNode)
      return
    }

    updateActiveTabExplorerNodes(toggleNode)
  }

  function metadataHint(dbType: string | undefined, schema: string) {
    if (dbType === 'redis') {
      return 'Redis currently supports DB 0-15 selection only. Key browsing is not available yet.'
    }
    if (dbType === 'postgres' && !schema) {
      return 'Select a schema first.'
    }
    return ''
  }

  useEffect(() => {
    if (!activeTab?.result) {
      if (activeTab?.visibleColumnIndexes !== null) {
        updateActiveTab({ visibleColumnIndexes: null })
      }
      return
    }
    const nextIndexes = activeTab.result.columns.map((_, index) => index)
    const currentIndexes = activeTab.visibleColumnIndexes
    const isSame =
      Array.isArray(currentIndexes) &&
      currentIndexes.length === nextIndexes.length &&
      currentIndexes.every((value, index) => value === nextIndexes[index])
    if (!isSame) {
      updateActiveTab({ visibleColumnIndexes: nextIndexes })
    }
  }, [activeTab?.id, activeTab?.result])

  useEffect(() => {
    if (activeResultView !== 'result' && activeResultView !== 'vertical') {
      if (activeResultPage !== 1) {
        updateActiveTab({ resultPage: 1 })
      }
      return
    }
    const nextPage = Math.min(activeResultPage, totalResultPages)
    if (nextPage !== activeResultPage) {
      updateActiveTab({ resultPage: nextPage })
    }
  }, [activeResultPage, activeResultView, activeTab?.id, activeTab?.result, totalResultPages, activeVisibleColumnIndexes])

  useLayoutEffect(() => {
    const profile = formatProfileRef.current
    const isPendingFormatProfile = Boolean(
      profile &&
      activeTab?.id === profile.tabID &&
      (activeTab?.sql ?? DEFAULT_SQL) === profile.expectedSQL,
    )

    const updateHeight = (stage: 'layout' | 'raf') => {
      const measureStartedAt = performance.now()
      const nextHeight = measureEditorHeight(editorContainerRef.current, activeTab?.sql ?? DEFAULT_SQL)
      if (activeTab?.id) {
        setEditorHeights((current) => {
          const nextValue = `${nextHeight}px`
          return current[activeTab.id] === nextValue ? current : { ...current, [activeTab.id]: nextValue }
        })
      }
      const measureMs = performance.now() - measureStartedAt

      if (profile && isPendingFormatProfile) {
        if (stage === 'layout') {
          profile.layoutMeasureMs = measureMs
          logSQLFormatProfile('layout', {
            id: profile.id,
            tabID: profile.tabID,
            layoutMeasureMs: Number(measureMs.toFixed(2)),
            elapsedMs: Number((performance.now() - profile.startedAt).toFixed(2)),
          })
        } else {
          profile.rafMeasureMs = measureMs
          profile.totalMs = performance.now() - profile.startedAt
          logSQLFormatProfile('paint', {
            id: profile.id,
            tabID: profile.tabID,
            rafMeasureMs: Number(measureMs.toFixed(2)),
            totalMs: Number((profile.totalMs ?? 0).toFixed(2)),
            formatterMs: Number(profile.formatterMs.toFixed(2)),
            stateScheduleMs: Number((profile.stateScheduleMs ?? 0).toFixed(2)),
            layoutMeasureMs: Number((profile.layoutMeasureMs ?? 0).toFixed(2)),
          })
          formatProfileRef.current = null
        }
      }
    }

    updateHeight('layout')
    const frameID = window.requestAnimationFrame(() => updateHeight('raf'))

    return () => {
      window.cancelAnimationFrame(frameID)
    }
  }, [activeTab?.id, activeTab?.sql])

  async function handleDeleteSavedQuery(entry: SavedQuery) {
    if (!canQuery) {
      return
    }
    try {
      await deleteSavedQuery(entry.id)
      setSavedQueries((current) => current.filter((item) => item.id !== entry.id))
      pushToast('Saved query deleted.', 'success')
    } catch (error) {
      pushToast(error instanceof ApiError ? error.message : 'Failed to delete saved query.', 'error')
    } finally {
      setSavedQueryToDelete(null)
    }
  }

  const toggleVisibleColumn = useCallback((index: number) => {
    const base = activeVisibleColumnIndexes ?? activeTab?.result?.columns.map((_, columnIndex) => columnIndex) ?? []
    let next: number[]
    if (base.includes(index)) {
      next = base.filter((item) => item !== index)
    } else {
      next = [...base, index].sort((left, right) => left - right)
    }
    updateActiveTab({ visibleColumnIndexes: next })
  }, [activeTab?.result?.columns, activeVisibleColumnIndexes, updateActiveTab])

  return (
    <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
      {connectionsError ? <InlineAlert>{connectionsError}</InlineAlert> : null}

      <section className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border border-border bg-panel shadow-soft">
        <div className="border-b border-border/80 px-4">
          <div className="flex flex-wrap items-center gap-5">
            {tabs.map((tab) => (
              <button
                key={tab.id}
                type="button"
                onClick={() => handleSelectTab(tab)}
                className={cn(
                  'inline-flex items-center gap-2 border-b-2 px-0.5 py-3 text-[13px] font-medium transition-colors',
                  tab.id === activeTabId
                    ? 'border-ink text-ink'
                    : 'border-transparent text-muted hover:text-ink',
                )}
              >
                <span>{tab.title}</span>
                <span
                  role="button"
                  aria-label={`Close ${tab.title}`}
                  onClick={(event) => {
                    event.stopPropagation()
                    handleCloseTab(tab.id)
                  }}
                  className="inline-flex h-4 w-4 items-center justify-center rounded-full text-muted hover:bg-panel-soft hover:text-ink"
                >
                  <X className="h-3.5 w-3.5" />
                </span>
              </button>
            ))}
            <button
              type="button"
              onClick={handleAddTab}
              disabled={tabs.length >= MAX_EDITOR_TABS}
              title={tabs.length >= MAX_EDITOR_TABS ? `最多可開啟 ${MAX_EDITOR_TABS} 個子工作區` : undefined}
              className="inline-flex items-center gap-2 border-b-2 border-transparent px-0.5 py-3 text-[13px] font-medium text-muted transition-colors hover:text-ink disabled:cursor-not-allowed disabled:opacity-45 disabled:hover:text-muted"
            >
              <Plus className="h-4 w-4" />
              New Tab
            </button>
          </div>
        </div>

        <div className="grid min-h-0 flex-1 gap-3 xl:grid-cols-[280px_minmax(0,1fr)]">
          <section className="flex min-h-0 flex-col border-r border-border/80 bg-panel">
            <div className="border-b border-border/70 px-3 py-2">
              <div className="relative">
                <button
                  type="button"
                  aria-label="Asset Selector"
                  onClick={() => updateActiveTab({ assetPickerOpen: !activeAssetPickerOpen })}
                  className="group flex min-h-10 w-full items-center gap-2 rounded-lg px-1 py-1 text-left text-[12px] text-ink transition hover:bg-panel-soft focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/30"
                >
                  <FolderTree className="h-4 w-4 shrink-0 text-muted" />
                  <div className="min-w-0 flex-1">
                    {activeConnection ? (
                      <div className="min-w-0">
                        <p className="truncate text-[12px] font-semibold leading-5 text-ink" title={activeConnection.name}>{activeConnection.name}</p>
                        <p className="text-[10px] font-medium uppercase leading-4 tracking-[0.12em] text-faint">
                          {formatConnectionBadge(activeConnection)}
                        </p>
                      </div>
                    ) : (
                      <span className="text-[13px] font-semibold text-ink">Select assets</span>
                    )}
                  </div>
                </button>

              {activeAssetPickerOpen ? (
                <div className="absolute -left-3 -right-3 top-[calc(100%+8px)] z-20 rounded-lg border border-border bg-white p-3 shadow-soft">
                  <div className="flex items-center rounded-lg border border-border bg-white px-2 transition focus-within:border-slate-400">
                    <div className="min-w-0 flex-1">
                      <SearchInput
                        aria-label="Asset Picker Search"
                        value={activeAssetPickerSearch}
                        onChange={(event) => updateActiveTab({ assetPickerSearch: event.target.value })}
                        placeholder="Search assets"
                        wrapperClassName="h-8 rounded-none border-0 bg-transparent px-0 shadow-none focus-within:border-transparent"
                      />
                    </div>
                    <button
                      type="button"
                      aria-label="Reload DB instances"
                      title="Reload DB instances"
                      onClick={() => void handleReloadConnections()}
                      disabled={!canQuery || connectionsLoading}
                      className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-muted transition hover:bg-panel-soft hover:text-ink disabled:cursor-not-allowed disabled:opacity-40"
                    >
                      <RefreshCw className={`h-3.5 w-3.5 ${connectionsLoading ? 'animate-spin' : ''}`} />
                    </button>
                  </div>
                  <div className="mt-3 max-h-[440px] overflow-auto">
                    {filteredConnections.length === 0 ? (
                      <p className="px-1 py-2 text-[12px] text-muted">No matching assets.</p>
                    ) : (
                      <div>
                        {groupedAssetPickerConnections.map((group) => (
                          <div key={group.dbType} className="border-t border-border first:border-t-0">
                            <p className="px-2 pb-1 pt-3 text-[12px] font-semibold text-muted first:pt-1">{group.label}</p>
                            <div className="grid gap-1 pb-2">
                              {group.connections.map((connection) => {
                                const selected = activeConnection?.id === connection.id
                                return (
                                  <button
                                    key={connection.id}
                                    type="button"
                                    onClick={() => handleSelectConnection(connection)}
                                    className={`flex w-full items-center rounded-md px-2.5 py-2 text-left text-[12px] ${
                                      selected
                                        ? 'bg-panel-soft text-ink ring-1 ring-border'
                                        : 'text-ink hover:bg-panel-soft'
                                    }`}
                                  >
                                    <span className="min-w-0 flex-1 break-words font-medium leading-5" title={connection.name}>{connection.name}</span>
                                  </button>
                                )
                              })}
                            </div>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                </div>
              ) : null}
              </div>
            </div>

            <div className="flex min-h-0 flex-1 flex-col px-3 pt-3 pb-3">
              {activeTab?.metadataError ? <InlineAlert className="mb-2" tone="info">{activeTab.metadataError}</InlineAlert> : null}
              <div className="flex items-center rounded-lg border border-border bg-white px-2 transition focus-within:border-slate-400">
                <div className="min-w-0 flex-1">
                  <SearchInput
                    aria-label="Explorer Search"
                    value={activeExplorerSearch}
                    onChange={(event) => updateActiveTab({ explorerSearch: event.target.value })}
                    placeholder="Search objects"
                    wrapperClassName="h-8 rounded-none border-0 bg-transparent px-0 shadow-none focus-within:border-transparent"
                  />
                </div>
                <button
                  type="button"
                  aria-label="Reload assets"
                  title="Reload assets"
                  onClick={handleReloadAssets}
                  disabled={!activeConnection || activeSearchIndexStatus === 'loading' || activeExplorerRootLoading}
                  className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-muted transition hover:bg-panel-soft hover:text-ink disabled:cursor-not-allowed disabled:opacity-40"
                >
                  <RefreshCw className={`h-3.5 w-3.5 ${activeSearchIndexStatus === 'loading' || activeExplorerRootLoading ? 'animate-spin' : ''}`} />
                </button>
              </div>
              <div className="mt-2 min-h-0 flex-1 overflow-auto">
                {connectionsLoading ? (
                  <p className="px-1 py-1 text-[12px] text-muted">Loading connections...</p>
                ) : activeExplorerRootLoading ? (
                  <p className="px-1 py-1 text-[12px] text-muted">Loading assets...</p>
                ) : activeExplorerSearch.trim() && activeExplorerSearch.trim().length < SEARCH_INDEX_MIN_KEYWORD_LENGTH ? (
                  <p className="px-1 py-1 text-[12px] text-muted">Enter at least 3 characters to search objects.</p>
                ) : activeExplorerSearch.trim() && (activeSearchingAssets || activeSearchIndexStatus === 'loading') ? (
                  <p className="px-1 py-1 text-[12px] text-muted">Loading...</p>
                ) : activeExplorerSearch.trim() && activeSearchIndexStatus === 'error' ? (
                  <p className="px-1 py-1 text-[12px] text-muted">{activeSearchIndexError || 'Object search is temporarily unavailable.'}</p>
                ) : activeExplorerSearch.trim() && activeSearchIndexTruncated ? (
                  <p className="px-1 py-1 text-[12px] text-muted">Metadata too large, narrow search by expanding database.</p>
                ) : !activeConnection || activeExplorerNodes.length === 0 ? (
                  <p className="px-1 py-1 text-[12px] text-muted">Select a connection to browse objects.</p>
                ) : renderedExplorerNodes.length === 0 ? (
                  <p className="px-1 py-1 text-[12px] text-muted">No matching assets.</p>
                ) : (
                  <AssetTree
                    nodes={renderedExplorerNodes}
                    onSelect={handleSelectNode}
                    onToggle={(node) => void handleToggleNode(node)}
                  />
                )}
              </div>
            </div>
          </section>

          <section className="flex min-h-0 flex-col bg-panel">
            {!activeTab ? (
              <LoadingBlock message="Loading editor..." className="m-4 min-h-[320px] rounded-xl border-border bg-panel" />
            ) : (
              <div className="flex min-h-0 flex-1 flex-col">
                <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2 px-4 pt-3 pb-2">
                  <div className="flex flex-wrap items-center gap-2 text-[13px] font-medium text-muted">
                    <span>Limit {queryConstraintBadges.limit}</span>
                    <span className="text-faint">·</span>
                    <span>Timeout {queryConstraintBadges.timeoutSeconds}s</span>
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <button
                      type="button"
                      onClick={handleFormatSQL}
                      disabled={!activeTab.sql.trim()}
                      className="inline-flex h-10 items-center gap-2 rounded-lg border border-border bg-white px-4 text-[13px] font-semibold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      Format
                    </button>
                    <button
                      type="button"
                      onClick={handleExplainQuery}
                      disabled={!canQuery || activeTabRunning || !activeTab.connectionId || !(activeSelectedSQL.trim() || activeTab.sql.trim())}
                      className="inline-flex h-10 items-center gap-2 rounded-lg border border-border bg-white px-4 text-[13px] font-semibold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {activeTabRunning ? 'Running...' : 'Explain'}
                    </button>
                    <button
                      type="button"
                      onClick={handleRunQuery}
                      disabled={!canQuery || activeTabRunning || !activeTab.connectionId || !(activeSelectedSQL.trim() || activeTab.sql.trim())}
                      className="inline-flex h-10 items-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      <Play className="h-4 w-4" />
                      {activeTabRunning ? 'Running...' : 'Run Query'}
                    </button>
                  </div>
                </div>

              <div className="shrink-0 px-4 pt-2 pb-3">
                <div ref={editorContainerRef} translate="no" className="overflow-hidden rounded-xl border border-border bg-panel-soft">
                  <CodeMirror
                    key={activeTab.id}
                    value={activeTab.sql}
                    height={activeEditorHeight}
                    extensions={editorExtensions}
                    onChange={handleEditorChange}
                    onStatistics={handleEditorStatistics}
                    theme="light"
                    basicSetup={SQL_EDITOR_BASIC_SETUP}
                  />
                </div>
              </div>

              <div className="flex min-h-0 flex-1 flex-col px-4 pt-2 pb-3">
                {hasSensitiveOverride || activeTab.result?.sensitive_override_active ? (
                  <div className="mb-3 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-[12px] text-amber-900">
                    <span className="font-semibold">Sensitive override active.</span> Queries and exports for this account will display unmasked data directly.
                  </div>
                ) : null}
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div className="flex flex-wrap items-center gap-3">
                    <div className="inline-flex items-center rounded-lg border border-border bg-white p-1">
                      <button
                        type="button"
                        onClick={() => updateActiveTab({ resultView: 'result' })}
                        className={`inline-flex items-center gap-2 rounded-md px-3 py-1.5 ${
                          activeResultView === 'result' ? 'bg-panel-soft text-ink' : 'text-muted hover:text-ink'
                        }`}
                      >
                        <FileClock className="h-4 w-4" />
                        Result
                      </button>
                      <button
                        type="button"
                        onClick={() => updateActiveTab({ resultView: 'vertical' })}
                        className={`inline-flex items-center gap-2 rounded-md px-3 py-1.5 ${
                          activeResultView === 'vertical' ? 'bg-panel-soft text-ink' : 'text-muted hover:text-ink'
                        }`}
                      >
                        <Layers3 className="h-4 w-4" />
                        Vertical
                      </button>
                      <button
                        type="button"
                        onClick={() => updateActiveTab({ resultView: 'object-meta' })}
                        className={`inline-flex items-center gap-2 rounded-md px-3 py-1.5 ${
                          activeResultView === 'object-meta' ? 'bg-panel-soft text-ink' : 'text-muted hover:text-ink'
                        }`}
                      >
                        <Database className="h-4 w-4" />
                        Object Meta
                      </button>
                      <button
                        type="button"
                        onClick={() => updateActiveTab({ resultView: 'history' })}
                        className={`inline-flex items-center gap-2 rounded-md px-3 py-1.5 ${
                          activeResultView === 'history' ? 'bg-panel-soft text-ink' : 'text-muted hover:text-ink'
                        }`}
                      >
                        <History className="h-4 w-4" />
                        History
                      </button>
                    </div>
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <button
                      type="button"
                      onClick={() => updateActiveTab({ resultView: 'saved' })}
                      className="inline-flex h-9 items-center gap-2 rounded-md border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      <Star className="h-4 w-4" />
                      Saved
                    </button>
                    <button
                      type="button"
                      onClick={openSaveQueryDialog}
                      disabled={!canQuery || !activeTab.connectionId || !activeExecutionSQL || isFavorited}
                      className="inline-flex h-9 items-center gap-2 rounded-md border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {isFavorited ? <StarOff className="h-4 w-4" /> : <Star className="h-4 w-4" />}
                      {isFavorited ? 'Saved' : 'Save'}
                    </button>
                    <button
                      type="button"
                      onClick={openSensitiveAccessConfirm}
                      disabled={!canApplySensitiveAccess || activeTabCreatingSensitiveAccess || !activeTab.connectionId || !activeExecutionSQL || !activeResultMatchesSQL || !activeResultHasSensitiveColumns}
                      className="inline-flex h-9 items-center gap-2 rounded-md border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {activeTabCreatingSensitiveAccess ? 'Submitting...' : 'Sensitive Access'}
                    </button>
                    {canApplyTicket ? (
                    <AttentionPulse activeKey={activeQueryAccessAttentionKey} disabled={!activeTab.connectionId}>
                      <button
                        type="button"
                        onClick={openQueryAccessTicket}
                        disabled={!activeTab.connectionId}
                        className="inline-flex h-9 items-center gap-2 rounded-md border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        Query Access
                      </button>
                    </AttentionPulse>
                    ) : null}
                    <div className="relative">
                      <button
                        type="button"
                        onClick={() => updateActiveTab({ columnFilterOpen: !activeColumnFilterOpen })}
                        disabled={!activeTab.result}
                        className="inline-flex h-9 items-center gap-2 rounded-md border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        <Filter className="h-4 w-4" />
                        Filter Columns
                      </button>
                      {activeColumnFilterOpen && activeTab.result ? (
                        <div className="absolute right-0 top-[calc(100%+8px)] z-10 w-64 rounded-lg border border-border bg-white p-3 shadow-soft">
                          <div className="mb-2 flex items-center justify-between">
                            <p className="text-[11px] font-semibold uppercase tracking-[0.12em] text-faint">Visible Columns</p>
                            <button
                              type="button"
                              onClick={() => updateActiveTab({ visibleColumnIndexes: activeTab.result?.columns.map((_, index) => index) ?? [] })}
                              className="text-[11px] font-semibold text-accent"
                            >
                              Reset
                            </button>
                          </div>
                          <div className="max-h-64 space-y-2 overflow-y-auto">
                            {activeTab.result.columns.map((column, index) => {
                              const checked = visibleResultColumnIndexes.includes(index)
                              return (
                                <label key={`${column}-${index}`} className={`flex items-center gap-2 text-[12px] ${sensitiveColumnIndexSet.has(index) ? 'text-[#b9381f]' : 'text-ink'}`}>
                                  <input
                                    type="checkbox"
                                    checked={checked}
                                    onChange={() => toggleVisibleColumn(index)}
                                  />
                                  <span className="truncate">{column}</span>
                                </label>
                              )
                            })}
                          </div>
                        </div>
                      ) : null}
                    </div>
                    <button
                      type="button"
                      onClick={openExportConfirm}
                      disabled={!canExport || activeTabExporting || !activeTab.connectionId || !activeExecutionSQL || !activeResultMatchesSQL}
                      className="inline-flex h-9 items-center gap-2 rounded-md border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      <Download className="h-4 w-4" />
                      {activeTabExporting ? 'Exporting...' : 'EXPORT'}
                    </button>
                  </div>
                </div>

                <div className="mt-3 flex flex-wrap items-center gap-2 text-[12px] text-muted">
                  {(activeResultView === 'result' || activeResultView === 'vertical') && activeTab.result ? (
                    <span className="inline-flex items-center rounded-full bg-emerald-50 px-3 py-1 font-semibold text-emerald-700">
                      Executed in {(activeTab.result.duration_ms / 1000).toFixed(activeTab.result.duration_ms >= 1000 ? 2 : 3)}s
                    </span>
                  ) : null}
                  {activeTab.lastRunAt ? (
                    <span>{formatDateTime(activeTab.lastRunAt, true)}</span>
                  ) : null}
                  <span>{resultMetaLine}</span>
                </div>

                {activeTab.error ? (
                  <div className="mt-3 space-y-2">
                    <InlineAlert>{activeTab.error}</InlineAlert>
                  </div>
                ) : null}

                <div translate="no" className="mt-3 min-h-0 flex-1 overflow-auto rounded-xl border border-border bg-white">
                  {activeResultView === 'history' ? (
                    history.length === 0 ? (
                      <div className="flex h-[180px] items-center justify-center text-[12px] text-muted">
                        No query history yet.
                      </div>
                    ) : (
                      <div className="divide-y divide-border">
                        {history.map((entry) => (
                          <button
                            key={entry.id}
                            type="button"
                            onClick={() => applySavedQuery({
                              connectionId: entry.db_connection_id,
                              sql: entry.sql_content,
                              label: entry.db_connection_name,
                              database: entry.database_name,
                              schema: entry.schema_name,
                              redisDbIndex: entry.redis_db_index,
                              preserveTitle: true,
                            })}
                            className="block w-full px-4 py-3 text-left transition hover:bg-slate-50/70"
                          >
                            <p className="truncate text-[12px] font-semibold text-ink">{entry.sql_content}</p>
                            <p className="mt-1 text-[11px] text-muted">
                              {formatHistoryContext(entry)} / {entry.duration_ms} ms / {formatDateTime(entry.created_at, true)}
                            </p>
                          </button>
                        ))}
                      </div>
                    )
                  ) : activeResultView === 'saved' ? (
                    savedQueries.length === 0 ? (
                      <div className="flex h-[180px] items-center justify-center text-[12px] text-muted">
                        No saved queries yet.
                      </div>
                    ) : (
                      <div className="divide-y divide-border">
                        {savedQueries.map((entry) => (
                          <div key={entry.id} className="flex items-start justify-between gap-3 px-4 py-3 transition hover:bg-slate-50/70">
                            <button
                              type="button"
                              onClick={() => applySavedQuery({
                                connectionId: entry.db_connection_id,
                                sql: entry.sql_content,
                                label: entry.label,
                                database: entry.database_name,
                                schema: entry.schema_name,
                                redisDbIndex: entry.redis_db_index,
                              })}
                              className="min-w-0 flex-1 text-left"
                            >
                              <div className="flex items-center justify-between gap-3">
                                <p className="truncate text-[12px] font-semibold text-ink">{entry.label}</p>
                                <span className="shrink-0 text-[10px] text-muted">{entry.db_connection_name}</span>
                              </div>
                              <p className="mt-1 truncate text-[11px] text-muted">{entry.sql_content}</p>
                            </button>
                            {canQuery ? (
                              <button
                                type="button"
                                onClick={() => setSavedQueryToDelete(entry)}
                                className="inline-flex h-8 w-8 items-center justify-center rounded-md border border-border bg-white text-muted transition hover:bg-page hover:text-danger"
                                aria-label={`Delete saved query ${entry.label}`}
                              >
                                <Trash2 className="h-4 w-4" />
                              </button>
                            ) : null}
                          </div>
                        ))}
                      </div>
                    )
                  ) : activeResultView === 'object-meta' ? (
                    !activeSelectedTable ? (
                      <div className="flex h-[180px] items-center justify-center text-[12px] text-muted">
                        {detailHint || 'Select a table from the asset tree to inspect its structure.'}
                      </div>
                    ) : (
                      <div className="flex min-h-[180px] flex-col">
                        <div className="border-b border-border px-3 py-2">
                          <div className="inline-flex items-center rounded-lg border border-border bg-white p-1">
                            <button
                              type="button"
                              onClick={() => updateActiveTab({ objectMetaTab: 'columns' })}
                              className={`inline-flex items-center gap-2 rounded-md px-3 py-1.5 text-[12px] font-medium ${
                                activeObjectMetaTab === 'columns' ? 'bg-panel-soft text-ink' : 'text-muted hover:text-ink'
                              }`}
                            >
                              Columns
                            </button>
                            <button
                              type="button"
                              onClick={() => updateActiveTab({ objectMetaTab: 'definition' })}
                              className={`inline-flex items-center gap-2 rounded-md px-3 py-1.5 text-[12px] font-medium ${
                                activeObjectMetaTab === 'definition' ? 'bg-panel-soft text-ink' : 'text-muted hover:text-ink'
                              }`}
                            >
                              Definition
                            </button>
                          </div>
                        </div>
                        {activeObjectMetaTab === 'columns' ? (
                          activeColumnsLoading ? (
                            <div className="flex h-[180px] items-center justify-center text-[12px] text-muted">
                              Loading table structure...
                            </div>
                          ) : activeColumns.length === 0 ? (
                            <div className="flex h-[180px] items-center justify-center text-[12px] text-muted">
                              No table structure available.
                            </div>
                          ) : (
                            <DataTable>
                              <DataTableHead>
                                <tr>
                                  <DataTableHeaderCell>Column</DataTableHeaderCell>
                                  <DataTableHeaderCell>Type</DataTableHeaderCell>
                                  <DataTableHeaderCell>Nullable</DataTableHeaderCell>
                                  <DataTableHeaderCell>Default</DataTableHeaderCell>
                                </tr>
                              </DataTableHead>
                              <DataTableBody>
                                {activeColumns.map((column) => (
                                  <DataTableRow key={column.name}>
                                    <DataTableCell>{column.name}</DataTableCell>
                                    <DataTableCell>{column.column_type}</DataTableCell>
                                    <DataTableCell>{column.is_nullable}</DataTableCell>
                                    <DataTableCell>{column.default || <span className="text-muted">(none)</span>}</DataTableCell>
                                  </DataTableRow>
                                ))}
                              </DataTableBody>
                            </DataTable>
                          )
                        ) : activeDefinitionLoading ? (
                          <div className="flex h-[180px] items-center justify-center text-[12px] text-muted">
                            Loading definition...
                          </div>
                        ) : !activeDefinition?.definition.trim() ? (
                          <div className="flex h-[180px] items-center justify-center text-[12px] text-muted">
                            No definition available.
                          </div>
                        ) : (
                          <pre className="overflow-x-auto px-4 py-3 font-mono text-[12px] leading-6 text-ink">{activeDefinition.definition}</pre>
                        )}
                      </div>
                    )
                  ) : activeResultView === 'vertical' ? (
                    !activeTab.result ? (
                      <div className="flex h-[180px] items-center justify-center text-[12px] text-muted">
                        No query has been executed yet.
                      </div>
                    ) : (
                      <div className="divide-y divide-border">
                        {pagedResultRows.map((row, rowOffset) => (
                          <div key={`${activeTab.id}-vertical-${activeResultPage}-${rowOffset}`} className="px-4 py-2.5">
                            <p className="mb-2 text-[10px] font-bold uppercase tracking-[0.14em] text-faint">
                              Row {(activeResultPage - 1) * RESULT_PAGE_SIZE + rowOffset + 1}
                            </p>
                            <div className="overflow-hidden rounded-lg border border-border bg-panel-soft">
                              {visibleResultColumnIndexes.map((columnIndex) => (
                                <div
                                  key={`${activeTab.id}-vertical-${rowOffset}-${columnIndex}`}
                                  className="grid grid-cols-[120px_minmax(0,1fr)] gap-3 border-t border-border px-3 py-2 first:border-t-0 sm:grid-cols-[160px_minmax(0,1fr)]"
                                >
                                  <p className={`text-[10px] font-bold uppercase tracking-[0.14em] ${sensitiveColumnIndexSet.has(columnIndex) ? 'text-[#b9381f]' : 'text-faint'}`}>
                                    {activeTab.result?.columns[columnIndex]}
                                  </p>
                                  <p className="break-all text-[12px] text-ink">
                                    {!Array.isArray(row)
                                      ? <span className="text-muted">(empty)</span>
                                      : row[columnIndex] === null
                                        ? <span className="text-muted">(null)</span>
                                        : String(row[columnIndex])}
                                  </p>
                                </div>
                              ))}
                            </div>
                          </div>
                        ))}
                      </div>
                    )
                  ) : activeTab.result ? (
                    <DataTable>
                      <DataTableHead>
                        <tr>
                          {visibleResultColumnIndexes.map((columnIndex) => (
                            <DataTableHeaderCell
                              key={`${activeTab.result?.columns[columnIndex]}-${columnIndex}`}
                              className={sensitiveColumnIndexSet.has(columnIndex) ? 'text-[#b9381f]' : ''}
                            >
                              {activeTab.result?.columns[columnIndex]}
                            </DataTableHeaderCell>
                          ))}
                        </tr>
                      </DataTableHead>
                      <DataTableBody>
                        {pagedResultRows.map((row, rowOffset) => (
                          <DataTableRow key={`${activeTab.id}-row-${activeResultPage}-${rowOffset}`}>
                            {visibleResultColumnIndexes.map((columnIndex) => (
                              <DataTableCell key={`${activeTab.id}-cell-${rowOffset}-${columnIndex}`} className="align-top">
                                {!Array.isArray(row)
                                  ? <span className="text-muted">(empty)</span>
                                  : row[columnIndex] === null
                                    ? <span className="text-muted">(null)</span>
                                    : String(row[columnIndex])}
                              </DataTableCell>
                            ))}
                          </DataTableRow>
                        ))}
                      </DataTableBody>
                    </DataTable>
                  ) : (
                    <div className="flex h-[180px] items-center justify-center text-[12px] text-muted">
                      No query has been executed yet.
                    </div>
                  )}
                </div>
                {(activeResultView === 'result' || activeResultView === 'vertical') && activeTab.result && totalResultPages > 1 ? (
                  <div className="mt-3">
                    <Pagination
                      offset={(activeResultPage - 1) * RESULT_PAGE_SIZE}
                      pageSize={RESULT_PAGE_SIZE}
                      count={Math.min(activeTab.result.rows.length - (activeResultPage - 1) * RESULT_PAGE_SIZE, RESULT_PAGE_SIZE)}
                      total={activeTab.result.rows.length}
                      onChange={(nextOffset) => updateActiveTab({ resultPage: Math.floor(nextOffset / RESULT_PAGE_SIZE) + 1 })}
                    />
                  </div>
                ) : null}
              </div>
              </div>
            )}
          </section>
        </div>
      </section>
      <ConfirmDialog
        open={sensitiveAccessDurationDialog !== null}
        title="Set Sensitive Access Duration"
        description={sensitiveAccessDurationDialog ? (
          <div className="space-y-4">
            <p className="text-[13px] leading-6 text-muted">
              Enter the temporary access duration in minutes. Maximum {MAX_SENSITIVE_ACCESS_DURATION_MINUTES} minutes
              (3 days). You will review the request details in the next step.
            </p>
            <div className="space-y-2">
              <p className="text-[11px] font-semibold uppercase tracking-[0.12em] text-faint">Quick Select</p>
              <div className="flex flex-wrap gap-2">
                {SENSITIVE_ACCESS_DURATION_PRESETS.map((preset) => (
                  <button
                    key={preset.minutes}
                    type="button"
                    onClick={() =>
                      setSensitiveAccessDurationDialog((current) => (
                        current
                          ? {
                              ...current,
                              value: String(preset.minutes),
                              error: '',
                            }
                          : current
                      ))
                    }
                    className={cn(
                      'inline-flex h-8 items-center rounded-md border px-3 text-[12px] font-semibold transition',
                      sensitiveAccessDurationDialog.value.trim() === String(preset.minutes)
                        ? 'border-accent bg-accent/10 text-accent'
                        : 'border-border bg-white text-ink hover:bg-page',
                    )}
                  >
                    {preset.label}
                  </button>
                ))}
              </div>
            </div>
            <div className="space-y-2">
              <label
                htmlFor="sensitive-access-duration"
                className="text-[11px] font-semibold uppercase tracking-[0.12em] text-faint"
              >
                Requested Access Duration (minutes)
              </label>
              <input
                id="sensitive-access-duration"
                type="number"
                min={1}
                max={MAX_SENSITIVE_ACCESS_DURATION_MINUTES}
                step={1}
                inputMode="numeric"
                value={sensitiveAccessDurationDialog.value}
                onChange={(event) =>
                  setSensitiveAccessDurationDialog((current) => (
                    current
                      ? {
                          ...current,
                          value: event.target.value,
                          error: '',
                        }
                      : current
                  ))
                }
                className="h-10 w-full rounded-control border border-border bg-panel px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
              />
              <p className="text-[12px] text-faint">Examples: 30 for 30 minutes, 120 for 2 hours, 1440 for 1 day.</p>
              {sensitiveAccessDurationDialog.error ? (
                <p className="text-[12px] font-medium text-danger">{sensitiveAccessDurationDialog.error}</p>
              ) : null}
            </div>
            {(() => {
              const validation = validateSensitiveAccessDurationInput(sensitiveAccessDurationDialog.value)
              if ('error' in validation) {
                return null
              }

              return (
                <div className="rounded-xl border border-border bg-white/80 p-3">
                  <p className="text-[11px] font-semibold uppercase tracking-[0.12em] text-faint">Access Preview</p>
                  <div className="mt-3 grid gap-2 sm:grid-cols-2">
                    <div>
                      <p className="text-[11px] font-semibold text-faint">Human Readable</p>
                      <p className="mt-1 text-[13px] font-semibold text-ink">{formatSensitiveAccessDuration(validation.minutes)}</p>
                    </div>
                    <div>
                      <p className="text-[11px] font-semibold text-faint">Expires At</p>
                      <p className="mt-1 text-[13px] font-semibold text-ink">{formatSensitiveAccessExpiry(validation.minutes)}</p>
                    </div>
                  </div>
                </div>
              )
            })()}
          </div>
        ) : null}
        confirmLabel="Continue"
        cancelLabel="Cancel"
        panelClassName="max-w-lg"
        onCancel={() => setSensitiveAccessDurationDialog(null)}
        onConfirm={handleSensitiveAccessDurationContinue}
      />
      <ConfirmDialog
        open={requestConfirmState !== null}
        title={requestConfirmState?.kind === 'sensitive-access' ? 'Confirm Sensitive Access Request' : 'Confirm Export Request'}
        description={requestConfirmState ? (
          <div className="space-y-4">
            <div className="rounded-xl border border-border bg-white/80 p-3">
              <p className="text-[11px] font-semibold uppercase tracking-[0.12em] text-faint">Asset Context</p>
              <div className="mt-3 grid gap-2 sm:grid-cols-2">
                <div>
                  <p className="text-[11px] font-semibold text-faint">Connection</p>
                  <p className="mt-1 text-[13px] font-semibold text-ink">{requestConfirmState.connectionName}</p>
                </div>
                <div>
                  <p className="text-[11px] font-semibold text-faint">Type</p>
                  <p className="mt-1 text-[13px] font-semibold uppercase text-ink">{requestConfirmState.connectionType}</p>
                </div>
                <div>
                  <p className="text-[11px] font-semibold text-faint">Database</p>
                  <p className="mt-1 text-[13px] text-ink">{requestConfirmState.database || 'Not selected'}</p>
                </div>
                <div>
                  <p className="text-[11px] font-semibold text-faint">Schema</p>
                  <p className="mt-1 text-[13px] text-ink">{requestConfirmState.schema || 'Not selected'}</p>
                </div>
                <div className="sm:col-span-2">
                  <p className="text-[11px] font-semibold text-faint">Selected Table</p>
                  <p className="mt-1 text-[13px] text-ink">{requestConfirmState.tableName || 'No table selected in asset tree'}</p>
                </div>
                {requestConfirmState.kind === 'sensitive-access' ? (
                  <div className="sm:col-span-2">
                    <p className="text-[11px] font-semibold text-faint">Requested Access Duration</p>
                    <p className="mt-1 text-[13px] text-ink">{requestConfirmState.sensitiveAccessDuration} minutes</p>
                  </div>
                ) : null}
              </div>
            </div>
            <div className="space-y-2">
              <label
                htmlFor="request-reason"
                className="text-[11px] font-semibold uppercase tracking-[0.12em] text-faint"
              >
                {requestConfirmState.kind === 'export' ? 'Export Reason' : 'Access Reason'}
              </label>
              <textarea
                id="request-reason"
                value={exportReason}
                onChange={(event) => setExportReason(event.target.value)}
                rows={3}
                className="w-full resize-y rounded-control border border-border bg-panel px-3 py-2 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                placeholder={requestConfirmState.kind === 'export' ? 'Explain why this data export is needed.' : 'Explain why unmasked sensitive data access is needed.'}
              />
            </div>
            <div className="rounded-xl border border-border bg-slate-950 p-3">
              <p className="text-[11px] font-semibold uppercase tracking-[0.12em] text-slate-300">SQL To Submit</p>
              <pre className="mt-3 max-h-72 overflow-auto whitespace-pre-wrap break-all font-mono text-[12px] leading-5 text-slate-100">
                {requestConfirmState.sql}
              </pre>
            </div>
          </div>
        ) : null}
        confirmLabel={requestConfirmState?.kind === 'sensitive-access' ? 'Confirm and Submit' : 'Confirm and Export'}
        cancelLabel="Cancel"
        loading={requestConfirmLoading}
        confirmDisabled={requestConfirmState !== null && exportReason.trim() === ''}
        panelClassName="max-w-3xl"
        onCancel={() => {
          setRequestConfirmState(null)
          setExportReason('')
        }}
        onConfirm={() => {
          void handleConfirmRequest()
        }}
      />
      <ConfirmDialog
        open={saveQueryDialog !== null}
        title="Save SQL"
        description={saveQueryDialog ? (
          <div className="space-y-4">
            <div className="space-y-2">
              <label
                htmlFor="saved-query-alias"
                className="text-[11px] font-semibold uppercase tracking-[0.12em] text-faint"
              >
                Alias
              </label>
              <input
                id="saved-query-alias"
                value={saveQueryDialog.label}
                maxLength={SAVED_QUERY_LABEL_MAX_LENGTH}
                onChange={(event) =>
                  setSaveQueryDialog((current) => (
                    current
                      ? {
                          ...current,
                          label: event.target.value,
                          error: '',
                        }
                      : current
                  ))
                }
                className="h-10 w-full rounded-control border border-border bg-panel px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                placeholder="Name this saved SQL"
                autoFocus
              />
              {saveQueryDialog.error ? (
                <p className="text-[12px] font-medium text-danger">{saveQueryDialog.error}</p>
              ) : null}
            </div>
            <div className="rounded-xl border border-border bg-slate-950 p-3">
              <p className="text-[11px] font-semibold uppercase tracking-[0.12em] text-slate-300">SQL To Save</p>
              <pre className="mt-3 max-h-56 overflow-auto whitespace-pre-wrap break-all font-mono text-[12px] leading-5 text-slate-100">
                {saveQueryDialog.sql}
              </pre>
            </div>
          </div>
        ) : null}
        confirmLabel="Save"
        cancelLabel="Cancel"
        confirmDisabled={saveQueryDialog !== null && saveQueryDialog.label.trim() === ''}
        panelClassName="max-w-2xl"
        onCancel={() => setSaveQueryDialog(null)}
        onConfirm={() => {
          void handleConfirmSaveQuery()
        }}
      />
      <ConfirmDialog
        open={savedQueryToDelete !== null}
        title="Delete Saved Query"
        description={savedQueryToDelete ? `Delete "${savedQueryToDelete.label}"? If you were at the 20-query limit, deleting it will free up a slot.` : ''}
        confirmLabel="Delete"
        cancelLabel="Cancel"
        tone="danger"
        onCancel={() => setSavedQueryToDelete(null)}
        onConfirm={() => {
          if (savedQueryToDelete) {
            void handleDeleteSavedQuery(savedQueryToDelete)
          }
        }}
      />
    </div>
  )
}
