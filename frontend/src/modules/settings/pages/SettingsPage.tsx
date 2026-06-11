import { useEffect, useMemo, useState } from 'react'
import { Loader2, RefreshCcw, Settings2 } from 'lucide-react'
import { getSettings, patchSettings } from '@/modules/settings/api'
import { listUsers } from '@/modules/users/api'
import { ApiError } from '@/shared/api/client'
import { useAuth } from '@/shared/auth/AuthContext'
import type { PlatformSettings } from '@/shared/types/settings'
import type { UserSummary } from '@/shared/types/user'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { useToast } from '@/shared/ui/ToastContext'

const EMPTY_SETTINGS: PlatformSettings = {
  sensitive_export_reviewer_user_ids: [],
  sensitive_query_access_reviewer_user_ids: [],
}

export function SettingsPage() {
  const { user } = useAuth()
  const { pushToast } = useToast()
  const [users, setUsers] = useState<UserSummary[]>([])
  const [settings, setSettings] = useState<PlatformSettings>(EMPTY_SETTINGS)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const canWrite = user?.permissions.includes('settings.write') ?? false

  useEffect(() => {
    let active = true

    async function bootstrap() {
      setLoading(true)
      setError('')
      try {
        const [settingsResponse, usersResponse] = await Promise.all([getSettings(), listUsers()])
        if (!active) {
          return
        }
        setSettings(settingsResponse)
        setUsers(usersResponse.users)
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

  const userOptions = useMemo(
    () => users.map((item) => ({ id: item.id, label: `${item.username} (#${item.id})`, helper: item.email })),
    [users],
  )

  function toggleUser(targetKey: keyof PlatformSettings, userID: number) {
    if (!canWrite) {
      return
    }
    setSettings((current) => {
      const base = current[targetKey]
      const next = base.includes(userID) ? base.filter((item) => item !== userID) : [...base, userID].sort((left, right) => left - right)
      return {
        ...current,
        [targetKey]: next,
      }
    })
  }

  async function handleSave() {
    setSaving(true)
    setError('')
    try {
      const saved = await patchSettings(settings)
      setSettings(saved)
      pushToast('設定已更新', 'success')
    } catch (saveError) {
      setError(saveError instanceof ApiError ? saveError.message : '更新設定失敗。')
    } finally {
      setSaving(false)
    }
  }

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
      <section className="rounded-[22px] border border-white/85 bg-[rgba(248,250,252,0.82)] shadow-soft">
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
                本期先管理 SQL 匯出審批人與臨時敏感查詢審批人名單。只有 `settings.write` 可修改，`settings.read` 僅可查看。
              </p>
            </div>

            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                onClick={() => void handleRefresh()}
                className="inline-flex h-10 items-center gap-2 rounded-[12px] border border-border bg-white px-3.5 text-[13px] font-semibold text-ink transition-colors hover:bg-panel-soft"
              >
                <RefreshCcw className="h-4 w-4" />
                重新整理
              </button>
              <button
                type="button"
                onClick={() => void handleSave()}
                disabled={!canWrite || saving}
                className="inline-flex h-10 items-center gap-2 rounded-[12px] bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition-colors hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Settings2 className="h-4 w-4" />}
                儲存設定
              </button>
            </div>
          </div>
        </div>
      </section>

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      {loading ? (
        <LoadingBlock message="載入平台設定中…" className="min-h-[320px] rounded-[22px] border-white/80 bg-white/86" />
      ) : (
        <div className="grid gap-3 xl:grid-cols-2">
          <SettingsCard
            title="敏感導出審批人"
            description="命中敏感欄位的 SQL EXPORT 工單，由這份名單中的任一人審批。"
            options={userOptions}
            selected={settings.sensitive_export_reviewer_user_ids}
            disabled={!canWrite}
            onToggle={(userID) => toggleUser('sensitive_export_reviewer_user_ids', userID)}
          />
          <SettingsCard
            title="敏感查詢審批人"
            description="Sensitive Access 工單由這份名單中的任一人審批，也可提前撤銷。"
            options={userOptions}
            selected={settings.sensitive_query_access_reviewer_user_ids}
            disabled={!canWrite}
            onToggle={(userID) => toggleUser('sensitive_query_access_reviewer_user_ids', userID)}
          />
        </div>
      )}
    </div>
  )
}

function SettingsCard({
  title,
  description,
  options,
  selected,
  disabled,
  onToggle,
}: {
  title: string
  description: string
  options: Array<{ id: number; label: string; helper: string }>
  selected: number[]
  disabled: boolean
  onToggle: (userID: number) => void
}) {
  return (
    <section className="rounded-[22px] border border-white/85 bg-white/92 shadow-soft">
      <div className="border-b border-border/80 px-4 py-3">
        <p className="text-[14px] font-semibold text-ink">{title}</p>
        <p className="mt-1 text-[12px] leading-5 text-muted">{description}</p>
      </div>
      <div className="max-h-[520px] overflow-y-auto px-4 py-3">
        {options.length === 0 ? (
          <p className="text-[12px] text-muted">目前沒有可配置的使用者。</p>
        ) : (
          <div className="space-y-2">
            {options.map((option) => {
              const checked = selected.includes(option.id)
              return (
                <label
                  key={option.id}
                  className={`flex items-start gap-3 rounded-[14px] border px-3 py-2.5 ${
                    checked ? 'border-[#c7d7fe] bg-[#eef2ff]' : 'border-border bg-white'
                  } ${disabled ? 'cursor-default' : 'cursor-pointer'}`}
                >
                  <input
                    type="checkbox"
                    checked={checked}
                    disabled={disabled}
                    onChange={() => onToggle(option.id)}
                    className="mt-0.5"
                  />
                  <span className="min-w-0">
                    <span className="block text-[13px] font-semibold text-ink">{option.label}</span>
                    <span className="block text-[12px] text-muted">{option.helper}</span>
                  </span>
                </label>
              )
            })}
          </div>
        )}
      </div>
    </section>
  )
}
