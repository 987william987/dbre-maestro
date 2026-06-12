import { useEffect, useMemo, useState } from 'react'
import { createPortal } from 'react-dom'
import { ChevronDown, Download, Search, X } from 'lucide-react'
import { ApiError } from '@/shared/api/client'
import { useAuth } from '@/shared/auth/AuthContext'
import { formatDateTime } from '@/shared/lib/format'
import type { AuditLog } from '@/shared/types/audit'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { exportAuditLogs, listAuditLogs } from '@/modules/audit/api'
import { useToast } from '@/shared/ui/ToastContext'

const PAGE_SIZE = 20

const ACTION_OPTIONS = [
  { value: '', label: '全部動作' },
  { value: 'login', label: '登入' },
  { value: 'logout', label: '登出' },
  { value: 'setting_change', label: '設定變更' },
  { value: 'user_create', label: '建立使用者' },
  { value: 'user_update', label: '更新使用者' },
  { value: 'user_delete', label: '刪除使用者' },
  { value: 'user_membership_add', label: '加入使用者群組' },
  { value: 'user_membership_remove', label: '移除使用者群組' },
  { value: 'user_permission_add', label: '新增使用者權限' },
  { value: 'ticket_submit', label: '建立工單' },
  { value: 'ticket_approve', label: '核准工單' },
  { value: 'ticket_reject', label: '駁回工單' },
  { value: 'ticket_request_execution', label: '申請執行工單' },
  { value: 'ticket_execute_start', label: '開始執行工單' },
  { value: 'ticket_execute_complete', label: '工單執行完成' },
  { value: 'ticket_execute_failed', label: '工單執行失敗' },
  { value: 'ticket_schedule', label: '排程執行工單' },
  { value: 'ticket_stop', label: '停止工單' },
  { value: 'query_execute', label: '執行查詢' },
  { value: 'export_create', label: '建立匯出申請' },
  { value: 'export_approve', label: '核准匯出申請' },
  { value: 'export_reject', label: '駁回匯出申請' },
  { value: 'export_download', label: '下載匯出檔案' },
  { value: 'audit_export', label: '匯出稽核日誌' },
  { value: 'notification_failure', label: '通知發送失敗' },
] as const

const RESOURCE_OPTIONS = [
  { value: '', label: '全部資源' },
  { value: 'db_connection', label: '資料庫連線' },
  { value: 'ticket', label: '工單' },
  { value: 'user', label: '使用者' },
  { value: 'export', label: '匯出申請' },
  { value: 'audit_log', label: '稽核日誌' },
] as const

