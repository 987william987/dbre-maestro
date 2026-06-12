import { useEffect, useState } from 'react'
import { ArrowLeft, FileText, Loader2, PanelTopOpen, ScrollText } from 'lucide-react'
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
    <div className="flex h-full flex-col gap-3 p-3 sm:p-4">
      <section className="rounded-xl border border-border bg-panel-soft shadow-soft">
        <div className="border-b border-border/80 px-4 py-3 sm:px-5">
          <div className="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
            <div className="max-w-3xl">
              <div className="flex flex-wrap items-center gap-2 text-[11px] font-semibold text-muted">
                <span className="rounded-full border border-border bg-white px-2.5 py-1 text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                  Tickets
                </span>
                <span>/</span>
                <span>Create Request</span>
              </div>
              <h2 className="mt-3 text-[24px] font-bold tracking-[-0.03em] text-ink">建立工單</h2>
              <p className="mt-2 text-[13px] leading-6 text-muted">
                先把變更目的、目標資料庫與 SQL 內容整理清楚，再交給 Reviewer / DBA 繼續推進。這一頁的重點不是資訊量，而是填寫節奏要順。
              </p>
            </div>
            <Link
              to="/tickets"
              className="inline-flex h-10 items-center gap-2 rounded-lg border border-border bg-white px-4 text-[13px] font-semibold text-ink transition hover:bg-panel-soft"
            >
              <ArrowLeft className="h-4 w-4" />
              返回列表
            </Link>
          </div>

          <div className="mt-4 grid gap-2 md:grid-cols-3">
            <div className="rounded-lg border border-border bg-white px-3 py-2.5 shadow-soft">
              <div className="flex items-center justify-between">
                <span className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">Ticket Type</span>
                <FileText className="h-3.5 w-3.5 text-muted" />
              </div>
              <p className="mt-1 text-[18px] font-bold tracking-tight text-ink">{ticketType.toUpperCase()}</p>
              <p className="mt-0.5 text-[12px] text-muted">依資料修改性質選擇 DDL 或 DML</p>
            </div>

            <div className="rounded-lg border border-border bg-white px-3 py-2.5 shadow-soft">
              <div className="flex items-center justify-between">
                <span className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">Connection Pool</span>
                <PanelTopOpen className="h-3.5 w-3.5 text-muted" />
              </div>
              <p className="mt-1 text-[16px] font-bold tracking-tight text-ink">
                {loadingConnections ? '載入中…' : `${connections.length} 個可選連線`}
              </p>
              <p className="mt-0.5 text-[12px] text-muted">可綁定既有 DB 連線，也可先不指定</p>
            </div>

            <div className="rounded-lg border border-border bg-white px-3 py-2.5 shadow-soft">
              <div className="flex items-center justify-between">
                <span className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">Readiness</span>
                <ScrollText className="h-3.5 w-3.5 text-muted" />
              </div>
              <p className="mt-1 text-[16px] font-bold tracking-tight text-ink">
                {title.trim() && sqlContent.trim() ? '可送出' : '待補資料'}
              </p>
              <p className="mt-0.5 text-[12px] text-muted">至少填寫標題與 SQL 內容才能建立工單</p>
            </div>
          </div>
        </div>
      </section>

      <form className="grid gap-3 xl:grid-cols-[0.95fr_1.05fr]" onSubmit={handleSubmit}>
        <section className="rounded-xl border border-border bg-panel shadow-soft">
          <div className="border-b border-border/80 px-4 py-3">
            <div className="flex items-center gap-2">
              <FileText className="h-4 w-4 text-accent" />
              <p className="text-[13px] font-semibold text-ink">Request Brief</p>
            </div>
          </div>

          <div className="mx-4 mt-4 rounded-lg border border-border bg-panel-soft px-4 py-3">
            <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">Metadata</p>
            <p className="mt-1.5 text-[12px] text-muted">先定義工單上下文，讓後續審核者快速理解這筆 SQL 的目標與風險。</p>
          </div>

          <div className="grid gap-4 px-4 py-4">
            <label className="flex flex-col gap-1.5">
              <span className="text-[12px] font-semibold text-ink">標題</span>
              <input
                value={title}
                onChange={(event) => setTitle(event.target.value)}
                className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                placeholder="e.g. 建立索引、批次資料修正"
                disabled={submitting}
              />
            </label>

            <label className="flex flex-col gap-1.5">
              <span className="text-[12px] font-semibold text-ink">描述</span>
              <textarea
                value={description}
                onChange={(event) => setDescription(event.target.value)}
                className="min-h-28 rounded-lg border border-border bg-panel-soft px-3 py-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                placeholder="補充這次變更背景、影響範圍與執行考量。"
                disabled={submitting}
              />
            </label>

            <div className="grid gap-4 sm:grid-cols-2">
              <label className="flex flex-col gap-1.5">
                <span className="text-[12px] font-semibold text-ink">工單類型</span>
                <select
                  value={ticketType}
                  onChange={(event) => setTicketType(event.target.value as TicketType)}
                  className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] font-semibold text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                  disabled={submitting}
                >
                  <option value="ddl">DDL</option>
                  <option value="dml">DML</option>
                </select>
              </label>

              <label className="flex flex-col gap-1.5">
                <span className="text-[12px] font-semibold text-ink">目標資料庫</span>
                <select
                  value={dbConnectionId}
                  onChange={(event) => setDbConnectionId(event.target.value)}
                  className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
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

        <section className="rounded-xl border border-border bg-panel shadow-soft">
          <div className="border-b border-border/80 px-4 py-3">
            <div className="flex items-center gap-2">
              <ScrollText className="h-4 w-4 text-accent" />
              <p className="text-[13px] font-semibold text-ink">SQL Draft</p>
            </div>
          </div>

          <label className="flex h-full flex-col gap-1.5 px-4 py-4">
            <span className="text-[12px] font-semibold text-ink">SQL 內容</span>
            <textarea
              value={sqlContent}
              onChange={(event) => setSqlContent(event.target.value)}
              className="min-h-[430px] flex-1 rounded-xl border border-border bg-panel-soft px-4 py-4 font-mono text-[13px] leading-7 text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
              placeholder={'ALTER TABLE ...;\nUPDATE ...;'}
              disabled={submitting}
            />
          </label>

          <div className="mx-4 mb-4 rounded-lg border border-border bg-panel-soft px-4 py-3">
            <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">Execution Notes</p>
            <p className="mt-1.5 text-[12px] leading-6 text-muted">
              後端目前要求 `title` 與 `sql_content` 必填，`ticket_type` 必須是 `ddl` 或 `dml`。先把 SQL 以可審核的形式整理乾淨，比堆更多欄位重要。
            </p>
          </div>
        </section>

        <div className="xl:col-span-2">
          {error ? (
            <div className="mb-4 rounded-lg border border-danger/20 bg-red-50 px-4 py-3 text-[13px] text-danger">
              {error}
            </div>
          ) : null}

          <div className="flex flex-wrap justify-end gap-2.5">
            <Link
              to="/tickets"
              className="inline-flex h-10 items-center justify-center rounded-lg border border-border bg-white px-4 text-[13px] font-semibold text-ink transition hover:bg-panel-soft"
            >
              取消
            </Link>
            <button
              type="submit"
              disabled={submitting || title.trim() === '' || sqlContent.trim() === ''}
              className="inline-flex h-10 items-center justify-center gap-2 rounded-lg bg-brand px-5 text-[13px] font-bold text-white shadow-soft transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
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
