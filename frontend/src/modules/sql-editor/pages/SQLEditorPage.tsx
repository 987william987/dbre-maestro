import { useEffect, useMemo, useState } from 'react'
import CodeMirror from '@uiw/react-codemirror'
import { javascript } from '@codemirror/lang-javascript'
import { sql } from '@codemirror/lang-sql'
import {
  Database,
  Download,
  FileClock,
  FolderTree,
  History,
  Play,
  Plus,
  Star,
  StarOff,
  Table2,
  X,
} from 'lucide-react'
import { ApiError } from '@/shared/api/client'
import { useAuth } from '@/shared/auth/AuthContext'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { MetadataColumn, MetadataTable, QueryResult } from '@/shared/types/sqlEditor'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { useToast } from '@/shared/ui/ToastContext'
import { listDBConnections } from '@/modules/db-connections/api'
import { createExportRequest } from '@/modules/exports/api'
import { executeQuery, listMetadataColumns, listMetadataTables } from '@/modules/sql-editor/api'

type EditorTab = {
  id: string
  title: string
  connectionId: number | null
  sql: string
  result: QueryResult | null
  error: string
  lastRunAt: string | null
}

type QueryHistoryEntry = {
  id: string
  connectionId: number
  sql: string
  createdAt: string
}

type FavoriteQueryEntry = {
  id: string
  label: string
  connectionId: number
  sql: string
}

type PersistedState = {
  activeTabId: string
  tabs: EditorTab[]
  history: QueryHistoryEntry[]
  favorites: FavoriteQueryEntry[]
}

const STORAGE_PREFIX = 'dbre_maestro.sql_editor'
const DEFAULT_SQL = 'SELECT 1;'
const HISTORY_LIMIT = 20

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
      history: Array.isArray(parsed.history) ? parsed.history.slice(0, HISTORY_LIMIT) : [],
      favorites: Array.isArray(parsed.favorites) ? parsed.favorites : [],
    }
  } catch {
    return null
  }
}