export function AuditLogsPage() {
  const { user } = useAuth()
  const { pushToast } = useToast()
  const [logs, setLogs] = useState<AuditLog[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [filters, setFilters] = useState({
    actionType: '',
    actorKeyword: '',
    resourceType: '',
    resourceKeyword: '',
    from: '',
    to: '',
  })
  const [offset, setOffset] = useState(0)
  const [selectedLog, setSelectedLog] = useState<AuditLog | null>(null)
  const canExport = user?.permissions.includes('audit_logs.write') ?? false

  async function loadLogs(nextOffset: number) {
    setLoading(true)
    setError('')
    try {
      const response = await listAuditLogs({
        actionType: filters.actionType,
        actorName: filters.actorKeyword.trim(),
        resourceType: filters.resourceType,
        resourceName: filters.resourceKeyword.trim(),
        from: toRFC3339(filters.from),
        to: toRFC3339(filters.to),
        offset: nextOffset,
        limit: PAGE_SIZE,
      })
      setLogs(response.logs)
      setTotal(response.total)
    } catch (loadError) {
      setError(loadError instanceof ApiError ? loadError.message : '讀取稽核日誌失敗。')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void loadLogs(offset)
  }, [offset])

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setOffset(0)
    await loadLogs(0)
  }

  const filterHint = useMemo(() => {
    const active: string[] = []
    if (filters.actionType) active.push(`動作：${formatActionType(filters.actionType)}`)
    if (filters.resourceType) active.push(`資源：${formatResourceType(filters.resourceType)}`)
    if (filters.actorKeyword) active.push(`操作人：${filters.actorKeyword}`)
    if (filters.resourceKeyword) active.push(`資源名稱：${filters.resourceKeyword}`)
    if (filters.from || filters.to) active.push(`時間區間：${formatDateFilter(filters.from) || '不限'} 至 ${formatDateFilter(filters.to) || '不限'}`)
    return active
  }, [filters])

  async function handleExport() {
    try {
      const response = await exportAuditLogs({
        actionType: filters.actionType,
        actorName: filters.actorKeyword.trim(),
        resourceType: filters.resourceType,
        resourceName: filters.resourceKeyword.trim(),
        from: toRFC3339(filters.from),
        to: toRFC3339(filters.to),
      })
      const blob = await response.blob()
      const url = window.URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = getDownloadFilename(response.headers.get('content-disposition')) ?? 'audit-logs.csv'
      document.body.appendChild(anchor)
      anchor.click()
      document.body.removeChild(anchor)
      window.URL.revokeObjectURL(url)
      pushToast('稽核日誌匯出已開始', 'success', { placement: 'center' })
    } catch (exportError) {
      pushToast(exportError instanceof ApiError ? exportError.message : '匯出稽核日誌失敗', 'error', { placement: 'center', durationMs: 3600 })
    }
  }

  return (
    <div className="flex h-full flex-col gap-3 p-3 sm:p-4">
      <section className="rounded-xl border border-border bg-panel-soft shadow-soft">
        <div className="border-b border-border/80 px-4 py-3 sm:px-5">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
            <div className="max-w-3xl">
              <div className="flex flex-wrap items-center gap-2 text-[11px] font-semibold text-muted">
                <span className="rounded-full border border-border bg-white px-2.5 py-1 text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                  Audit
                </span>
                <span>/</span>
                <span>Event Timeline</span>
              </div>
              <h2 className="mt-3 text-[24px] font-bold tracking-[-0.03em] text-ink">稽核日誌</h2>
              <p className="mt-2 text-[13px] leading-6 text-muted">
                查看登入、工單、匯出與設定變更等操作紀錄。常用條件已整理成可選項，複雜明細改由詳情視窗查看。
              </p>
            </div>

            <div className="rounded-lg border border-border bg-white px-3 py-2.5 text-[12px] text-muted shadow-soft">
              <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">Total Events</p>
              <p className="mt-1 text-[20px] font-bold tracking-tight text-ink">{total}</p>
            </div>
          </div>
        </div>

        <form className="px-4 py-3 sm:px-5" onSubmit={handleSubmit}>
          <div className="flex flex-wrap items-center gap-2">
            <FilterHint
              hint="從清單挑選常見操作事件，例如登入、登出、設定變更。"
              className="min-w-[160px] max-w-[220px] flex-1"
            >
              <SelectField
                value={filters.actionType}
                onChange={(value) => setFilters((current) => ({ ...current, actionType: value }))}
                options={ACTION_OPTIONS}
              />
            </FilterHint>
            <FilterHint
              hint="先用資源類型收斂，例如資料庫連線、工單或使用者。"
              className="min-w-[160px] max-w-[220px] flex-1"
            >
              <SelectField
                value={filters.resourceType}
                onChange={(value) => setFilters((current) => ({ ...current, resourceType: value }))}
                options={RESOURCE_OPTIONS}
              />
            </FilterHint>
            <FilterHint
              hint="可輸入操作人名稱關鍵字，例如 admin 或 william。"
              className="min-w-[160px] flex-1"
            >
              <input
                value={filters.actorKeyword}
                onChange={(event) => setFilters((current) => ({ ...current, actorKeyword: event.target.value }))}
                className="h-10 w-full rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                placeholder="操作人名稱"
              />
            </FilterHint>
            <FilterHint
              hint="可輸入資源名稱關鍵字，例如某個連線名稱或工單標題。"
              className="min-w-[180px] flex-1"
            >
              <input
                value={filters.resourceKeyword}
                onChange={(event) => setFilters((current) => ({ ...current, resourceKeyword: event.target.value }))}
                className="h-10 w-full rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                placeholder="資源名稱"
              />
            </FilterHint>
            <FilterHint
              hint="設定開始時間，僅顯示此時間之後的事件。"
              className="min-w-[220px] flex-1"
            >
              <input
                value={filters.from}
                onChange={(event) => setFilters((current) => ({ ...current, from: event.target.value }))}
                type="datetime-local"
                className="h-10 w-full rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
              />
            </FilterHint>
            <FilterHint
              hint="設定結束時間，僅顯示此時間之前的事件。"
              className="min-w-[220px] flex-1"
            >
              <input
                value={filters.to}
                onChange={(event) => setFilters((current) => ({ ...current, to: event.target.value }))}
                type="datetime-local"
                className="h-10 w-full rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
              />
            </FilterHint>
            <button
              type="submit"
              className="inline-flex h-10 shrink-0 items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white transition hover:bg-slate-800"
            >
              <Search className="h-4 w-4" />
              套用
            </button>
            {canExport ? (
              <button
                type="button"
                onClick={handleExport}
                className="inline-flex h-10 shrink-0 items-center justify-center gap-2 rounded-lg border border-border bg-white px-4 text-[13px] font-bold text-ink transition hover:bg-page"
              >
                <Download className="h-4 w-4" />
                匯出
              </button>
            ) : null}
          </div>

          <div className="mt-2 flex flex-wrap items-center gap-2 text-[12px] text-muted">
            {filterHint.length > 0 ? <span>目前篩選：{filterHint.join('、')}</span> : null}
          </div>
        </form>
      </section>

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      <section className="overflow-hidden rounded-xl border border-border bg-panel shadow-soft">
        {loading ? (
          <LoadingBlock message="載入稽核日誌中…" className="h-60 rounded-none border-0" />
        ) : logs.length === 0 ? (
          <div className="flex h-60 items-center justify-center text-sm text-muted">目前沒有符合條件的稽核紀錄。</div>
        ) : (
          <div className="overflow-x-auto">
            <table className="min-w-full border-collapse">
              <thead className="bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                <tr>
                  <th className="px-3 py-3">Created</th>
                  <th className="px-3 py-3">Actor</th>
                  <th className="px-3 py-3">Action</th>
                  <th className="px-3 py-3">Resource</th>
                  <th className="px-3 py-3">IP</th>
                  <th className="px-3 py-3">Details</th>
                </tr>
              </thead>
              <tbody>
                {logs.map((log) => (
                  <tr key={log.id} className="border-t border-border text-sm text-ink transition-colors hover:bg-slate-50/70">
                    <td className="px-3 py-2.5 text-[12px] text-muted whitespace-nowrap">{formatDateTime(log.created_at, true)}</td>
                    <td className="px-3 py-2.5 text-[13px] font-semibold whitespace-nowrap">{formatActor(log)}</td>
                    <td className="px-3 py-2.5 text-[12px] whitespace-nowrap">{formatActionType(log.action_type)}</td>
                    <td className="max-w-[360px] px-3 py-2.5 text-[12px] text-muted">
                      <div className="truncate" title={formatResource(log)}>
                        {formatResource(log)}
                      </div>
                    </td>
                    <td className="px-3 py-2.5 text-[12px] text-muted whitespace-nowrap">{formatIPAddress(log.ip_address)}</td>
                    <td className="px-3 py-2.5">
                      <button
                        type="button"
                        onClick={() => setSelectedLog(log)}
                        className="inline-flex h-8 items-center justify-center rounded-md border border-border bg-panel-soft px-3 text-[12px] font-semibold text-ink transition hover:bg-page"
                      >
                        查看
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <div className="flex items-center justify-between px-1">
        <p className="text-[12px] text-muted">
          共 {total} 筆，目前顯示 {logs.length === 0 ? 0 : offset + 1} - {Math.min(offset + logs.length, total)}
        </p>
        <div className="flex gap-2">
          <button
            type="button"
            disabled={offset === 0}
            onClick={() => setOffset((current) => Math.max(0, current - PAGE_SIZE))}
            className="inline-flex h-9 items-center justify-center rounded-md border border-border bg-panel px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:opacity-50"
          >
            上一頁
          </button>
          <button
            type="button"
            disabled={offset + PAGE_SIZE >= total}
            onClick={() => setOffset((current) => current + PAGE_SIZE)}
            className="inline-flex h-9 items-center justify-center rounded-md border border-border bg-panel px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:opacity-50"
          >
            下一頁
          </button>
        </div>
      </div>

      {selectedLog ? createPortal(
        <div className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-950/28 px-3 py-3 sm:px-4 sm:py-4">
          <div
            role="dialog"
            aria-modal="true"
            aria-labelledby="audit-log-detail-title"
            className="flex max-h-[min(720px,calc(100vh-2rem))] w-full max-w-[760px] flex-col overflow-hidden rounded-xl border border-border bg-panel shadow-[0_22px_60px_rgba(15,23,42,0.18)]"
          >
            <div className="flex items-start justify-between border-b border-border/80 px-5 py-4">
              <div>
                <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">Audit Detail</p>
                <h3 id="audit-log-detail-title" className="mt-1 text-[22px] font-bold tracking-[-0.03em] text-ink">
                  {formatActionType(selectedLog.action_type)}
                </h3>
              </div>
              <button
                type="button"
                onClick={() => setSelectedLog(null)}
                className="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-border bg-panel-soft text-muted transition hover:bg-page hover:text-ink"
                aria-label="關閉"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            <div className="grid gap-4 overflow-y-auto px-5 py-4">
              <section className="grid gap-3 sm:grid-cols-2">
                <InfoBox label="時間" value={formatDateTime(selectedLog.created_at, true)} />
                <InfoBox label="操作人" value={formatActor(selectedLog)} />
                <InfoBox label="資源" value={formatResource(selectedLog)} />
                <InfoBox label="來源 IP" value={formatIPAddress(selectedLog.ip_address)} />
              </section>

              <section className="rounded-xl border border-border bg-panel shadow-soft">
                <div className="border-b border-border/80 px-4 py-3">
                  <p className="text-[13px] font-semibold text-ink">完整明細</p>
                </div>
                <div className="px-4 py-4">
                  <pre className="overflow-x-auto rounded-lg bg-panel-soft px-3 py-3 text-[12px] text-muted">
                    {selectedLog.details ? JSON.stringify(selectedLog.details, null, 2) : '—'}
                  </pre>
                </div>
              </section>
            </div>
          </div>
        </div>,
        document.body,
      ) : null}
    </div>
  )
}

function SelectField({
  value,
  onChange,
  options,
}: {
  value: string
  onChange: (value: string) => void
  options: ReadonlyArray<{ value: string; label: string }>
}) {
  return (
    <div className="relative">
      <select
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="h-10 w-full appearance-none rounded-lg border border-border bg-panel-soft px-3 pr-9 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
      >
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
      <ChevronDown className="pointer-events-none absolute right-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted" />
    </div>
  )
}

function FilterHint({
  hint,
  className,
  children,
}: {
  hint: string
  className?: string
  children: React.ReactNode
}) {
  return (
    <div className={`group relative ${className ?? ''}`}>
      {children}
      <div className="pointer-events-none absolute left-0 top-[calc(100%+8px)] z-20 hidden w-64 rounded-md border border-border bg-white px-3 py-2 text-[11px] font-medium text-muted shadow-soft group-hover:block">
        {hint}
      </div>
    </div>
  )
}

function InfoBox({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-border bg-panel-soft px-3 py-3">
      <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">{label}</p>
      <p className="mt-1 text-[13px] text-ink">{value || '—'}</p>
    </div>
  )
}

function formatActor(log: AuditLog) {
  if (log.actor_name?.trim()) {
    return log.actor_name
  }
  if (log.actor_id) {
    return `使用者 #${log.actor_id}`
  }
  return '系統'
}

function formatActionType(actionType: string) {
  const option = ACTION_OPTIONS.find((item) => item.value === actionType)
  if (option) {
    return option.label
  }
  return actionType
    .split('_')
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}

function formatResourceType(resourceType?: string | null) {
  if (!resourceType) {
    return '未指定資源'
  }
  const option = RESOURCE_OPTIONS.find((item) => item.value === resourceType)
  return option?.label ?? resourceType
}

function formatResource(log: AuditLog) {
  const label = formatResourceType(log.resource_type)
  const detailsRecord = isRecord(log.details) ? log.details : null
  const detailName = typeof detailsRecord?.name === 'string' && detailsRecord.name.trim() ? detailsRecord.name.trim() : ''

  if (detailName) {
    return `${label} · ${detailName}`
  }
  if (label === '未指定資源' && log.resource_id) {
    return `未指定資源 · ${log.resource_id}`
  }
  return label
}

function formatIPAddress(ipAddress?: string | null) {
  return ipAddress?.trim() ? ipAddress : '系統事件'
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function toRFC3339(value: string) {
  if (!value) {
    return ''
  }
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '' : date.toISOString()
}

function formatDateFilter(value: string) {
  if (!value) {
    return ''
  }
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : formatDateTime(date.toISOString(), true)
}

function getDownloadFilename(contentDisposition: string | null) {
  if (!contentDisposition) {
    return null
  }
  const matched = contentDisposition.match(/filename="([^"]+)"/)
  return matched?.[1] ?? null
}
