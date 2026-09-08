import { useEffect, useMemo, useRef, useState } from 'react'
import CodeMirror from '@uiw/react-codemirror'
import { MySQL, PostgreSQL, sql } from '@codemirror/lang-sql'
import { EditorView } from '@codemirror/view'
import { Copy, Download, List, Play, Search, ShieldAlert, Table2, X } from 'lucide-react'
import { executeAdminQuery } from '@/modules/sql-editor/api'
import { ApiError } from '@/shared/api/client'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { QueryResult } from '@/shared/types/sqlEditor'

type AdminConsoleEntry = {
  id: number
  sql: string
  result: QueryResult | null
  error: string
}

type AdminQueryConsoleProps = {
  connection: DBConnection
  database: string
  schema: string
  endpoint: string
  credentialRole: string
  onExit: () => void
}

function adminEditorExtension(connection: DBConnection) {
  if (connection.db_type === 'mysql') return sql({ dialect: MySQL })
  if (connection.db_type === 'postgres' || connection.db_type === 'postgresql') return sql({ dialect: PostgreSQL })
  return []
}

const adminSelectionTheme = EditorView.theme({
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection': {
    backgroundColor: '#2563eb !important',
    color: '#ffffff !important',
  },
})

function mysqlDatabaseFromUse(statement: string) {
  const match = statement.trim().match(/^USE\s+(`(?:``|[^`])+`|[A-Za-z0-9_$.-]+)\s*;?$/i)
  if (!match) return null
  const identifier = match[1]
  return identifier.startsWith('`') ? identifier.slice(1, -1).replace(/``/g, '`') : identifier
}

function postgresDatabaseFromConnect(statement: string) {
  const match = statement.trim().match(/^\\(?:c|connect)\s+("(?:""|[^"])+"|\S+)$/i)
  if (!match) return null
  const identifier = match[1]
  return identifier.startsWith('"') ? identifier.slice(1, -1).replace(/""/g, '"') : identifier
}

function resultRowsAsText(result: QueryResult, separator: string) {
  const escapeCell = (value: string | null) => {
    const text = value === null ? '' : String(value)
    if (separator === ',' && /[",\r\n]/.test(text)) return `"${text.replace(/"/g, '""')}"`
    return text
  }
  return [result.columns, ...result.rows].map((row) => row.map(escapeCell).join(separator)).join('\n')
}

