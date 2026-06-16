import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import CodeMirror from '@uiw/react-codemirror'
import { javascript } from '@codemirror/lang-javascript'
import { sql } from '@codemirror/lang-sql'
import { format as formatSQL, type SqlLanguage } from 'sql-formatter'
import {
  Check,
  ChevronDown,
  ChevronRight,
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
  Search,
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
import type { DBConnection } from '@/shared/types/dbConnection'
import type { MetadataColumn, MetadataDefinition, MetadataItem, QueryHistoryEntry, QueryResult, SavedQuery } from '@/shared/types/sqlEditor'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { PageIntro } from '@/shared/ui/PageIntro'
import { Pagination } from '@/shared/ui/Pagination'
import { useToast } from '@/shared/ui/ToastContext'
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog'
import { createExportRequest } from '@/modules/exports/api'
import {
  createSavedQuery,
  createSensitiveAccessTicket,
  deleteSavedQuery,
  executeQuery,
  listQueryConnections,
  listMetadata,
  listMetadataColumns,
  listMetadataDefinition,
  listQueryHistory,
  listSavedQueries,
} from '@/modules/sql-editor/api'

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

const DEFAULT_SQL = 'SELECT 1;'
const HISTORY_LIMIT = 20
const SAVED_QUERY_LIMIT = 10
const EDITOR_BASE_VISIBLE_LINES = 12
const EDITOR_MAX_HEIGHT = 840
const EDITOR_LINE_HEIGHT = 24
const EDITOR_VERTICAL_PADDING = 24
const EDITOR_MIN_HEIGHT = EDITOR_VERTICAL_PADDING + EDITOR_BASE_VISIBLE_LINES * EDITOR_LINE_HEIGHT
const RESULT_PAGE_SIZE = 50
const METADATA_ERROR_MESSAGE = 'Metadata is temporarily unavailable. Please try again later.'
const SQL_EDITOR_PROFILE_ENABLED = import.meta.env.DEV
const SQL_EDITOR_EXTENSIONS = [sql()]
const REDIS_EDITOR_EXTENSIONS = [javascript()]
const SQL_EDITOR_BASIC_SETUP = {
  lineNumbers: true,
  foldGutter: false,
  highlightActiveLine: false,
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
    schema: connection?.db_type === 'postgres' ? tab.schema || undefined : undefined,
    redis_db_index: connection?.db_type === 'redis' && tab.database ? Number(tab.database) : undefined,
  }
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
    throw new Error('Explain 目前只支援單一 SQL statement。')
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
    sensitiveAccessDuration: 10,
    resultPage: 1,
    sql: DEFAULT_SQL,
    result: null,
    error: '',
    lastRunAt: null,
  }
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

