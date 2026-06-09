import { useEffect, useState } from 'react'
import { ArrowLeft, Loader2 } from 'lucide-react'
import { Link, useNavigate } from 'react-router-dom'
import { ApiError } from '@/shared/api/client'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { TicketType } from '@/shared/types/ticket'
import { createTicket, listConnections } from '@/modules/tickets/api'

export function NewTicketPage() {
  const navigate = useNavigate()
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [ticketType, setTicketType] = useState<TicketType>('ddl')
  const [dbConnectionId, setDbConnectionId] = useState('')
  const [sqlContent, setSqlContent] = useState('')
  const [connections, setConnections] = useState<DBConnection[]>([])
  const [loadingConnections, setLoadingConnections] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    let active = true

    async function loadConnections() {
      setLoadingConnections(true)
      try {
        const response = await listConnections()
        if (active) {
          setConnections(response.connections)
        }
      } catch (loadError) {
        if (active) {
          setError(loadError instanceof ApiError ? loadError.message : '讀取資料庫連線失敗。')
        }
      } finally {
        if (active) {
          setLoadingConnections(false)
        }
      }
    }

    void loadConnections()

    return () => {
      active = false
    }
  }, [])

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError('')
    setSubmitting(true)

    try {
      const created = await createTicket({
        title,
        description: description.trim() || null,
        sql_content: sqlContent,
        ticket_type: ticketType,
        db_connection_id: dbConnectionId ? Number(dbConnectionId) : null,
      })
      navigate(`/tickets/${created.id}`, { replace: true })
    } catch (submitError) {
      setError(submitError instanceof ApiError ? submitError.message : '建立工單失敗，請稍後重試。')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="flex h-full flex-col gap-6 p-5 sm:p-6">
      <div className="flex items-center justify-between gap-3 border-b border-border pb-5">
        <div>
          <p className="text-[11px] font-bold uppercase tracking-[0.2em] text-faint">New Ticket</p>
          <h2 className="mt-2 font-display text-2xl font-black tracking-tight text-ink">建立工單</h2>
          <p className="mt-1 text-sm text-muted">送出後會建立一筆新工單，後續可由 Reviewer / DBA 依權限繼續推進。</p>
        </div>
        <Link
          to="/tickets"
          className="inline-flex items-center gap-2 rounded-control border border-border bg-panel px-3 py-2 text-sm font-semibold text-ink transition hover:bg-page"
        >
          <ArrowLeft className="h-4 w-4" />
          返回列表
        </Link>
      </div>

      <form className="grid gap-6 xl:grid-cols-[1.1fr_0.9fr]" onSubmit={handleSubmit}>
        <section className="rounded-card border border-border bg-panel-soft p-5">
          <div className="grid gap-5">
            <label className="flex flex-col gap-1.5">
              <span className="text-xs font-semibold text-ink">標題</span>
              <input
                value={title}
                onChange={(event) => setTitle(event.target.value)}
                className="h-10 rounded-control border border-border bg-panel px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                placeholder="e.g. 建立索引、批次資料修正"
                disabled={submitting}
              />
            </label>

            <label className="flex flex-col gap-1.5">
              <span className="text-xs font-semibold text-ink">描述</span>
              <textarea
                value={description}
                onChange={(event) => setDescription(event.target.value)}
                className="min-h-28 rounded-card border border-border bg-panel px-3 py-2 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                placeholder="補充這次變更背景、影響範圍與執行考量。"
                disabled={submitting}
              />
            </label>

            <div className="grid gap-5 sm:grid-cols-2">
              <label className="flex flex-col gap-1.5">
                <span className="text-xs font-semibold text-ink">工單類型</span>
                <select
                  value={ticketType}
                  onChange={(event) => setTicketType(event.target.value as TicketType)}
                  className="h-10 rounded-control border border-border bg-panel px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                  disabled={submitting}
                >
                  <option value="ddl">DDL</option>
                  <option value="dml">DML</option>
                </select>
              </label>

              <label className="flex flex-col gap-1.5">
                <span className="text-xs font-semibold text-ink">目標資料庫</span>
                <select
                  value={dbConnectionId}
                  onChange={(event) => setDbConnectionId(event.target.value)}
                  className="h-10 rounded-control border border-border bg-panel px-3 text-sm text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                  disabled={submitting || loadingConnections}
                >
                  <option value="">未指定</option>
                  {connections.map((connection) => (
                    <option key={connection.id} value={String(connection.id)}>
                      {connection.name} ({connection.host}:{connection.port})
                    </option>
                  ))}
                </select>
              </label>
            </div>
          </div>
        </section>

        <section className="rounded-card border border-border bg-panel p-5">
          <label className="flex h-full flex-col gap-1.5">
            <span className="text-xs font-semibold text-ink">SQL 內容</span>
            <textarea
              value={sqlContent}
              onChange={(event) => setSqlContent(event.target.value)}
              className="min-h-[360px] flex-1 rounded-card border border-border bg-[#f9fbfd] px-3 py-3 font-mono text-sm text-[#1f2937] outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
              placeholder={'ALTER TABLE ...;\nUPDATE ...;'}
              disabled={submitting}
            />
          </label>

          <p className="mt-3 text-xs text-muted">
            後端目前要求 `title` 與 `sql_content` 必填，`ticket_type` 必須是 `ddl` 或 `dml`。
          </p>
        </section>

        <div className="xl:col-span-2">
          {error ? (
            <div className="mb-4 rounded-control border border-danger/20 bg-red-50 px-4 py-3 text-sm text-danger">
              {error}
            </div>
          ) : null}

          <div className="flex flex-wrap justify-end gap-2">
            <Link
              to="/tickets"
              className="inline-flex h-10 items-center justify-center rounded-control border border-border bg-panel px-4 text-sm font-semibold text-ink transition hover:bg-page"
            >
              取消
            </Link>
            <button
              type="submit"
              disabled={submitting || title.trim() === '' || sqlContent.trim() === ''}
              className="inline-flex h-10 items-center justify-center gap-2 rounded-control bg-brand px-4 text-sm font-bold text-white transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
              {submitting ? '建立中…' : '建立工單'}
            </button>
          </div>
        </div>
      </form>
    </div>
  )
}