export function SQLEditorPage() {
  const { user } = useAuth()
  const { pushToast } = useToast()
  const storageKey = user ? `${STORAGE_PREFIX}.${user.id}` : `${STORAGE_PREFIX}.anonymous`
  const [connections, setConnections] = useState<DBConnection[]>([])
  const [connectionsLoading, setConnectionsLoading] = useState(true)
  const [connectionsError, setConnectionsError] = useState('')
  const [tabs, setTabs] = useState<EditorTab[]>([createTab()])
  const [activeTabId, setActiveTabId] = useState<string>(() => createTab().id)
  const [history, setHistory] = useState<QueryHistoryEntry[]>([])
  const [favorites, setFavorites] = useState<FavoriteQueryEntry[]>([])
  const [runningTabId, setRunningTabId] = useState<string | null>(null)
  const [exportingTabId, setExportingTabId] = useState<string | null>(null)
  const [metadataLoading, setMetadataLoading] = useState(false)
  const [tables, setTables] = useState<MetadataTable[]>([])
  const [selectedTable, setSelectedTable] = useState<MetadataTable | null>(null)
  const [columns, setColumns] = useState<MetadataColumn[]>([])
  const [columnsLoading, setColumnsLoading] = useState(false)
  const [metadataError, setMetadataError] = useState('')
  const [editorError, setEditorError] = useState('')

  useEffect(() => {
    const restored = safeParseState(window.localStorage.getItem(storageKey))
    if (!restored) {
      const firstTab = createTab()
      setTabs([firstTab])
      setActiveTabId(firstTab.id)
      setHistory([])
      setFavorites([])
      return
    }

    setTabs(restored.tabs)
    setActiveTabId(restored.activeTabId)
    setHistory(restored.history)
    setFavorites(restored.favorites)
  }, [storageKey])

  useEffect(() => {
    if (!tabs.length) {
      return
    }

    const state: PersistedState = {
      activeTabId,
      tabs,
      history,
      favorites,
    }
    window.localStorage.setItem(storageKey, JSON.stringify(state))
  }, [activeTabId, favorites, history, storageKey, tabs])

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
        setTabs((currentTabs) => currentTabs.map((tab) => {
          if (tab.connectionId !== null || response.connections.length === 0) {
            return tab
          }
          return { ...tab, connectionId: response.connections[0].id }
        }))
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

  const activeTab = useMemo(
    () => tabs.find((tab) => tab.id === activeTabId) ?? tabs[0] ?? null,
    [activeTabId, tabs],
  )

  useEffect(() => {
    if (!activeTab?.connectionId) {
      setTables([])
      setSelectedTable(null)
      setColumns([])
      setMetadataError('')
      return
    }

    let active = true

    async function loadTables() {
      setMetadataLoading(true)
      setMetadataError('')
      try {
        const response = await listMetadataTables(activeTab.connectionId!)
        if (!active) {
          return
        }
        setTables(response.tables)
        setSelectedTable((current) => {
          if (current && response.tables.some((table) => table.schema === current.schema && table.name === current.name)) {
            return current
          }
          return response.tables[0] ?? null
        })
      } catch (error) {
        if (active) {
          setMetadataError(error instanceof ApiError ? error.message : '讀取 metadata 失敗。')
          setTables([])
          setSelectedTable(null)
          setColumns([])
        }
      } finally {
        if (active) {
          setMetadataLoading(false)
        }
      }
    }

    void loadTables()

    return () => {
      active = false
    }
  }, [activeTab?.connectionId])

  useEffect(() => {
    if (!activeTab?.connectionId || !selectedTable) {
      setColumns([])
      return
    }

    let active = true
    const connectionId = activeTab.connectionId
    const schema = selectedTable.schema
    const table = selectedTable.name

    async function loadColumns() {
      setColumnsLoading(true)
      try {
        const response = await listMetadataColumns(connectionId, schema, table)
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
  }, [activeTab?.connectionId, selectedTable])

  function updateActiveTab(patch: Partial<EditorTab>) {
    if (!activeTab) {
      return
    }

    setTabs((currentTabs) => currentTabs.map((tab) => (tab.id === activeTab.id ? { ...tab, ...patch } : tab)))
  }

  function handleAddTab() {
    const nextTab = createTab(tabs.length + 1)
    if (connections.length > 0) {
      nextTab.connectionId = connections[0].id
    }
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
    if (!activeTab?.connectionId || !activeTab.sql.trim()) {
      setEditorError('請先選擇資料庫連線並輸入查詢內容。')
      return
    }

    setEditorError('')
    setRunningTabId(activeTab.id)
    updateActiveTab({ error: '' })

    try {
      const result = await executeQuery({
        db_connection_id: activeTab.connectionId,
        sql: activeTab.sql,
      })

      const now = new Date().toISOString()
      updateActiveTab({
        result,
        error: '',
        lastRunAt: now,
      })
      setHistory((current) => {
        const nextEntry: QueryHistoryEntry = {
          id: `${Date.now()}`,
          connectionId: activeTab.connectionId!,
          sql: activeTab.sql,
          createdAt: now,
        }
        const deduped = current.filter((entry) => !(entry.connectionId === nextEntry.connectionId && entry.sql === nextEntry.sql))
        return [nextEntry, ...deduped].slice(0, HISTORY_LIMIT)
      })
      pushToast('查詢已完成', 'success')
    } catch (error) {
      const message = error instanceof ApiError ? error.message : '查詢執行失敗。'
      updateActiveTab({
        error: message,
        result: null,
      })
      setEditorError(message)
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
      })
      window.open(response.download_url, '_blank', 'noopener,noreferrer')
      pushToast('已建立匯出請求', 'success')
    } catch (error) {
      setEditorError(error instanceof ApiError ? error.message : '建立匯出請求失敗。')
    } finally {
      setExportingTabId(null)
    }
  }

  function handleToggleFavorite() {
    if (!activeTab?.connectionId || !activeTab.sql.trim()) {
      return
    }

    const existing = favorites.find((item) => item.connectionId === activeTab.connectionId && item.sql === activeTab.sql)
    if (existing) {
      setFavorites((current) => current.filter((item) => item.id !== existing.id))
      pushToast('已從收藏移除', 'info')
      return
    }

    const nextFavorite: FavoriteQueryEntry = {
      id: `${Date.now()}`,
      label: activeTab.title,
      connectionId: activeTab.connectionId,
      sql: activeTab.sql,
    }
    setFavorites((current) => [nextFavorite, ...current])
    pushToast('已加入收藏', 'success')
  }

  function applySavedQuery(entry: Pick<FavoriteQueryEntry, 'connectionId' | 'sql' | 'label'>) {
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
  }

  const isFavorited = !!(activeTab && favorites.some((item) => item.connectionId === activeTab.connectionId && item.sql === activeTab.sql))
  const editorExtensions = useMemo(
    () => [activeTab && connections.find((connection) => connection.id === activeTab.connectionId)?.db_type === 'redis' ? javascript() : sql()],
    [activeTab, connections],
  )

  return (
    <div className="flex h-full min-h-0 flex-col gap-3 p-3 sm:p-4">
      <section className="rounded-[22px] border border-white/85 bg-[rgba(248,250,252,0.82)] shadow-soft">
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
              <div className="rounded-[14px] border border-border bg-white px-3 py-2.5 shadow-soft">
                <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">Tabs</p>
                <p className="mt-1 text-[20px] font-bold tracking-tight text-ink">{tabs.length}</p>
              </div>
              <div className="rounded-[14px] border border-border bg-white px-3 py-2.5 shadow-soft">
                <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">History</p>
                <p className="mt-1 text-[20px] font-bold tracking-tight text-ink">{history.length}</p>
              </div>
              <div className="rounded-[14px] border border-border bg-white px-3 py-2.5 shadow-soft">
                <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">Favorites</p>
                <p className="mt-1 text-[20px] font-bold tracking-tight text-ink">{favorites.length}</p>
              </div>
            </div>
          </div>
        </div>
      </section>

      {connectionsError ? <InlineAlert>{connectionsError}</InlineAlert> : null}
      {editorError ? <InlineAlert>{editorError}</InlineAlert> : null}

      <div className="grid min-h-0 flex-1 gap-3 xl:grid-cols-[280px_minmax(0,1fr)]">
        <section className="flex min-h-0 flex-col rounded-[22px] border border-white/85 bg-white/92 shadow-soft">
          <div className="border-b border-border/80 px-4 py-3">
            <div className="flex items-center gap-2">
              <FolderTree className="h-4 w-4 text-accent" />
              <p className="text-[13px] font-semibold text-ink">Workspace Assets</p>
            </div>
          </div>

          <div className="flex min-h-0 flex-1 flex-col gap-3 p-4">
            <div className="rounded-[14px] border border-border bg-panel-soft px-3 py-3">
              <div className="flex items-center gap-2">
                <Database className="h-4 w-4 text-muted" />
                <p className="text-[12px] font-semibold text-ink">Connections</p>
              </div>
              {connectionsLoading ? (
                <p className="mt-2 text-[12px] text-muted">載入中…</p>
              ) : connections.length === 0 ? (
                <p className="mt-2 text-[12px] text-muted">目前沒有可用的 DB connection。</p>
              ) : (
                <select
                  aria-label="SQL Editor Connection"
                  value={activeTab?.connectionId ?? ''}
                  onChange={(event) => updateActiveTab({ connectionId: Number(event.target.value), result: null, error: '' })}
                  className="mt-2 h-10 w-full rounded-[12px] border border-border bg-white px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                >
                  {connections.map((connection) => (
                    <option key={connection.id} value={connection.id}>
                      {connection.name} ({connection.db_type})
                    </option>
                  ))}
                </select>
              )}
            </div>

            <div className="rounded-[14px] border border-border bg-panel-soft px-3 py-3">
              <div className="flex items-center gap-2">
                <History className="h-4 w-4 text-muted" />
                <p className="text-[12px] font-semibold text-ink">History</p>
              </div>
              <div className="mt-2 flex max-h-[180px] flex-col gap-1 overflow-y-auto">
                {history.length === 0 ? (
                  <p className="text-[12px] text-muted">尚無查詢歷史。</p>
                ) : (
                  history.map((entry) => (
                    <button
                      key={entry.id}
                      type="button"
                      onClick={() => applySavedQuery({ connectionId: entry.connectionId, sql: entry.sql, label: 'History Query' })}
                      className="rounded-[10px] border border-transparent bg-white px-3 py-2 text-left text-[12px] transition hover:border-border hover:bg-page"
                    >
                      <p className="truncate font-semibold text-ink">{entry.sql}</p>
                      <p className="mt-1 text-[11px] text-muted">{new Date(entry.createdAt).toLocaleString()}</p>
                    </button>
                  ))
                )}
              </div>
            </div>

            <div className="min-h-0 flex-1 rounded-[14px] border border-border bg-panel-soft px-3 py-3">
              <div className="flex items-center gap-2">
                <Table2 className="h-4 w-4 text-muted" />
                <p className="text-[12px] font-semibold text-ink">Metadata</p>
              </div>
              {metadataError ? <InlineAlert className="mt-2" tone="info">{metadataError}</InlineAlert> : null}
              {metadataLoading ? (
                <p className="mt-2 text-[12px] text-muted">載入 tables 中…</p>
              ) : (
                <div className="mt-2 grid min-h-0 gap-3 lg:grid-cols-[0.9fr_1.1fr] xl:grid-cols-1">
                  <div className="max-h-[220px] overflow-y-auto rounded-[12px] border border-border bg-white p-1">
                    {tables.length === 0 ? (
                      <p className="px-2 py-2 text-[12px] text-muted">尚無 tables。</p>
                    ) : (
                      tables.map((table) => (
                        <button
                          key={`${table.schema}.${table.name}`}
                          type="button"
                          onClick={() => setSelectedTable(table)}
                          className={`w-full rounded-[10px] px-2 py-2 text-left text-[12px] transition ${
                            selectedTable?.schema === table.schema && selectedTable?.name === table.name
                              ? 'bg-panel-soft text-ink'
                              : 'text-muted hover:bg-page hover:text-ink'
                          }`}
                        >
                          <p className="font-semibold">{table.name}</p>
                          <p className="mt-1 text-[11px]">{table.schema}</p>
                        </button>
                      ))
                    )}
                  </div>

                  <div className="max-h-[220px] overflow-y-auto rounded-[12px] border border-border bg-white p-1">
                    {selectedTable ? (
                      <>
                        <p className="px-2 py-2 text-[12px] font-semibold text-ink">
                          {selectedTable.schema}.{selectedTable.name}
                        </p>
                        {columnsLoading ? (
                          <p className="px-2 py-2 text-[12px] text-muted">載入 columns 中…</p>
                        ) : columns.length === 0 ? (
                          <p className="px-2 py-2 text-[12px] text-muted">尚無 columns。</p>
                        ) : (
                          columns.map((column) => (
                            <div key={column.name} className="rounded-[10px] px-2 py-2 text-[12px] text-ink">
                              <p className="font-semibold">{column.name}</p>
                              <p className="mt-1 text-[11px] text-muted">{column.column_type}</p>
                            </div>
                          ))
                        )}
                      </>
                    ) : (
                      <p className="px-2 py-2 text-[12px] text-muted">先選擇一個 table。</p>
                    )}
                  </div>
                </div>
              )}
            </div>
          </div>
        </section>

        <section className="flex min-h-0 flex-col rounded-[22px] border border-white/85 bg-white/92 shadow-soft">
          <div className="border-b border-border/80 px-4 py-3">
            <div className="flex flex-wrap items-center gap-2">
              {tabs.map((tab) => (
                <button
                  key={tab.id}
                  type="button"
                  onClick={() => setActiveTabId(tab.id)}
                  className={`inline-flex items-center gap-2 rounded-[12px] border px-3 py-2 text-[12px] font-semibold transition ${
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
                className="inline-flex items-center gap-2 rounded-[12px] border border-border bg-white px-3 py-2 text-[12px] font-semibold text-ink transition hover:bg-page"
              >
                <Plus className="h-4 w-4" />
                New Tab
              </button>
            </div>
          </div>

          {!activeTab ? (
            <LoadingBlock message="載入 editor 中…" className="m-4 min-h-[320px] rounded-[18px] border-white/80 bg-white/86" />
          ) : (
            <div className="flex min-h-0 flex-1 flex-col">
              <div className="flex flex-wrap items-center gap-2 border-b border-border/80 px-4 py-3">
                <button
                  type="button"
                  onClick={handleRunQuery}
                  disabled={runningTabId === activeTab.id || !activeTab.connectionId}
                  className="inline-flex h-10 items-center gap-2 rounded-[12px] bg-brand px-4 text-[13px] font-bold text-white transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  <Play className="h-4 w-4" />
                  {runningTabId === activeTab.id ? '執行中…' : 'Run Query'}
                </button>
                <button
                  type="button"
                  onClick={handleToggleFavorite}
                  className="inline-flex h-10 items-center gap-2 rounded-[12px] border border-border bg-white px-4 text-[13px] font-semibold text-ink transition hover:bg-page"
                >
                  {isFavorited ? <StarOff className="h-4 w-4" /> : <Star className="h-4 w-4" />}
                  {isFavorited ? '取消收藏' : '加入收藏'}
                </button>
                <select
                  aria-label="SQL Editor Active Connection"
                  value={activeTab.connectionId ?? ''}
                  onChange={(event) => updateActiveTab({ connectionId: Number(event.target.value), result: null, error: '' })}
                  className="h-10 min-w-[220px] rounded-[12px] border border-border bg-white px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                >
                  {connections.map((connection) => (
                    <option key={connection.id} value={connection.id}>
                      {connection.name} ({connection.db_type})
                    </option>
                  ))}
                </select>
                {activeTab.lastRunAt ? (
                  <p className="text-[12px] text-muted">Last run: {new Date(activeTab.lastRunAt).toLocaleString()}</p>
                ) : null}
              </div>

              <div className="min-h-0 flex-1 p-4">
                <div className="overflow-hidden rounded-[18px] border border-[#d8e2ee] bg-[#eef4fb]">
                  <CodeMirror
                    value={activeTab.sql}
                    height="320px"
                    extensions={editorExtensions}
                    onChange={(value) => updateActiveTab({ sql: value })}
                    theme="light"
                    basicSetup={{
                      lineNumbers: true,
                      foldGutter: false,
                      highlightActiveLine: false,
                    }}
                  />
                </div>
              </div>

              <div className="border-t border-border/80 px-4 py-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex items-center gap-3 text-[12px] text-muted">
                    <span className="inline-flex items-center gap-2">
                      <FileClock className="h-4 w-4" />
                      Query Result
                    </span>
                    {activeTab.result ? (
                      <span>{activeTab.result.row_count} rows / {activeTab.result.duration_ms} ms</span>
                    ) : null}
                  </div>
                  <button
                    type="button"
                    onClick={() => void handleExport()}
                    disabled={exportingTabId === activeTab.id || !activeTab.connectionId || !activeTab.sql.trim()}
                    className="inline-flex h-9 items-center gap-2 rounded-[10px] border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    <Download className="h-4 w-4" />
                    {exportingTabId === activeTab.id ? '匯出中…' : 'Export Result'}
                  </button>
                </div>

                {activeTab.error ? <InlineAlert className="mt-3">{activeTab.error}</InlineAlert> : null}

                <div className="mt-3 max-h-[320px] overflow-auto rounded-[16px] border border-border bg-white">
                  {activeTab.result ? (
                    <table className="min-w-full border-collapse">
                      <thead className="bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                        <tr>
                          {activeTab.result.columns.map((column) => (
                            <th key={column} className="px-3 py-3">{column}</th>
                          ))}
                        </tr>
                      </thead>
                      <tbody>
                        {activeTab.result.rows.map((row, rowIndex) => (
                          <tr key={`${activeTab.id}-row-${rowIndex}`} className="border-t border-border text-[12px] text-ink hover:bg-slate-50/70">
                            {row.map((cell, cellIndex) => (
                              <td key={`${activeTab.id}-cell-${rowIndex}-${cellIndex}`} className="px-3 py-2.5 align-top">
                                {cell === null ? <span className="text-muted">(null)</span> : String(cell)}
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
              </div>
            </div>
          )}
        </section>
      </div>
    </div>
  )
}