async function buildSearchNodes(connection: DBConnection, keyword: string): Promise<AssetTreeNode[]> {
  const rootNode = createConnectionNode(connection, connection.id)
  rootNode.expanded = true
  rootNode.loaded = true

  if (connection.db_type === 'redis') {
    const response = await listMetadata(connection.id)
    rootNode.children = response.items
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

    return rootNode.children.length > 0 || matchesAssetKeyword(rootNode, keyword) ? [rootNode] : []
  }

  const databaseResponse = await listMetadata(connection.id)
  const databaseNodes = await Promise.all(databaseResponse.items.map(async (databaseItem) => {
    const databaseNode: AssetTreeNode = {
      id: `database-${connection.id}-${databaseItem.name}`,
      kind: 'database',
      connectionId: connection.id,
      label: databaseItem.name,
      database: databaseItem.name,
      schema: databaseItem.schema,
      active: false,
      selectable: true,
      expanded: true,
      loaded: true,
      loading: false,
      item: databaseItem,
      children: [],
    }

    if (connection.db_type === 'postgres') {
      const schemaResponse = await listMetadata(connection.id, { database: databaseItem.name })
      const schemaNodes = await Promise.all(schemaResponse.items.map(async (schemaItem) => {
        const schemaNode: AssetTreeNode = {
          id: `schema-${connection.id}-${databaseItem.name}-${schemaItem.name}`,
          kind: 'schema',
          connectionId: connection.id,
          label: schemaItem.name,
          database: databaseItem.name,
          schema: schemaItem.name,
          active: false,
          selectable: true,
          expanded: true,
          loaded: true,
          loading: false,
          item: schemaItem,
          children: [],
        }

        const tableResponse = await listMetadata(connection.id, {
          database: databaseItem.name,
          schema: schemaItem.name,
        })

        schemaNode.children = tableResponse.items
          .map((tableItem) => ({
            id: `table-${connection.id}-${databaseItem.name}-${tableItem.schema}-${tableItem.name}`,
            kind: 'table' as const,
            connectionId: connection.id,
            label: tableItem.name,
            database: databaseItem.name,
            schema: tableItem.schema,
            active: false,
            selectable: true,
            expanded: false,
            loaded: true,
            loading: false,
            item: tableItem,
            children: [],
          }))
          .filter((node) => matchesAssetKeyword(node, keyword))

        return matchesAssetKeyword(schemaNode, keyword) || schemaNode.children.length > 0 ? schemaNode : null
      }))

      databaseNode.children = schemaNodes.filter((node): node is AssetTreeNode => node !== null)
      return matchesAssetKeyword(databaseNode, keyword) || databaseNode.children.length > 0 ? databaseNode : null
    }

    const tableResponse = await listMetadata(connection.id, { database: databaseItem.name })
    databaseNode.children = tableResponse.items
      .map((tableItem) => ({
        id: `table-${connection.id}-${databaseItem.name}-${tableItem.schema}-${tableItem.name}`,
        kind: 'table' as const,
        connectionId: connection.id,
        label: tableItem.name,
        database: databaseItem.name,
        schema: tableItem.schema,
        active: false,
        selectable: true,
        expanded: false,
        loaded: true,
        loading: false,
        item: tableItem,
        children: [],
      }))
      .filter((node) => matchesAssetKeyword(node, keyword))

    return matchesAssetKeyword(databaseNode, keyword) || databaseNode.children.length > 0 ? databaseNode : null
  }))

  rootNode.children = databaseNodes.filter((node): node is AssetTreeNode => node !== null)
  return rootNode.children.length > 0 || matchesAssetKeyword(rootNode, keyword) ? [rootNode] : []
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

    return (
      <div key={node.id}>
        <div
          className={`group flex items-center rounded-md border border-transparent pr-2 text-[12px] ${
            node.active ? 'bg-panel-soft text-ink' : 'text-muted hover:border-border/70 hover:bg-panel-soft'
          }`}
          style={{ paddingLeft }}
        >
          <button
            type="button"
            onClick={() => onToggle(node)}
            className="mr-1 inline-flex h-6 w-5 items-center justify-center rounded text-faint hover:text-ink"
            aria-label={`Toggle ${node.label}`}
          >
            {canExpand ? (
              node.expanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />
            ) : (
              <span className="h-3.5 w-3.5" />
            )}
          </button>
          <button
            type="button"
            onClick={() => onSelect(node)}
            className="flex min-w-0 flex-1 items-center gap-2 py-1.5 text-left"
          >
            <span className="flex h-4 w-4 items-center justify-center text-muted">{iconForNode(node)}</span>
            <span className="truncate font-medium">{node.label}</span>
            {node.loading ? <span className="text-[10px] font-semibold text-faint">Loading…</span> : null}
          </button>
        </div>
        {node.expanded && hasChildren ? <div className="mt-0.5 space-y-0.5">{node.children.map((child) => renderNode(child, depth + 1))}</div> : null}
      </div>
    )
  }

  return <div className="space-y-0.5">{nodes.map((node) => renderNode(node))}</div>
}

