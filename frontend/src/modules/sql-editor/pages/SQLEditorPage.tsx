import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import CodeMirror from '@uiw/react-codemirror'
import { javascript } from '@codemirror/lang-javascript'
import { sql } from '@codemirror/lang-sql'
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
import { ApiError } from '@/shared/api/client'
import { useAuth } from '@/shared/auth/AuthContext'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { MetadataColumn, MetadataItem, QueryHistoryEntry, QueryResult, SavedQuery } from '@/shared/types/sqlEditor'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { useToast } from '@/shared/ui/ToastContext'
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog'
import { listDBConnections } from '@/modules/db-connections/api'
import { createExportRequest } from '@/modules/exports/api'
import {
  createSavedQuery,
  createSensitiveAccessTicket,
  deleteSavedQuery,
  executeQuery,
  listMetadata,
  listMetadataColumns,
  listQueryHistory,
  listSavedQueries,
} from '@/modules/sql-editor/api'

type EditorTab = {
  id: string
  title: string
  connectionId: number | null
  sql: string
  result: QueryResult | null
  error: string
  lastRunAt: string | null
}

type PersistedState = {
  activeTabId: string
  tabs: EditorTab[]
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

const STORAGE_PREFIX = 'dbre_maestro.sql_editor'
const DEFAULT_SQL = 'SELECT 1;'
const HISTORY_LIMIT = 20
const SAVED_QUERY_LIMIT = 10
const EDITOR_BASE_VISIBLE_LINES = 12
const EDITOR_MAX_HEIGHT = 840
const EDITOR_LINE_HEIGHT = 24
const EDITOR_VERTICAL_PADDING = 24
const EDITOR_MIN_HEIGHT = EDITOR_VERTICAL_PADDING + EDITOR_BASE_VISIBLE_LINES * EDITOR_LINE_HEIGHT
const RESULT_PAGE_SIZE = 50

function parsePixelValue(value: string): number {
  const parsed = Number.parseFloat(value)
  return Number.isFinite(parsed) ? parsed : 0
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
    return selectedTable ? `${selectedTable.schema}.${selectedTable.name}` : detailHint || '選擇資料表後查看結構'
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
    sql: DEFAULT_SQL,
    result: null,
    error: '',
    lastRunAt: null,
  }
}