function AdminResult({ entry }: { entry: AdminConsoleEntry }) {
  const [view, setView] = useState<'table' | 'vertical'>('table')
  const [filter, setFilter] = useState('')
  const result = entry.result
  if (!result) return null
  if (result.columns.length === 0) {
    return <p className="font-mono text-[12px] text-emerald-300">{result.command || 'COMMAND'} OK, {result.affected_rows ?? 0} rows affected</p>
  }

  const keyword = filter.trim().toLowerCase()
  const rows = keyword
    ? result.rows.filter((row) => row.some((value) => String(value ?? '').toLowerCase().includes(keyword)))
    : result.rows

  async function copyAll() {
    await navigator.clipboard?.writeText(resultRowsAsText(result!, '\t'))
  }

  function exportCSV() {
    const blob = new Blob([resultRowsAsText(result!, ',')], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `admin-query-${entry.id}.csv`
    link.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div>
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <label className="flex h-8 min-w-[180px] flex-1 items-center gap-2 border border-slate-700 bg-[#0b1220] px-2 text-slate-400">
          <Search className="h-3.5 w-3.5" />
          <input aria-label={`Filter result ${entry.id}`} value={filter} onChange={(event) => setFilter(event.target.value)} className="min-w-0 flex-1 bg-transparent text-[12px] text-slate-100 outline-none" />
        </label>
        <span className="text-[11px] text-slate-400">{rows.length} rows</span>
        <div className="inline-flex border border-slate-700">
          <button type="button" aria-label="Table view" onClick={() => setView('table')} className={`h-8 px-2 ${view === 'table' ? 'bg-slate-700 text-white' : 'text-slate-400 hover:text-white'}`}><Table2 className="h-3.5 w-3.5" /></button>
          <button type="button" aria-label="Vertical view" onClick={() => setView('vertical')} className={`h-8 border-l border-slate-700 px-2 ${view === 'vertical' ? 'bg-slate-700 text-white' : 'text-slate-400 hover:text-white'}`}><List className="h-3.5 w-3.5" /></button>
        </div>
        <button type="button" onClick={() => void copyAll()} className="inline-flex h-8 items-center gap-1.5 border border-slate-700 px-2 text-[11px] text-slate-300 hover:bg-slate-800"><Copy className="h-3.5 w-3.5" />Copy all</button>
        <button type="button" onClick={exportCSV} className="inline-flex h-8 items-center gap-1.5 bg-indigo-500 px-2 text-[11px] font-semibold text-white hover:bg-indigo-400"><Download className="h-3.5 w-3.5" />Export</button>
      </div>
      {view === 'vertical' ? (
        <div className="space-y-2">
          {rows.map((row, rowIndex) => (
            <div key={rowIndex} className="grid grid-cols-[max-content_minmax(180px,1fr)] border border-slate-700 font-mono text-[12px]">
              {result.columns.map((column, columnIndex) => (
                <div key={columnIndex} className="contents">
                  <div className="border-b border-r border-slate-700 bg-slate-800 px-3 py-2 text-slate-400 last:border-b-0">{column}</div>
                  <div className="border-b border-slate-700 px-3 py-2 text-slate-100 last:border-b-0">{row[columnIndex] === null ? '(null)' : String(row[columnIndex] ?? '')}</div>
                </div>
              ))}
            </div>
          ))}
        </div>
      ) : (
        <div className="overflow-auto border border-slate-700">
          <table className="min-w-full border-collapse font-mono text-[12px]">
            <thead className="bg-slate-800 text-left text-slate-300"><tr>{result.columns.map((column, index) => <th key={`${column}-${index}`} className="border-b border-slate-700 px-3 py-2 font-medium">{column}</th>)}</tr></thead>
            <tbody className="divide-y divide-slate-800 text-slate-100">{rows.map((row, rowIndex) => <tr key={rowIndex}>{result.columns.map((_, columnIndex) => <td key={columnIndex} className="whitespace-nowrap px-3 py-2">{row[columnIndex] === null ? '(null)' : String(row[columnIndex] ?? '')}</td>)}</tr>)}</tbody>
          </table>
        </div>
      )}
    </div>
  )
}

export function AdminQueryConsole({ connection, database, schema, endpoint, credentialRole, onExit }: AdminQueryConsoleProps) {
  const [command, setCommand] = useState('')
  const [entries, setEntries] = useState<AdminConsoleEntry[]>([])
  const [running, setRunning] = useState(false)
  const [historyIndex, setHistoryIndex] = useState<number | null>(null)
  const [currentDatabase, setCurrentDatabase] = useState(database)
  const [currentSchema, setCurrentSchema] = useState(schema)
  const historyDraft = useRef('')
  const outputRef = useRef<HTMLDivElement | null>(null)
  const latestEntryRef = useRef<HTMLElement | null>(null)
  const nextEntryID = useRef(1)
  const extensions = useMemo(() => [adminEditorExtension(connection), adminSelectionTheme], [connection])

  useEffect(() => {
    if (outputRef.current && latestEntryRef.current) {
      outputRef.current.scrollTop = latestEntryRef.current.offsetTop - outputRef.current.offsetTop
    }
  }, [entries])

  async function executeCommand() {
    const statement = command.trim()
    if (!statement || running) return

    const entryID = nextEntryID.current++
    setHistoryIndex(null)
    historyDraft.current = ''
    setRunning(true)
    try {
      const result = await executeAdminQuery({
        db_connection_id: connection.id,
        sql: statement,
        database: currentDatabase || undefined,
        schema: currentSchema || undefined,
        redis_db_index: connection.db_type === 'redis' && database ? Number(database) : undefined,
      })
      if (connection.db_type === 'mysql') {
        const nextDatabase = mysqlDatabaseFromUse(statement)
        if (nextDatabase) setCurrentDatabase(nextDatabase)
      } else if (connection.db_type === 'postgres' || connection.db_type === 'postgresql') {
        const nextDatabase = postgresDatabaseFromConnect(statement)
        if (nextDatabase) {
          setCurrentDatabase(nextDatabase)
          setCurrentSchema('')
        }
      }
      setEntries((current) => [...current, { id: entryID, sql: statement, result, error: '' }])
      setCommand('')
    } catch (error) {
      const message = error instanceof ApiError || error instanceof Error ? error.message : 'Admin command failed.'
      setEntries((current) => [...current, { id: entryID, sql: statement, result: null, error: message }])
    } finally {
      setRunning(false)
    }
  }

  return (
    <div className="flex h-[calc(100vh-12rem)] min-h-[520px] max-h-[900px] w-full flex-col overflow-hidden bg-[#111827] text-slate-100 selection:bg-blue-600 selection:text-white">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-700 px-4 py-3">
        <div className="flex min-w-0 items-center gap-3">
          <ShieldAlert className="h-5 w-5 shrink-0 text-amber-400" />
          <div className="min-w-0">
            <p className="text-[13px] font-semibold text-white">Administrator console</p>
            <p className="truncate font-mono text-[11px] text-slate-400">{connection.name} · {endpoint} · {credentialRole}{currentDatabase ? ` · ${currentDatabase}` : ''}</p>
          </div>
        </div>
        <button
          type="button"
          onClick={onExit}
          disabled={running}
          className="inline-flex h-9 items-center gap-2 border border-slate-600 px-3 text-[12px] font-semibold text-slate-100 transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
        >
          <X className="h-4 w-4" />
          Exit admin mode
        </button>
      </div>

      <div ref={outputRef} className="min-h-0 flex-1 overflow-auto p-4">
        {entries.length === 0 ? (
          <p className="font-mono text-[12px] text-slate-500">No administrator commands executed in this session.</p>
        ) : (
          <div className="space-y-5">
            {entries.map((entry, index) => (
              <section ref={index === entries.length - 1 ? latestEntryRef : undefined} key={entry.id} className="border-b border-slate-700 pb-5">
                <pre className="mb-3 whitespace-pre-wrap break-words font-mono text-[12px] leading-5 text-slate-200"><span className="text-amber-400">SQL&gt; </span>{entry.sql}</pre>
                {entry.error ? (
                  <pre className="whitespace-pre-wrap font-mono text-[12px] text-red-300">ERROR: {entry.error}</pre>
                ) : <AdminResult entry={entry} />}
                {entry.result ? <p className="mt-2 font-mono text-[11px] text-slate-500">{entry.result.row_count} rows · {entry.result.duration_ms} ms</p> : null}
              </section>
            ))}
          </div>
        )}
      </div>

      <div
        className="shrink-0 border-t border-slate-700 p-4"
        onKeyDownCapture={(event) => {
          if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
            event.preventDefault()
            void executeCommand()
          } else if ((event.key === 'ArrowUp' || event.key === 'ArrowDown') && entries.length > 0) {
            event.preventDefault()
            if (event.key === 'ArrowUp') {
              if (historyIndex === null) historyDraft.current = command
              const nextIndex = historyIndex === null ? entries.length - 1 : Math.max(0, historyIndex - 1)
              setHistoryIndex(nextIndex)
              setCommand(entries[nextIndex].sql)
            } else if (historyIndex !== null) {
              const nextIndex = historyIndex + 1
              if (nextIndex >= entries.length) {
                setHistoryIndex(null)
                setCommand(historyDraft.current)
              } else {
                setHistoryIndex(nextIndex)
                setCommand(entries[nextIndex].sql)
              }
            }
          }
        }}
      >
        <div translate="no" className="overflow-hidden border border-slate-600 bg-[#0b1220]">
          <CodeMirror
            aria-label="Administrator command"
            value={command}
            minHeight="120px"
            maxHeight="240px"
            extensions={extensions}
            onChange={setCommand}
            theme="dark"
            basicSetup={{ lineNumbers: false, foldGutter: false }}
          />
        </div>
        <div className="mt-3 flex justify-end">
          <button
            type="button"
            onClick={() => void executeCommand()}
            disabled={running || !command.trim()}
            className="inline-flex h-9 items-center gap-2 bg-amber-500 px-4 text-[12px] font-bold text-black transition hover:bg-amber-400 disabled:cursor-not-allowed disabled:bg-slate-700 disabled:text-slate-400"
          >
            <Play className="h-4 w-4" />
            {running ? 'Executing...' : 'Execute'}
          </button>
        </div>
      </div>
    </div>
  )
}
