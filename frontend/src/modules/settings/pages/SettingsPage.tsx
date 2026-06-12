import { useEffect, useState } from 'react'
import { RefreshCcw, Settings2 } from 'lucide-react'
import { getSettings } from '@/modules/settings/api'
import { ApiError } from '@/shared/api/client'
import type { PlatformSettings } from '@/shared/types/settings'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'

export function SettingsPage() {
  const [settings, setSettings] = useState<PlatformSettings | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    let active = true

    async function bootstrap() {
      setLoading(true)
      setError('')
      try {
        const settingsResponse = await getSettings()
        if (active) {
          setSettings(settingsResponse)
        }
      } catch (loadError) {
        if (active) {
          setError(loadError instanceof ApiError ? loadError.message : '讀取平台設定失敗。')
        }
      } finally {
        if (active) {
          setLoading(false)
        }
      }
    }

    void bootstrap()

    return () => {
      active = false
    }
  }, [])

  async function handleRefresh() {
    setLoading(true)
    setError('')
    try {
      const refreshed = await getSettings()
      setSettings(refreshed)
    } catch (loadError) {
      setError(loadError instanceof ApiError ? loadError.message : '重新整理設定失敗。')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex h-full flex-col gap-3 p-3 sm:p-4">
      <section className="rounded-xl border border-border bg-panel-soft shadow-soft">
        <div className="border-b border-border/80 px-4 py-3 sm:px-5">
          <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
            <div className="max-w-3xl">
              <div className="flex flex-wrap items-center gap-2 text-[11px] font-semibold text-muted">
                <span className="rounded-full border border-border bg-white px-2.5 py-1 text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                  Settings
                </span>
                <span>/</span>
                <span>Platform Controls</span>
              </div>
              <h2 className="mt-3 text-[24px] font-bold tracking-[-0.03em] text-ink">平台設定</h2>
              <p className="mt-2 text-[13px] leading-6 text-muted">
                特殊審批人已改回 Users / RBAC 的 permission 模組管理。這一頁保留作為平台層設定入口，不再維護 reviewer 名單。
              </p>
            </div>

            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                onClick={() => void handleRefresh()}
                className="inline-flex h-10 items-center gap-2 rounded-lg border border-border bg-white px-3.5 text-[13px] font-semibold text-ink transition-colors hover:bg-panel-soft"
              >
                <RefreshCcw className="h-4 w-4" />
                重新整理
              </button>
              <div className="inline-flex h-10 items-center gap-2 rounded-lg border border-border bg-white px-4 text-[13px] font-semibold text-muted">
                <Settings2 className="h-4 w-4" />
                Reviewer 改由 RBAC 管理
              </div>
            </div>
          </div>
        </div>
      </section>

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      {loading ? (
        <LoadingBlock message="載入平台設定中…" className="min-h-[320px] rounded-xl border-border bg-panel" />
      ) : (
        <section className="rounded-xl border border-border bg-panel shadow-soft">
          <div className="border-b border-border/80 px-4 py-3">
            <p className="text-[14px] font-semibold text-ink">目前策略</p>
            <p className="mt-1 text-[12px] leading-5 text-muted">
              `sql_export` reviewer 使用 `sql_editor.export_review`；`sensitive_query_access` reviewer 使用 `sql_editor.sensitive_review`。
            </p>
          </div>
          <div className="space-y-3 px-4 py-4 text-[13px] text-muted">
            <p>若要調整審批人，請到 Users / RBAC 工作台修改 user 或 auth group 的 permission。</p>
            <p>目前這頁不需要 `users.read` 或 `users.write` 即可載入，避免跨模組耦合。</p>
            {settings ? (
              <div className="rounded-lg border border-border bg-panel-soft px-3 py-3 text-[12px] text-ink">
                已讀取平台設定物件，共 {Object.keys(settings).length} 個欄位。
              </div>
            ) : null}
          </div>
        </section>
      )}
    </div>
  )
}