function safeParseState(raw: string | null): PersistedState | null {
  if (!raw) {
    return null
  }

  try {
    const parsed = JSON.parse(raw) as Partial<PersistedState>
    if (!Array.isArray(parsed.tabs) || parsed.tabs.length === 0 || typeof parsed.activeTabId !== 'string') {
      return null
    }

    return {
      activeTabId: parsed.activeTabId,
      tabs: parsed.tabs.map((tab, index) => ({
        id: typeof tab.id === 'string' ? tab.id : `tab-restored-${index}`,
        title: typeof tab.title === 'string' ? tab.title : `Query ${index + 1}`,
        connectionId: typeof tab.connectionId === 'number' ? tab.connectionId : null,
        sql: typeof tab.sql === 'string' && tab.sql.trim() ? tab.sql : DEFAULT_SQL,
        result: tab.result ?? null,
        error: typeof tab.error === 'string' ? tab.error : '',
        lastRunAt: typeof tab.lastRunAt === 'string' ? tab.lastRunAt : null,
      })),
    }
  } catch {
    return null
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
    const selfMatched =
      node.label.toLowerCase().includes(keyword) ||
      (node.meta ? node.meta.toLowerCase().includes(keyword) : false) ||
      (node.database ? node.database.toLowerCase().includes(keyword) : false) ||
      (node.schema ? node.schema.toLowerCase().includes(keyword) : false)

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
            {node.kind === 'connection' && node.active ? (
              <span className="rounded-full bg-white px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-[0.12em] text-muted">
                Selected
              </span>
            ) : null}
            {node.meta ? <span className="ml-auto text-[10px] font-semibold uppercase tracking-[0.12em] text-faint">{node.meta}</span> : null}
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
  const { user } = useAuth()
  const { pushToast } = useToast()
  const hasSensitiveOverride = Boolean(user?.permissions.includes('global.sensitive'))
  const canApplySensitiveAccess = Boolean(user?.permissions.includes('sql_editor.sensitive_apply'))
  const accessibleConnectionIDs = user?.dbConnectionIds ?? []
  const storageKey = user ? `${STORAGE_PREFIX}.${user.id}` : `${STORAGE_PREFIX}.anonymous`
  const [connections, setConnections] = useState<DBConnection[]>([])
  const [connectionsLoading, setConnectionsLoading] = useState(true)
  const [connectionsError, setConnectionsError] = useState('')
  const [tabs, setTabs] = useState<EditorTab[]>([createTab()])
  const [activeTabId, setActiveTabId] = useState<string>(() => createTab().id)
  const [history, setHistory] = useState<QueryHistoryEntry[]>([])
  const [savedQueries, setSavedQueries] = useState<SavedQuery[]>([])
  const [runningTabId, setRunningTabId] = useState<string | null>(null)
  const [exportingTabId, setExportingTabId] = useState<string | null>(null)
  const [explorerNodes, setExplorerNodes] = useState<AssetTreeNode[]>([])
  const [selectedDatabase, setSelectedDatabase] = useState('')
  const [selectedSchema, setSelectedSchema] = useState('')
  const [selectedTable, setSelectedTable] = useState<MetadataItem | null>(null)
  const [columns, setColumns] = useState<MetadataColumn[]>([])
  const [columnsLoading, setColumnsLoading] = useState(false)
  const [metadataError, setMetadataError] = useState('')
  const [explorerSearch, setExplorerSearch] = useState('')
  const [assetPickerOpen, setAssetPickerOpen] = useState(false)
  const [assetPickerSearch, setAssetPickerSearch] = useState('')
  const [resultView, setResultView] = useState<'result' | 'vertical' | 'object-meta' | 'history' | 'saved'>('result')
  const [columnFilterOpen, setColumnFilterOpen] = useState(false)
  const [visibleColumnIndexes, setVisibleColumnIndexes] = useState<number[] | null>(null)
  const [savedQueryToDelete, setSavedQueryToDelete] = useState<SavedQuery | null>(null)
  const [selectedSQL, setSelectedSQL] = useState('')
  const [sensitiveAccessDuration, setSensitiveAccessDuration] = useState(10)
  const [editorHeight, setEditorHeight] = useState(`${EDITOR_MIN_HEIGHT}px`)
  const [resultPage, setResultPage] = useState(1)

  useEffect(() => {
    const restored = safeParseState(window.localStorage.getItem(storageKey))
    if (!restored) {
      const firstTab = createTab()
      setTabs([firstTab])
      setActiveTabId(firstTab.id)
      return
    }

    setTabs(restored.tabs)
    setActiveTabId(restored.activeTabId)
  }, [storageKey])

  useEffect(() => {
    if (!tabs.length) {
      return
    }

    const state: PersistedState = {
      activeTabId,
      tabs,
    }
    window.localStorage.setItem(storageKey, JSON.stringify(state))
  }, [activeTabId, storageKey, tabs])

  useEffect(() => {
    let active = true

    async function loadConnections() {
      setConnectionsLoading(true)
      setConnectionsError('')
      try {
        const response = await listDBConnections()
        if (!active) {
          return
        }

        setConnections(response.connections)
      } catch (error) {
        if (active) {
          setConnectionsError(error instanceof ApiError ? error.message : '讀取資料庫連線失敗。')
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
          pushToast(error instanceof ApiError ? error.message : '讀取查詢歷史失敗。', 'error')
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
          pushToast(error instanceof ApiError ? error.message : '讀取常用 SQL 失敗。', 'error')
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
  const activePathLabel = useMemo(() => {
    const parts = [activeConnection?.name].filter(Boolean) as string[]
    if (selectedDatabase) {
      parts.push(selectedDatabase)
    }
    if (selectedSchema && activeConnection?.db_type === 'postgres') {
      parts.push(selectedSchema)
    }
    if (selectedTable) {
      parts.push(selectedTable.name)
    }
    return parts
  }, [activeConnection?.db_type, activeConnection?.name, selectedDatabase, selectedSchema, selectedTable])
  const filteredExplorerNodes = useMemo(
    () => filterAssetTree(explorerNodes, explorerSearch),
    [explorerNodes, explorerSearch],
  )
  const filteredConnections = useMemo(() => {
    const keyword = assetPickerSearch.trim().toLowerCase()
    if (!keyword) {
      return accessibleConnections
    }
    return accessibleConnections.filter((connection) =>
      connection.name.toLowerCase().includes(keyword) ||
      connection.db_type.toLowerCase().includes(keyword) ||
      (connection.database_name ?? '').toLowerCase().includes(keyword),
    )
  }, [accessibleConnections, assetPickerSearch])

  useEffect(() => {
    if (accessibleConnections.length === 0) {
      setTabs((currentTabs) => currentTabs.map((tab) => (tab.connectionId === null ? tab : { ...tab, connectionId: null })))
      return
    }

    const accessibleIDSet = new Set(accessibleConnections.map((connection) => connection.id))
    setTabs((currentTabs) => currentTabs.map((tab) => (
      tab.connectionId !== null && !accessibleIDSet.has(tab.connectionId)
        ? { ...tab, connectionId: null }
        : tab
    )))
  }, [accessibleConnections])

  useEffect(() => {
    if (!activeConnection) {
      setExplorerNodes([])
      setSelectedDatabase('')
      setSelectedSchema('')
      setSelectedTable(null)
      setColumns([])
      return
    }

    setExplorerNodes((current) => {
      const existing = current[0]
      if (existing && existing.connectionId === activeConnection.id) {
        return current
      }
      return [createConnectionNode(activeConnection, activeConnection.id)]
    })
  }, [activeConnection])

  useEffect(() => {
    setExplorerNodes((current) =>
      syncAssetTreeActiveStates(current, activeTab?.connectionId ?? null, selectedDatabase, selectedSchema, selectedTable),
    )
  }, [activeTab?.connectionId, selectedDatabase, selectedSchema, selectedTable])

  useEffect(() => {
    if (!activeTab?.connectionId || !selectedTable || activeConnection?.db_type === 'redis') {
      setColumns([])
      return
    }

    let active = true
    const connectionId = activeTab.connectionId
    const schema = selectedTable.schema
    const table = selectedTable.name

    if (!schema) {
      setColumns([])
      return
    }
    const schemaName = schema

    async function loadColumns() {
      setColumnsLoading(true)
      try {
        const response = await listMetadataColumns(connectionId, schemaName, table, selectedDatabase || undefined)
        if (active) {
          setColumns(response.columns)
        }
      } catch (error) {
        if (active) {
          setMetadataError(error instanceof ApiError ? error.message : '讀取欄位失敗。')
          setColumns([])
        }
      } finally {
        if (active) {
          setColumnsLoading(false)
        }
      }
    }

    void loadColumns()

    return () => {
      active = false
    }
  }, [activeConnection?.db_type, activeTab?.connectionId, selectedDatabase, selectedTable])

  function updateActiveTab(patch: Partial<EditorTab>) {
    if (!activeTab) {
      return
    }

    setTabs((currentTabs) => currentTabs.map((tab) => (tab.id === activeTab.id ? { ...tab, ...patch } : tab)))
  }

  async function loadNodeChildren(node: AssetTreeNode) {
    const connection = connections.find((item) => item.id === node.connectionId)
    if (!connection) {
      return
    }

    setExplorerNodes((current) =>
      updateAssetTreeNode(current, node.id, (target) => ({ ...target, loading: true, expanded: true })),
    )
    setMetadataError('')

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

      setExplorerNodes((current) =>
        syncAssetTreeActiveStates(
          updateAssetTreeNode(current, node.id, (target) => ({
            ...target,
            children,
            expanded: true,
            loaded: true,
            loading: false,
          })),
          activeTab?.connectionId ?? null,
          selectedDatabase,
          selectedSchema,
          selectedTable,
        ),
      )
    } catch (error) {
      setMetadataError(error instanceof ApiError ? error.message : '讀取 metadata 失敗。')
      setExplorerNodes((current) =>
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

  async function handleRunQuery() {
    const sqlToExecute = selectedSQL.trim() || activeTab?.sql.trim() || ''
    if (!activeTab?.connectionId || !sqlToExecute) {
      updateActiveTab({ error: '請先選擇資料庫連線並輸入查詢內容。' })
      return
    }

    setRunningTabId(activeTab.id)
    updateActiveTab({ error: '' })

    try {
      const result = await executeQuery({
        db_connection_id: activeTab.connectionId,
        sql: sqlToExecute,
        database: selectedDatabase || undefined,
        schema: activeConnection?.db_type === 'postgres' ? selectedSchema || undefined : undefined,
        redis_db_index: activeConnection?.db_type === 'redis' && selectedDatabase ? Number(selectedDatabase) : undefined,
      })

      updateActiveTab({
        result,
        error: '',
        lastRunAt: new Date().toISOString(),
      })
      setResultView('result')
      void listQueryHistory(HISTORY_LIMIT).then((response) => setHistory(response.history)).catch(() => undefined)
      pushToast('查詢已完成', 'success')
    } catch (error) {
      const message = error instanceof ApiError ? error.message : '查詢執行失敗。'
      updateActiveTab({
        error: message,
        result: null,
      })
    } finally {
      setRunningTabId(null)
    }
  }

  async function handleExport() {
    if (!activeTab?.connectionId || !activeTab.sql.trim()) {
      return
    }

    setExportingTabId(activeTab.id)
    try {
      const response = await createExportRequest({
        db_connection_id: activeTab.connectionId,
        sql_content: activeTab.sql,
        database_name: selectedDatabase || undefined,
        schema_name: activeConnection?.db_type === 'postgres' ? selectedSchema || undefined : undefined,
      })
      pushToast(`已建立匯出工單 ${response.ticket_no}`, 'success', { placement: 'center' })
    } catch (error) {
      pushToast(error instanceof ApiError ? error.message : '建立匯出請求失敗。', 'error')
    } finally {
      setExportingTabId(null)
    }
  }

  async function handleCreateSensitiveAccess() {
    if (!activeTab?.connectionId || !activeTab.sql.trim()) {
      return
    }
    if (activeConnection?.db_type !== 'mysql') {
      pushToast('Sensitive Access 目前只支援 MySQL。', 'info', { placement: 'center' })
      return
    }

    try {
      const response = await createSensitiveAccessTicket({
        db_connection_id: activeTab.connectionId,
        sql_content: activeTab.sql,
        database_name: selectedDatabase || undefined,
        schema_name: selectedSchema || undefined,
        approved_duration_minutes: sensitiveAccessDuration,
      })
      pushToast(`已建立 Sensitive Access 工單 ${response.ticket_no}`, 'success', { placement: 'center' })
    } catch (error) {
      pushToast(error instanceof ApiError ? error.message : '建立 Sensitive Access 工單失敗。', 'error')
    }
  }

  async function handleSaveQuery() {
    if (!activeTab?.connectionId || !activeTab.sql.trim()) {
      return
    }

    const existing = savedQueries.find((item) =>
      item.db_connection_id === activeTab.connectionId &&
      item.sql_content === activeTab.sql &&
      (item.database_name ?? '') === selectedDatabase &&
      (item.schema_name ?? '') === selectedSchema &&
      (item.redis_db_index ?? null) === (activeConnection?.db_type === 'redis' && selectedDatabase ? Number(selectedDatabase) : null),
    )
    if (existing) {
      pushToast('這條 SQL 已經在常用清單中。', 'info')
      return
    }

    if (savedQueries.length >= SAVED_QUERY_LIMIT) {
      pushToast('常用 SQL 最多只能儲存 10 組。', 'error')
      return
    }

    try {
      const created = await createSavedQuery({
        label: activeTab.title,
        db_connection_id: activeTab.connectionId,
        database: selectedDatabase || undefined,
        schema: selectedSchema || undefined,
        redis_db_index: activeConnection?.db_type === 'redis' && selectedDatabase ? Number(selectedDatabase) : undefined,
        sql_content: activeTab.sql,
      })
      setSavedQueries((current) => [created, ...current].slice(0, SAVED_QUERY_LIMIT))
    } catch (error) {
      pushToast(error instanceof ApiError ? error.message : '儲存常用 SQL 失敗。', 'error')
      return
    }
    pushToast('已加入收藏', 'success')
  }

  function applySavedQuery(entry: { connectionId: number; sql: string; label: string; database?: string | null; schema?: string | null; redisDbIndex?: number | null }) {
    if (!activeTab) {
      return
    }

    setActiveTabId(activeTab.id)
    updateActiveTab({
      connectionId: entry.connectionId,
      sql: entry.sql,
      title: entry.label,
      result: null,
      error: '',
    })
    setSelectedDatabase(entry.database ?? '')
    setSelectedSchema(entry.schema ?? '')
    setSelectedTable(null)
    setColumns([])
    if (entry.redisDbIndex !== undefined && entry.redisDbIndex !== null) {
      setSelectedDatabase(String(entry.redisDbIndex))
    }
    setResultView('result')
  }

  const isFavorited = !!(activeTab && savedQueries.some((item) =>
    item.db_connection_id === activeTab.connectionId &&
    item.sql_content === activeTab.sql &&
    (item.database_name ?? '') === selectedDatabase &&
    (item.schema_name ?? '') === selectedSchema &&
    (item.redis_db_index ?? null) === (activeConnection?.db_type === 'redis' && selectedDatabase ? Number(selectedDatabase) : null),
  ))
  const editorExtensions = useMemo(
    () => [activeTab && accessibleConnections.find((connection) => connection.id === activeTab.connectionId)?.db_type === 'redis' ? javascript() : sql()],
    [activeTab, accessibleConnections],
  )
  const visibleResultColumnIndexes = useMemo(() => {
    if (!activeTab?.result) {
      return []
    }
    if (!visibleColumnIndexes || visibleColumnIndexes.length === 0) {
      return activeTab.result.columns.map((_, index) => index)
    }
    return visibleColumnIndexes.filter((index) => index >= 0 && index < activeTab.result!.columns.length)
  }, [activeTab?.result, visibleColumnIndexes])
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
    const start = (resultPage - 1) * RESULT_PAGE_SIZE
    return activeTab.result.rows.slice(start, start + RESULT_PAGE_SIZE)
  }, [activeTab?.result, resultPage])
  const detailHint = metadataHint(activeConnection?.db_type)
  const resultMetaLine = useMemo(() => formatResultMetaLine({
    resultView,
    result: activeTab?.result ?? null,
    selectedTable,
    detailHint,
    historyCount: history.length,
    savedCount: savedQueries.length,
    currentPage: resultPage,
    totalPages: totalResultPages,
  }), [activeTab?.result, detailHint, history.length, resultPage, resultView, savedQueries.length, selectedTable, totalResultPages])

  function handleSelectNode(node: AssetTreeNode) {
    if (node.kind === 'connection') {
      if (activeTab?.connectionId !== node.connectionId) {
        updateActiveTab({ connectionId: node.connectionId, result: null, error: '' })
        setSelectedDatabase('')
        setSelectedSchema('')
        setSelectedTable(null)
        setColumns([])
      }
      return
    }

    if (node.kind === 'database') {
      setSelectedDatabase(node.database || node.label)
      setSelectedSchema('')
      setSelectedTable(null)
      setColumns([])
      return
    }

    if (node.kind === 'schema') {
      setSelectedDatabase(node.database || selectedDatabase)
      setSelectedSchema(node.schema || node.label)
      setSelectedTable(null)
      setColumns([])
      return
    }

    if (node.kind === 'table') {
      setSelectedDatabase(node.database || selectedDatabase)
      setSelectedSchema(node.schema || '')
      setSelectedTable(node.item ?? null)
      setResultView('object-meta')
      return
    }

    setSelectedDatabase(node.database || node.label.replace('DB ', ''))
    setSelectedSchema('')
    setSelectedTable(null)
    setColumns([])
  }

  function handleSelectConnection(connection: DBConnection) {
    updateActiveTab({ connectionId: connection.id, result: null, error: '' })
    setSelectedDatabase('')
    setSelectedSchema('')
    setSelectedTable(null)
    setColumns([])
    setMetadataError('')
    setExplorerSearch('')
    setAssetPickerOpen(false)
    setAssetPickerSearch('')
    setExplorerNodes([createConnectionNode(connection, connection.id)])
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

    setExplorerNodes((current) =>
      updateAssetTreeNode(current, node.id, (target) => ({ ...target, expanded: !target.expanded })),
    )
  }

  function metadataHint(dbType: string | undefined) {
    if (dbType === 'redis') {
      return 'Redis 目前先提供 DB 0-15 選擇，key 瀏覽後續再補。'
    }
    if (dbType === 'postgres' && !selectedSchema) {
      return '先選擇 schema。'
    }
    return ''
  }

  useEffect(() => {
    if (!activeTab?.result) {
      setVisibleColumnIndexes(null)
      return
    }
    setVisibleColumnIndexes(activeTab.result.columns.map((_, index) => index))
  }, [activeTab?.id, activeTab?.result])

  useEffect(() => {
    setSelectedSQL('')
  }, [activeTabId])

  useEffect(() => {
    if (resultView !== 'result' && resultView !== 'vertical') {
      setResultPage(1)
      return
    }
    setResultPage((current) => Math.min(current, totalResultPages))
  }, [activeTab?.id, activeTab?.result, resultView, totalResultPages, visibleColumnIndexes])

  useLayoutEffect(() => {
    const updateHeight = () => {
      const nextHeight = measureEditorHeight(editorContainerRef.current, activeTab?.sql ?? DEFAULT_SQL)
      setEditorHeight(`${nextHeight}px`)
    }

    updateHeight()
    const frameID = window.requestAnimationFrame(updateHeight)

    return () => {
      window.cancelAnimationFrame(frameID)
    }
  }, [activeTab?.id, activeTab?.sql])

  async function handleDeleteSavedQuery(entry: SavedQuery) {
    try {
      await deleteSavedQuery(entry.id)
      setSavedQueries((current) => current.filter((item) => item.id !== entry.id))
      pushToast('已刪除常用 SQL', 'success')
    } catch (error) {
      pushToast(error instanceof ApiError ? error.message : '刪除常用 SQL 失敗。', 'error')
    } finally {
      setSavedQueryToDelete(null)
    }
  }

  function toggleVisibleColumn(index: number) {
    setVisibleColumnIndexes((current) => {
      const base = current ?? activeTab?.result?.columns.map((_, columnIndex) => columnIndex) ?? []
      if (base.includes(index)) {
        return base.filter((item) => item !== index)
      }
      return [...base, index].sort((left, right) => left - right)
    })
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-3 p-3 sm:p-4">
      <section className="rounded-xl border border-border bg-panel-soft shadow-soft">
        <div className="border-b border-border/80 px-4 py-3 sm:px-5">
          <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
            <div className="max-w-3xl">
              <div className="flex flex-wrap items-center gap-2 text-[11px] font-semibold text-muted">
                <span className="rounded-full border border-border bg-white px-2.5 py-1 text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                  SQL Editor
                </span>
                <span>/</span>
                <span>Query Console</span>
              </div>
              <h2 className="mt-3 text-[24px] font-bold tracking-[-0.03em] text-ink">SQL / Redis 工作台</h2>
              <p className="mt-2 text-[13px] leading-6 text-muted">
                在同一個工作區執行 read-only 查詢、瀏覽 metadata、保留歷史與收藏，並從結果區直接建立匯出請求。
              </p>
            </div>

            <div className="grid min-w-[250px] gap-2 text-[12px] text-muted sm:grid-cols-3 lg:min-w-[360px]">
              <div className="rounded-lg border border-border bg-white px-3 py-2.5 shadow-soft">
                <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">Tabs</p>
                <p className="mt-1 text-[20px] font-bold tracking-tight text-ink">{tabs.length}</p>
              </div>
              <div className="rounded-lg border border-border bg-white px-3 py-2.5 shadow-soft">
                <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">History</p>
                <p className="mt-1 text-[20px] font-bold tracking-tight text-ink">{history.length}</p>
              </div>
              <div className="rounded-lg border border-border bg-white px-3 py-2.5 shadow-soft">
                <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">Saved Queries</p>
                <p className="mt-1 text-[20px] font-bold tracking-tight text-ink">{savedQueries.length}</p>
              </div>
            </div>
          </div>
        </div>
      </section>

      {connectionsError ? <InlineAlert>{connectionsError}</InlineAlert> : null}

      <div className="grid min-h-0 flex-1 gap-3 xl:grid-cols-[280px_minmax(0,1fr)]">
        <section className="flex min-h-0 flex-col rounded-xl border border-border bg-panel shadow-soft">
          <div className="border-b border-border/80 px-4 py-3">
            <div className="flex items-center gap-2">
              <FolderTree className="h-4 w-4 text-accent" />
              <p className="text-[13px] font-semibold text-ink">Workspace Assets</p>
            </div>
          </div>

          <div className="flex min-h-0 flex-1 flex-col gap-3 p-4">
            <div className="flex min-h-0 flex-1 flex-col rounded-lg border border-border bg-panel-soft px-3 py-3">
              <div className="flex items-center gap-2">
                <Table2 className="h-4 w-4 text-muted" />
                <p className="text-[12px] font-semibold text-ink">Workspace Explorer</p>
              </div>
              {metadataError ? <InlineAlert className="mt-2" tone="info">{metadataError}</InlineAlert> : null}
              <div className="mt-2 min-h-0 flex-1">
                <div className="flex h-full min-h-0 flex-col overflow-hidden rounded-lg bg-white px-3 py-3">
                  <div className="px-1">
                    <p className="text-[10px] font-semibold uppercase tracking-[0.14em] text-faint">Objects</p>
                    <label className="mt-2 flex h-9 items-center gap-2 rounded-md border border-border bg-panel-soft px-2.5">
                      <Search className="h-3.5 w-3.5 text-faint" />
                      <input
                        aria-label="Explorer Search"
                        value={explorerSearch}
                        onChange={(event) => setExplorerSearch(event.target.value)}
                        placeholder="Search assets"
                        className="w-full bg-transparent text-[12px] text-ink outline-none placeholder:text-muted"
                      />
                    </label>
                  </div>
                  <div className="mt-3 min-h-0 flex-1 overflow-y-auto border-t border-border/80 pt-3">
                    {connectionsLoading ? (
                      <p className="px-1 py-2 text-[12px] text-muted">載入連線中…</p>
                    ) : !activeConnection || explorerNodes.length === 0 ? (
                      <p className="px-1 py-2 text-[12px] text-muted">目前沒有可用的 DB connection。</p>
                    ) : filteredExplorerNodes.length === 0 ? (
                      <p className="px-1 py-2 text-[12px] text-muted">沒有符合搜尋條件的資產。</p>
                    ) : (
                      <AssetTree
                        nodes={filteredExplorerNodes}
                        onSelect={handleSelectNode}
                        onToggle={(node) => void handleToggleNode(node)}
                      />
                    )}
                  </div>
                </div>
              </div>
            </div>
          </div>
        </section>

        <section className="flex min-h-0 flex-col rounded-xl border border-border bg-panel shadow-soft">
          <div className="border-b border-border/80 px-4 py-3">
            <div className="flex flex-wrap items-center gap-2">
              {tabs.map((tab) => (
                <button
                  key={tab.id}
                  type="button"
                  onClick={() => setActiveTabId(tab.id)}
                  className={`inline-flex items-center gap-2 rounded-lg border px-3 py-2 text-[12px] font-semibold transition ${
                    tab.id === activeTabId
                      ? 'border-border bg-panel-soft text-ink'
                      : 'border-transparent bg-white text-muted hover:border-border hover:bg-page hover:text-ink'
                  }`}
                >
                  <span>{tab.title}</span>
                  <span
                    role="button"
                    aria-label={`Close ${tab.title}`}
                    onClick={(event) => {
                      event.stopPropagation()
                      handleCloseTab(tab.id)
                    }}
                    className="inline-flex h-4 w-4 items-center justify-center rounded-full text-muted hover:bg-white hover:text-ink"
                  >
                    <X className="h-3.5 w-3.5" />
                  </span>
                </button>
              ))}
              <button
                type="button"
                onClick={handleAddTab}
                className="inline-flex items-center gap-2 rounded-lg border border-border bg-white px-3 py-2 text-[12px] font-semibold text-ink transition hover:bg-page"
              >
                <Plus className="h-4 w-4" />
                New Tab
              </button>
            </div>
          </div>

          {!activeTab ? (
            <LoadingBlock message="載入 editor 中…" className="m-4 min-h-[320px] rounded-xl border-border bg-panel" />
          ) : (
            <div className="flex min-h-0 flex-1 flex-col">
              <div className="flex flex-wrap items-center gap-2 border-b border-border/80 px-4 py-3">
                <div className="relative min-w-[320px] flex-1 basis-[360px]">
                  <button
                    type="button"
                    onClick={() => setAssetPickerOpen((current) => !current)}
                    className="flex min-h-[52px] w-full items-center gap-2 rounded-lg border border-border bg-white px-3 py-2 text-left text-[12px] text-ink"
                  >
                    <Workflow className="h-4 w-4 shrink-0 text-faint" />
                    <div className="min-w-0 flex-1">
                      <p className="text-[10px] font-semibold uppercase tracking-[0.14em] text-faint">Selected Asset</p>
                      {activeConnection ? (
                        <>
                          <p className="mt-1 break-all text-[13px] font-medium leading-5 text-ink">{activeConnection.name}</p>
                          <p className="mt-0.5 text-[10px] uppercase tracking-[0.12em] text-faint">
                            {activeConnection.db_type}
                            {activeConnection.host ? ` / ${activeConnection.host}` : ''}
                          </p>
                        </>
                      ) : (
                        <p className="mt-1 text-[13px] font-medium text-ink">Select asset</p>
                      )}
                    </div>
                    <ChevronDown className="h-4 w-4 shrink-0 text-faint" />
                  </button>
                  {assetPickerOpen ? (
                    <div className="absolute left-0 right-0 top-[calc(100%+8px)] z-10 rounded-lg border border-border bg-white p-2 shadow-soft">
                      <label className="flex h-9 items-center gap-2 rounded-md border border-border bg-panel-soft px-2.5">
                        <Search className="h-3.5 w-3.5 text-faint" />
                        <input
                          aria-label="Asset Picker Search"
                          value={assetPickerSearch}
                          onChange={(event) => setAssetPickerSearch(event.target.value)}
                          placeholder="Search assets"
                          className="w-full bg-transparent text-[12px] text-ink outline-none placeholder:text-muted"
                        />
                      </label>
                      <div className="mt-2 max-h-[220px] overflow-y-auto">
                        {filteredConnections.length === 0 ? (
                          <p className="px-2 py-2 text-[12px] text-muted">沒有符合的 asset。</p>
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
                                  {connection.db_type}
                                  {connection.host ? ` / ${connection.host}` : ''}
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
                <button
                  type="button"
                  onClick={handleRunQuery}
                  disabled={runningTabId === activeTab.id || !activeTab.connectionId}
                  className="inline-flex h-10 items-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  <Play className="h-4 w-4" />
                  {runningTabId === activeTab.id ? '執行中…' : 'Run Query'}
                </button>
                <div className="inline-flex h-10 min-w-[220px] items-center rounded-lg border border-border bg-white px-3 text-[12px] font-semibold text-muted">
                  <span className="truncate">{activePathLabel.length > 0 ? activePathLabel.join(' / ') : '從左側 Explorer 選擇資料源'}</span>
                </div>
              </div>

              <div className="shrink-0 p-4">
                <div ref={editorContainerRef} className="overflow-hidden rounded-xl border border-border bg-panel-soft">
                  <CodeMirror
                    value={activeTab.sql}
                    height={editorHeight}
                    extensions={editorExtensions}
                    onChange={(value) => updateActiveTab({ sql: value })}
                    onStatistics={(stats) => setSelectedSQL(stats.selectedText ? stats.selectionCode : '')}
                    theme="light"
                    basicSetup={{
                      lineNumbers: true,
                      foldGutter: false,
                      highlightActiveLine: false,
                    }}
                  />
                </div>
              </div>

              <div className="flex min-h-0 flex-1 flex-col border-t border-border/80 px-4 py-3">
                {hasSensitiveOverride || activeTab.result?.sensitive_override_active ? (
                  <div className="mb-3 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-[12px] text-amber-900">
                    <span className="font-semibold">Sensitive override active.</span> 此帳號的查詢與匯出結果會直接顯示未脫敏資料。
                  </div>
                ) : null}
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div className="flex flex-wrap items-center gap-3">
                    <div className="inline-flex items-center rounded-lg border border-border bg-white p-1">
                      <button
                        type="button"
                        onClick={() => setResultView('result')}
                        className={`inline-flex items-center gap-2 rounded-md px-3 py-1.5 ${
                          resultView === 'result' ? 'bg-panel-soft text-ink' : 'text-muted hover:text-ink'
                        }`}
                      >
                        <FileClock className="h-4 w-4" />
                        Result
                      </button>
                      <button
                        type="button"
                        onClick={() => setResultView('vertical')}
                        className={`inline-flex items-center gap-2 rounded-md px-3 py-1.5 ${
                          resultView === 'vertical' ? 'bg-panel-soft text-ink' : 'text-muted hover:text-ink'
                        }`}
                      >
                        <Layers3 className="h-4 w-4" />
                        Vertical
                      </button>
                      <button
                        type="button"
                        onClick={() => setResultView('object-meta')}
                        className={`inline-flex items-center gap-2 rounded-md px-3 py-1.5 ${
                          resultView === 'object-meta' ? 'bg-panel-soft text-ink' : 'text-muted hover:text-ink'
                        }`}
                      >
                        <Database className="h-4 w-4" />
                        Object Meta
                      </button>
                      <button
                        type="button"
                        onClick={() => setResultView('history')}
                        className={`inline-flex items-center gap-2 rounded-md px-3 py-1.5 ${
                          resultView === 'history' ? 'bg-panel-soft text-ink' : 'text-muted hover:text-ink'
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
                      onClick={() => setResultView('saved')}
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
                      <select
                        value={sensitiveAccessDuration}
                        onChange={(event) => setSensitiveAccessDuration(Number(event.target.value))}
                        disabled={!canApplySensitiveAccess}
                        className="h-9 border-r border-border bg-transparent px-2 text-[12px] font-semibold text-ink outline-none disabled:cursor-not-allowed"
                      >
                        <option value={10}>10m</option>
                        <option value={30}>30m</option>
                        <option value={60}>60m</option>
                      </select>
                      <button
                        type="button"
                        onClick={() => void handleCreateSensitiveAccess()}
                        disabled={!canApplySensitiveAccess || !activeTab.connectionId || !activeTab.sql.trim()}
                        className="inline-flex h-9 items-center gap-2 px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        Sensitive Access
                      </button>
                    </div>
                    <div className="relative">
                      <button
                        type="button"
                        onClick={() => setColumnFilterOpen((current) => !current)}
                        disabled={!activeTab.result}
                        className="inline-flex h-9 items-center gap-2 rounded-md border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        <Filter className="h-4 w-4" />
                        Filter Columns
                      </button>
                      {columnFilterOpen && activeTab.result ? (
                        <div className="absolute right-0 top-[calc(100%+8px)] z-10 w-64 rounded-lg border border-border bg-white p-3 shadow-soft">
                          <div className="mb-2 flex items-center justify-between">
                            <p className="text-[11px] font-semibold uppercase tracking-[0.12em] text-faint">Visible Columns</p>
                            <button
                              type="button"
                              onClick={() => setVisibleColumnIndexes(activeTab.result?.columns.map((_, index) => index) ?? [])}
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
                      disabled={exportingTabId === activeTab.id || !activeTab.connectionId || !activeTab.sql.trim()}
                      className="inline-flex h-9 items-center gap-2 rounded-md border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      <Download className="h-4 w-4" />
                      {exportingTabId === activeTab.id ? '匯出中…' : 'EXPORT'}
                    </button>
                  </div>
                </div>

                <div className="mt-3 flex flex-wrap items-center gap-2 text-[12px] text-muted">
                  {(resultView === 'result' || resultView === 'vertical') && activeTab.result ? (
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
                  {resultView === 'history' ? (
                    history.length === 0 ? (
                      <div className="flex h-[180px] items-center justify-center text-[12px] text-muted">
                        尚無查詢歷史。
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
                  ) : resultView === 'saved' ? (
                    savedQueries.length === 0 ? (
                      <div className="flex h-[180px] items-center justify-center text-[12px] text-muted">
                        尚無常用 SQL。
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
                  ) : resultView === 'object-meta' ? (
                    !selectedTable ? (
                      <div className="flex h-[180px] items-center justify-center text-[12px] text-muted">
                        {detailHint || '從左側資產樹點擊資料表後查看表結構。'}
                      </div>
                    ) : columnsLoading ? (
                      <div className="flex h-[180px] items-center justify-center text-[12px] text-muted">
                        載入表結構中…
                      </div>
                    ) : columns.length === 0 ? (
                      <div className="flex h-[180px] items-center justify-center text-[12px] text-muted">
                        尚無表結構資料。
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
                          {columns.map((column) => (
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
                  ) : resultView === 'vertical' ? (
                    !activeTab.result ? (
                      <div className="flex h-[180px] items-center justify-center text-[12px] text-muted">
                        尚未執行查詢。
                      </div>
                    ) : (
                      <div className="divide-y divide-border">
                        {pagedResultRows.map((row, rowOffset) => (
                          <div key={`${activeTab.id}-vertical-${resultPage}-${rowOffset}`} className="px-4 py-2.5">
                            <p className="mb-2 text-[10px] font-bold uppercase tracking-[0.14em] text-faint">
                              Row {(resultPage - 1) * RESULT_PAGE_SIZE + rowOffset + 1}
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
                          <tr key={`${activeTab.id}-row-${resultPage}-${rowOffset}`} className="border-t border-border text-[12px] text-ink hover:bg-slate-50/70">
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
                      尚未執行查詢。
                    </div>
                  )}
                </div>
                {(resultView === 'result' || resultView === 'vertical') && activeTab.result && totalResultPages > 1 ? (
                  <div className="mt-3 flex items-center justify-end gap-2 text-[12px] text-muted">
                    <button
                      type="button"
                      onClick={() => setResultPage((current) => Math.max(1, current - 1))}
                      disabled={resultPage <= 1}
                      className="inline-flex h-8 items-center rounded-md border border-border bg-white px-3 font-semibold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      上一頁
                    </button>
                    <span>{resultPage} / {totalResultPages}</span>
                    <button
                      type="button"
                      onClick={() => setResultPage((current) => Math.min(totalResultPages, current + 1))}
                      disabled={resultPage >= totalResultPages}
                      className="inline-flex h-8 items-center rounded-md border border-border bg-white px-3 font-semibold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      下一頁
                    </button>
                  </div>
                ) : null}
              </div>
            </div>
          )}
        </section>
      </div>
      <ConfirmDialog
        open={savedQueryToDelete !== null}
        title="刪除常用 SQL"
        description={savedQueryToDelete ? `確認刪除「${savedQueryToDelete.label}」？刪除後若已達 10 筆上限，才可再新增其他常用 SQL。` : ''}
        confirmLabel="刪除"
        cancelLabel="取消"
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
