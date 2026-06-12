import { useEffect, useState } from 'react'
import { ArrowLeft, FileText, Loader2, ScrollText } from 'lucide-react'
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
              <h2 className="text-[24px] font-bold tracking-[-0.03em] text-ink">建立工單</h2>
              <p className="mt-2 text-[13px] leading-6 text-muted">
                填寫變更內容與目標資料庫，送出後由 Reviewer / DBA 接手審核與執行。
              </p>
            </div>
            <Link
              to="/tickets"
              className="inline-flex h-10 shrink-0 items-center gap-2 rounded-lg border border-border bg-white px-4 text-[13px] font-semibold text-ink transition hover:bg-panel-soft"
            >
              <ArrowLeft className="h-4 w-4" />
              返回列表
            </Link>
          </div>
        </div>
      </section>

      <form className="grid gap-3 xl:grid-cols-[0.95fr_1.05fr]" onSubmit={handleSubmit}>
        <section className="rounded-xl border border-border bg-panel shadow-soft">
          <div className="border-b border-border/80 px-4 py-3">
            <div className="flex items-center gap-2">
              <FileText className="h-4 w-4 text-muted" />
              <p className="text-[13px] font-semibold text-ink">工單資訊</p>
            </div>
          </div>

          <div className="grid gap-4 px-4 py-4">
            <label className="flex flex-col gap-1.5">
              <span className="text-[12px] font-semibold text-ink">
                標題 <span className="text-danger">*</span>
              </span>
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
              <ScrollText className="h-4 w-4 text-muted" />
              <p className="text-[13px] font-semibold text-ink">
                SQL 內容 <span className="text-danger">*</span>
              </p>
            </div>
          </div>

          <label className="flex h-full flex-col gap-1.5 px-4 py-4">
            <span className="sr-only">SQL 內容</span>
            <textarea
              value={sqlContent}
              onChange={(event) => setSqlContent(event.target.value)}
              className="min-h-[430px] flex-1 rounded-xl border border-border bg-panel-soft px-4 py-4 font-mono text-[13px] leading-7 text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
              placeholder={'ALTER TABLE ...;\nUPDATE ...;'}
              disabled={submitting}
            />
          </label>

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