export function SQLEditorPage() {
  const editorContainerRef = useRef<HTMLDivElement | null>(null)
  const formatProfileRef = useRef<SQLFormatProfile | null>(null)
  const formatProfileIDRef = useRef(0)
  const { user } = useAuth()
  const { pushToast } = useToast()
  const hasSensitiveOverride = Boolean(user?.permissions.includes('global.sensitive'))
  const canApplySensitiveAccess = Boolean(user?.permissions.includes('sql_editor.sensitive_apply'))
  const accessibleConnectionIDs = user?.dbConnectionIds ?? []
  const [connections, setConnections] = useState<DBConnection[]>([])
  const [connectionsLoading, setConnectionsLoading] = useState(true)
  const [connectionsError, setConnectionsError] = useState('')
  const [tabs, setTabs] = useState<EditorTab[]>([createTab()])
  const [activeTabId, setActiveTabId] = useState<string>(() => createTab().id)
  const [history, setHistory] = useState<QueryHistoryEntry[]>([])
  const [savedQueries, setSavedQueries] = useState<SavedQuery[]>([])
  const [runningTabId, setRunningTabId] = useState<string | null>(null)
  const [exportingTabId, setExportingTabId] = useState<string | null>(null)
  const [savedQueryToDelete, setSavedQueryToDelete] = useState<SavedQuery | null>(null)
  const [editorHeight, setEditorHeight] = useState(`${EDITOR_MIN_HEIGHT}px`)

  useEffect(() => {
    let active = true

    async function loadConnections() {
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
  }, [])

  useEffect(() => {
    let active = true

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
  }, [])

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
  const activeSensitiveAccessDuration = activeTab?.sensitiveAccessDuration ?? 10
  const activeResultPage = activeTab?.resultPage ?? 1
  const filteredExplorerNodes = useMemo(
    () => (activeExplorerSearch.trim() ? activeSearchTreeNodes : filterAssetTree(activeExplorerNodes, activeExplorerSearch)),
    [activeExplorerNodes, activeExplorerSearch, activeSearchTreeNodes],
  )
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

  useEffect(() => {
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
  }, [accessibleConnections])

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
    const keyword = activeExplorerSearch.trim()
    if (!keyword || !activeConnection) {
      if (activeTab?.searchTreeNodes.length || activeTab?.searchingAssets) {
        updateActiveTab({ searchTreeNodes: [], searchingAssets: false })
      }
      return
    }

    let active = true
    const tabID = activeTab?.id
    updateActiveTab({ searchingAssets: true })

    void buildSearchNodes(activeConnection, keyword)
      .then((nodes) => {
        if (!active || !tabID) {
          return
        }
        updateTabByID(tabID, { searchTreeNodes: nodes })
      })
      .catch((error) => {
        if (!active || !tabID) {
          return
        }
        updateTabByID(tabID, { metadataError: formatMetadataError(error) })
        updateTabByID(tabID, { searchTreeNodes: [] })
      })
      .finally(() => {
        if (active && tabID) {
          updateTabByID(tabID, { searchingAssets: false })
        }
      })

    return () => {
      active = false
    }
  }, [activeConnection, activeExplorerSearch, activeTab?.id])

  useEffect(() => {
    if (!activeExplorerSearch.trim()) {
      return
    }

    updateActiveTabSearchTreeNodes((current) =>
      syncAssetTreeActiveStates(current, activeTab?.connectionId ?? null, activeDatabase, activeSchema, activeSelectedTable),
    )
  }, [activeDatabase, activeExplorerSearch, activeSchema, activeSelectedTable, activeTab?.connectionId])

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
    const nextTab = createTab(tabs.length + 1)
    setTabs((current) => [...current, nextTab])
    setActiveTabId(nextTab.id)
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
    const sqlToExecute = activeExecutionSQL
    if (!activeTab?.connectionId || !sqlToExecute) {
      updateActiveTab({ error: 'Select a database connection and enter a query first.' })
      return
    }

    setRunningTabId(activeTab.id)
    updateActiveTab({ error: '' })

    try {
      const finalSQL = mode === 'explain' ? buildExplainSQL(sqlToExecute) : sqlToExecute
      const result = await executeQuery(buildQueryPayload(activeTab, finalSQL, activeConnection ?? null))

      updateActiveTab({
        result,
        error: '',
        lastRunAt: new Date().toISOString(),
        resultView: 'result',
      })
      void listQueryHistory(HISTORY_LIMIT).then((response) => setHistory(response.history)).catch(() => undefined)
      pushToast(mode === 'explain' ? 'Explain completed.' : 'Query completed.', 'success')
    } catch (error) {
      const message = error instanceof ApiError || error instanceof Error ? error.message : 'Query execution failed.'
      updateActiveTab({
        error: message,
        result: null,
      })
    } finally {
      setRunningTabId(null)
    }
  }

  async function handleRunQuery() {
    await executeEditorSQL('run')
  }

  async function handleExplainQuery() {
    await executeEditorSQL('explain')
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

  async function handleExport() {
    if (!activeTab?.connectionId || !activeExecutionSQL) {
      return
    }

    setExportingTabId(activeTab.id)
    try {
      const response = await createExportRequest({
        db_connection_id: activeTab.connectionId,
        sql_content: activeExecutionSQL,
        database_name: activeDatabase || undefined,
        schema_name: activeConnection?.db_type === 'postgres' ? activeSchema || undefined : undefined,
      })
      pushToast(`Export ticket ${response.ticket_no} created.`, 'success', { placement: 'center' })
    } catch (error) {
      pushToast(error instanceof ApiError ? error.message : 'Failed to create export request.', 'error')
    } finally {
      setExportingTabId(null)
    }
  }

  async function handleCreateSensitiveAccess() {
    if (!activeTab?.connectionId || !activeExecutionSQL) {
      return
    }
    if (activeConnection?.db_type !== 'mysql') {
      pushToast('Sensitive Access currently supports MySQL only.', 'info', { placement: 'center' })
      return
    }

    try {
      const response = await createSensitiveAccessTicket({
        db_connection_id: activeTab.connectionId,
        sql_content: activeExecutionSQL,
        database_name: activeDatabase || undefined,
        schema_name: activeSchema || undefined,
        approved_duration_minutes: activeSensitiveAccessDuration,
      })
      pushToast(`Sensitive Access ticket ${response.ticket_no} created.`, 'success', { placement: 'center' })
    } catch (error) {
      pushToast(error instanceof ApiError ? error.message : 'Failed to create Sensitive Access ticket.', 'error')
    }
  }

  async function handleSaveQuery() {
    if (!activeTab?.connectionId || !activeTab.sql.trim()) {
      return
    }

    const existing = savedQueries.find((item) =>
      item.db_connection_id === activeTab.connectionId &&
      item.sql_content === activeTab.sql &&
      (item.database_name ?? '') === activeDatabase &&
      (item.schema_name ?? '') === activeSchema &&
      (item.redis_db_index ?? null) === (activeConnection?.db_type === 'redis' && activeDatabase ? Number(activeDatabase) : null),
    )
    if (existing) {
      pushToast('This SQL is already in your saved queries.', 'info')
      return
    }

    if (savedQueries.length >= SAVED_QUERY_LIMIT) {
      pushToast('You can save up to 10 queries.', 'error')
      return
    }

    try {
      const created = await createSavedQuery({
        label: activeTab.title,
        db_connection_id: activeTab.connectionId,
        database: activeDatabase || undefined,
        schema: activeSchema || undefined,
        redis_db_index: activeConnection?.db_type === 'redis' && activeDatabase ? Number(activeDatabase) : undefined,
        sql_content: activeTab.sql,
      })
      setSavedQueries((current) => [created, ...current].slice(0, SAVED_QUERY_LIMIT))
    } catch (error) {
      pushToast(error instanceof ApiError ? error.message : 'Failed to save query.', 'error')
      return
    }
    pushToast('Saved query added.', 'success')
  }

  const applySavedQuery = useCallback((entry: { connectionId: number; sql: string; label: string; database?: string | null; schema?: string | null; redisDbIndex?: number | null }) => {
    if (!activeTab) {
      return
    }

    setActiveTabId(activeTab.id)
    updateActiveTab({
      connectionId: entry.connectionId,
      database: entry.database ?? '',
      schema: entry.schema ?? '',
      selectedTable: null,
      sql: entry.sql,
      title: entry.label,
      result: null,
      error: '',
      columns: [],
      definition: null,
      objectMetaTab: 'columns',
      resultView: 'result',
    })
    if (entry.redisDbIndex !== undefined && entry.redisDbIndex !== null) {
      updateActiveTab({
        database: String(entry.redisDbIndex),
        schema: '',
        selectedTable: null,
      })
    }
  }, [activeTab, updateActiveTab])

  const isFavorited = !!(activeTab && savedQueries.some((item) =>
    item.db_connection_id === activeTab.connectionId &&
    item.sql_content === activeTab.sql &&
    (item.database_name ?? '') === activeDatabase &&
    (item.schema_name ?? '') === activeSchema &&
    (item.redis_db_index ?? null) === (activeConnection?.db_type === 'redis' && activeDatabase ? Number(activeDatabase) : null),
  ))
  const editorExtensions = useMemo(
    () => (activeConnection?.db_type === 'redis' ? REDIS_EDITOR_EXTENSIONS : SQL_EDITOR_EXTENSIONS),
    [activeConnection?.db_type],
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
      updateActiveTab({
        database: node.database || activeDatabase,
        schema: node.schema || '',
        selectedTable: node.item ?? null,
        objectMetaTab: 'columns',
        resultView: 'object-meta',
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
    })
    updateActiveTab({ metadataError: '' })
    updateActiveTab({
      explorerNodes: [createConnectionNode(connection, connection.id)],
      searchTreeNodes: [],
    })
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

    updateActiveTabExplorerNodes((current) =>
      updateAssetTreeNode(current, node.id, (target) => ({ ...target, expanded: !target.expanded })),
    )
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
      setEditorHeight(`${nextHeight}px`)
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
      <PageIntro
        title="SQL Editor"
        description="Run read-only queries, browse metadata, and keep query history and saved queries in one workspace. Create export requests directly from the result panel."
      />

      {connectionsError ? <InlineAlert>{connectionsError}</InlineAlert> : null}

      <section className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border border-border bg-panel shadow-soft">
        <div className="border-b border-border/80 px-4">
          <div className="flex flex-wrap items-center gap-5">
            {tabs.map((tab) => (
              <button
                key={tab.id}
                type="button"
                onClick={() => setActiveTabId(tab.id)}
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
              className="inline-flex items-center gap-2 border-b-2 border-transparent px-0.5 py-3 text-[13px] font-medium text-muted transition-colors hover:text-ink"
            >
              <Plus className="h-4 w-4" />
              New Tab
            </button>
          </div>
        </div>

        <div className="grid min-h-0 flex-1 gap-3 xl:grid-cols-[280px_minmax(0,1fr)]">
          <section className="flex min-h-0 flex-col border-r border-border/80 bg-panel">
            <div className="px-4 py-3">
              <div className="relative">
                <button
                  type="button"
                  aria-label="Asset Selector"
                  onClick={() => updateActiveTab({ assetPickerOpen: !activeAssetPickerOpen })}
                  className="flex w-full items-center gap-2 text-left text-[12px] text-ink transition"
                >
                <FolderTree className="h-4 w-4 shrink-0 text-muted" />
                <div className="min-w-0 flex-1">
                  {activeConnection ? (
                    <>
                      <p className="break-all text-[13px] font-semibold leading-5 text-ink">{activeConnection.name}</p>
                      <p className="mt-0.5 text-[10px] uppercase tracking-[0.12em] text-faint">
                        {formatConnectionBadge(activeConnection)}
                      </p>
                    </>
                  ) : (
                    <p className="text-[13px] font-semibold text-ink">Select assets</p>
                  )}
                </div>
                <ChevronDown className="h-4 w-4 shrink-0 text-faint" />
                </button>

              {activeAssetPickerOpen ? (
                <div className="absolute left-0 right-0 top-[calc(100%+8px)] z-20 rounded-lg border border-border bg-white p-2 shadow-soft">
                  <label className="flex h-9 items-center gap-2 rounded-md border border-border bg-panel-soft px-2.5">
                    <Search className="h-3.5 w-3.5 text-faint" />
                    <input
                      aria-label="Asset Picker Search"
                      value={activeAssetPickerSearch}
                      onChange={(event) => updateActiveTab({ assetPickerSearch: event.target.value })}
                      placeholder="Select assets"
                      className="w-full bg-transparent text-[12px] text-ink outline-none placeholder:text-muted"
                    />
                  </label>
                  <div className="mt-2 max-h-[220px] overflow-y-auto">
                    {filteredConnections.length === 0 ? (
                      <p className="px-2 py-2 text-[12px] text-muted">No matching assets.</p>
                    ) : (
                      filteredConnections.map((connection) => (
                        <button
                          key={connection.id}
                          type="button"
                          onClick={() => handleSelectConnection(connection)}
                          className={`flex w-full items-center gap-2 rounded-md px-2 py-2 text-left text-[12px] ${
                            activeConnection?.id === connection.id
                              ? 'bg-panel-soft text-ink ring-1 ring-border-strong'
                              : 'text-muted hover:bg-panel-soft hover:text-ink'
                          }`}
                        >
                          <span className="flex h-4 w-4 items-center justify-center text-muted">
                            {activeConnection?.id === connection.id ? (
                              <Check className="h-3.5 w-3.5 text-ink" />
                            ) : (
                              <Workflow className="h-3.5 w-3.5" />
                            )}
                          </span>
                          <div className="min-w-0 flex-1 pr-2">
                            <p className="break-all font-medium leading-5">{connection.name}</p>
                            <p className="break-all text-[10px] uppercase tracking-[0.12em] text-faint">
                              {formatConnectionBadge(connection)}
                            </p>
                          </div>
                          {activeConnection?.id === connection.id ? (
                            <span className="rounded-full bg-white px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-[0.12em] text-ink">
                              Selected
                            </span>
                          ) : null}
                        </button>
                      ))
                    )}
                  </div>
                </div>
              ) : null}
              </div>
            </div>

            <div className="flex min-h-0 flex-1 flex-col px-4 py-3">
              {activeTab?.metadataError ? <InlineAlert className="mb-2" tone="info">{activeTab.metadataError}</InlineAlert> : null}
              <label className="flex h-9 items-center gap-2 rounded-md border border-border bg-panel-soft px-2.5">
                <Search className="h-3.5 w-3.5 text-faint" />
                <input
                  aria-label="Explorer Search"
                  value={activeExplorerSearch}
                  onChange={(event) => updateActiveTab({ explorerSearch: event.target.value })}
                  placeholder="Search objects"
                  className="w-full bg-transparent text-[12px] text-ink outline-none placeholder:text-muted"
                />
              </label>
              <div className="mt-3 min-h-0 flex-1 overflow-y-auto pt-1">
                {connectionsLoading ? (
                  <p className="px-1 py-2 text-[12px] text-muted">Loading connections...</p>
                ) : activeSearchingAssets ? (
                  <p className="px-1 py-2 text-[12px] text-muted">Searching assets...</p>
                ) : !activeConnection || activeExplorerNodes.length === 0 ? (
                  <p className="px-1 py-2 text-[12px] text-muted">No DB connections available.</p>
                ) : filteredExplorerNodes.length === 0 ? (
                  <p className="px-1 py-2 text-[12px] text-muted">No matching assets.</p>
                ) : (
                  <AssetTree
                    nodes={filteredExplorerNodes}
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
                <div className="flex justify-end px-4 pt-3 pb-2">
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
                      disabled={runningTabId === activeTab.id || !activeTab.connectionId || !(activeSelectedSQL.trim() || activeTab.sql.trim())}
                      className="inline-flex h-10 items-center gap-2 rounded-lg border border-border bg-white px-4 text-[13px] font-semibold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {runningTabId === activeTab.id ? 'Running...' : 'Explain'}
                    </button>
                    <button
                      type="button"
                      onClick={handleRunQuery}
                      disabled={runningTabId === activeTab.id || !activeTab.connectionId || !(activeSelectedSQL.trim() || activeTab.sql.trim())}
                      className="inline-flex h-10 items-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      <Play className="h-4 w-4" />
                      {runningTabId === activeTab.id ? 'Running...' : 'Run Query'}
                    </button>
                  </div>
                </div>

              <div className="shrink-0 px-4 pt-2 pb-3">
                <div ref={editorContainerRef} className="overflow-hidden rounded-xl border border-border bg-panel-soft">
                  <CodeMirror
                    key={activeTab.id}
                    value={activeTab.sql}
                    height={editorHeight}
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
                      onClick={() => void handleSaveQuery()}
                      disabled={!activeTab.connectionId || !activeTab.sql.trim() || isFavorited}
                      className="inline-flex h-9 items-center gap-2 rounded-md border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {isFavorited ? <StarOff className="h-4 w-4" /> : <Star className="h-4 w-4" />}
                      {isFavorited ? 'Saved' : 'Save'}
                    </button>
                    <div className="inline-flex items-center overflow-hidden rounded-md border border-border bg-white">
                      <DropdownSelect
                        ariaLabel="Sensitive access duration"
                        value={String(activeSensitiveAccessDuration)}
                        onChange={(value) => updateActiveTab({ sensitiveAccessDuration: Number(value) })}
                        disabled={!canApplySensitiveAccess}
                        size="sm"
                        triggerClassName="h-9 rounded-none border-0 border-r border-border bg-transparent px-2 shadow-none hover:border-r hover:border-border"
                        menuClassName="left-0 w-28 rounded-2xl p-2"
                        options={[
                          { value: '10', label: '10m' },
                          { value: '30', label: '30m' },
                          { value: '60', label: '60m' },
                        ]}
                      />
                      <button
                        type="button"
                        onClick={() => void handleCreateSensitiveAccess()}
                        disabled={!canApplySensitiveAccess || !activeTab.connectionId || !activeExecutionSQL}
                        className="inline-flex h-9 items-center gap-2 px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        Sensitive Access
                      </button>
                    </div>
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
                      onClick={() => void handleExport()}
                      disabled={exportingTabId === activeTab.id || !activeTab.connectionId || !activeExecutionSQL}
                      className="inline-flex h-9 items-center gap-2 rounded-md border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      <Download className="h-4 w-4" />
                      {exportingTabId === activeTab.id ? 'Exporting...' : 'EXPORT'}
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
                    <span>{new Date(activeTab.lastRunAt).toLocaleString()}</span>
                  ) : null}
                  <span>{resultMetaLine}</span>
                </div>

                {activeTab.error ? <InlineAlert className="mt-3">{activeTab.error}</InlineAlert> : null}

                <div className="mt-3 min-h-0 flex-1 overflow-auto rounded-xl border border-border bg-white">
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
                            })}
                            className="block w-full px-4 py-3 text-left transition hover:bg-slate-50/70"
                          >
                            <p className="truncate text-[12px] font-semibold text-ink">{entry.sql_content}</p>
                            <p className="mt-1 text-[11px] text-muted">
                              {entry.db_connection_name} / {entry.duration_ms} ms / {new Date(entry.created_at).toLocaleString()}
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
                            <button
                              type="button"
                              onClick={() => setSavedQueryToDelete(entry)}
                              className="inline-flex h-8 w-8 items-center justify-center rounded-md border border-border bg-white text-muted transition hover:bg-page hover:text-danger"
                              aria-label={`Delete saved query ${entry.label}`}
                            >
                              <Trash2 className="h-4 w-4" />
                            </button>
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
                            <table className="min-w-full border-collapse">
                              <thead className="bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                                <tr>
                                  <th className="px-3 py-3">Column</th>
                                  <th className="px-3 py-3">Type</th>
                                  <th className="px-3 py-3">Nullable</th>
                                  <th className="px-3 py-3">Default</th>
                                </tr>
                              </thead>
                              <tbody>
                                {activeColumns.map((column) => (
                                  <tr key={column.name} className="border-t border-border text-[12px] text-ink hover:bg-slate-50/70">
                                    <td className="px-3 py-2.5 font-semibold">{column.name}</td>
                                    <td className="px-3 py-2.5">{column.column_type}</td>
                                    <td className="px-3 py-2.5">{column.is_nullable}</td>
                                    <td className="px-3 py-2.5">{column.default || <span className="text-muted">(none)</span>}</td>
                                  </tr>
                                ))}
                              </tbody>
                            </table>
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
                    <table className="min-w-full border-collapse">
                      <thead className="bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                        <tr>
                          {visibleResultColumnIndexes.map((columnIndex) => (
                            <th
                              key={`${activeTab.result?.columns[columnIndex]}-${columnIndex}`}
                              className={`px-3 py-3 ${sensitiveColumnIndexSet.has(columnIndex) ? 'text-[#b9381f]' : ''}`}
                            >
                              {activeTab.result?.columns[columnIndex]}
                            </th>
                          ))}
                        </tr>
                      </thead>
                      <tbody>
                        {pagedResultRows.map((row, rowOffset) => (
                          <tr key={`${activeTab.id}-row-${activeResultPage}-${rowOffset}`} className="border-t border-border text-[12px] text-ink hover:bg-slate-50/70">
                            {visibleResultColumnIndexes.map((columnIndex) => (
                              <td key={`${activeTab.id}-cell-${rowOffset}-${columnIndex}`} className="px-3 py-2.5 align-top">
                                {!Array.isArray(row)
                                  ? <span className="text-muted">(empty)</span>
                                  : row[columnIndex] === null
                                    ? <span className="text-muted">(null)</span>
                                    : String(row[columnIndex])}
                              </td>
                            ))}
                          </tr>
                        ))}
                      </tbody>
                    </table>
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
        open={savedQueryToDelete !== null}
        title="Delete Saved Query"
        description={savedQueryToDelete ? `Delete "${savedQueryToDelete.label}"? If you were at the 10-query limit, deleting it will free up a slot.` : ''}
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
