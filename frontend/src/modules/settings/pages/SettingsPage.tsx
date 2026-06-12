import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { RefreshCcw, Save, Settings2 } from 'lucide-react'
import { getSettings, listSettingsDBConnections, patchSettings } from '@/modules/settings/api'
import { ApiError } from '@/shared/api/client'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { PlatformSettings } from '@/shared/types/settings'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { useToast } from '@/shared/ui/ToastContext'

type SettingsForm = {
  inventoryEnabled: boolean
  inventoryRegions: string
  inventoryEngines: string
  inventoryIntervalMinutes: string
  objectEnabled: boolean
  objectConnectionIDs: number[]
  objectIntervalMinutes: string
}

export function SettingsPage() {
  const { pushToast } = useToast()
  const [settings, setSettings] = useState<PlatformSettings | null>(null)
  const [form, setForm] = useState<SettingsForm | null>(null)
  const [connections, setConnections] = useState<Array<Pick<DBConnection, 'id' | 'name' | 'db_type' | 'host' | 'port'>>>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    let active = true

    async function bootstrap() {
      setLoading(true)
      setError('')
      try {
        const [settingsResponse, connectionsResponse] = await Promise.all([
          getSettings(),
          listSettingsDBConnections(),
        ])
        if (active) {
          setSettings(settingsResponse)
          setForm(toForm(settingsResponse))
          setConnections(connectionsResponse.connections)
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
      const [refreshed, connectionsResponse] = await Promise.all([
        getSettings(),
        listSettingsDBConnections(),
      ])
      setSettings(refreshed)
      setForm(toForm(refreshed))
      setConnections(connectionsResponse.connections)
    } catch (loadError) {
      setError(loadError instanceof ApiError ? loadError.message : '重新整理設定失敗。')
    } finally {
      setLoading(false)
    }
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!form) {
      return
    }

    setSaving(true)
    setError('')
    try {
      const payload = toPayload(settings, form)
      const saved = await patchSettings(payload)
      setSettings(saved)
      setForm(toForm(saved))
      pushToast('平台設定已更新', 'success')
    } catch (saveError) {
      setError(saveError instanceof ApiError ? saveError.message : '更新平台設定失敗。')
    } finally {
      setSaving(false)
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
                <span>DB Metadata Controls</span>
              </div>
              <h2 className="mt-3 text-[24px] font-bold tracking-[-0.03em] text-ink">平台設定</h2>
              <p className="mt-2 text-[13px] leading-6 text-muted">
                這一頁目前收斂為 `DB Metadata` 掃描設定。AWS 帳號權限仍依賴 runtime IAM role；DB 帳密仍由 DB Connections 管理。
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
                Metadata Scope
              </div>
            </div>
          </div>
        </div>
      </section>

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      {loading || !form ? (
        <LoadingBlock message="載入平台設定中…" className="min-h-[320px] rounded-xl border-border bg-panel" />
      ) : (
        <form onSubmit={handleSubmit} className="grid gap-3">
          <section className="rounded-xl border border-border bg-panel shadow-soft">
            <div className="border-b border-border/80 px-4 py-3">
              <p className="text-[14px] font-semibold text-ink">Inventory 掃描</p>
              <p className="mt-1 text-[12px] leading-5 text-muted">每五分鐘左右從 AWS API 掃一次雲端實例總覽，不做即時 status。</p>
            </div>
            <div className="grid gap-4 px-4 py-4 md:grid-cols-2">
              <label className="flex items-center gap-2 text-[13px] font-medium text-ink">
                <input
                  type="checkbox"
                  checked={form.inventoryEnabled}
                  onChange={(event) => setForm((current) => current ? { ...current, inventoryEnabled: event.target.checked } : current)}
                />
                啟用 inventory 掃描
              </label>
              <Field
                label="掃描間隔（分鐘）"
                value={form.inventoryIntervalMinutes}
                onChange={(value) => setForm((current) => current ? { ...current, inventoryIntervalMinutes: value } : current)}
              />
              <Field
                label="掃描 regions"
                value={form.inventoryRegions}
                onChange={(value) => setForm((current) => current ? { ...current, inventoryRegions: value } : current)}
                placeholder="ap-northeast-1, ap-southeast-1"
              />
              <Field
                label="掃描 engines"
                value={form.inventoryEngines}
                onChange={(value) => setForm((current) => current ? { ...current, inventoryEngines: value } : current)}
                placeholder="aurora-mysql, aurora-postgresql, redis"
              />
            </div>
          </section>

          <section className="rounded-xl border border-border bg-panel shadow-soft">
            <div className="border-b border-border/80 px-4 py-3">
              <p className="text-[14px] font-semibold text-ink">Object 掃描</p>
              <p className="mt-1 text-[12px] leading-5 text-muted">每小時掃描一次物件快照，僅針對 settings 指定的 connection IDs。</p>
            </div>
            <div className="grid gap-4 px-4 py-4 md:grid-cols-2">
              <label className="flex items-center gap-2 text-[13px] font-medium text-ink">
                <input
                  type="checkbox"
                  checked={form.objectEnabled}
                  onChange={(event) => setForm((current) => current ? { ...current, objectEnabled: event.target.checked } : current)}
                />
                啟用 object 掃描
              </label>
              <Field
                label="掃描間隔（分鐘）"
                value={form.objectIntervalMinutes}
                onChange={(value) => setForm((current) => current ? { ...current, objectIntervalMinutes: value } : current)}
              />
            </div>
            <div className="border-t border-border/80 px-4 py-4">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <p className="text-[13px] font-semibold text-ink">允許掃描的 DB Connections</p>
                  <p className="mt-1 text-[12px] leading-5 text-muted">直接勾選要納入 object snapshot 的實例，不再靠手打 connection ID。</p>
                </div>
                <div className="rounded-full border border-border bg-panel-soft px-3 py-1 text-[11px] font-semibold text-muted">
                  已選 {form.objectConnectionIDs.length} 筆
                </div>
              </div>

              {connections.length === 0 ? (
                <div className="mt-4 rounded-lg border border-dashed border-border bg-panel-soft px-4 py-4 text-[12px] text-muted">
                  目前沒有可選的 DB Connection。
                </div>
              ) : (
                <div className="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-3">
                  {connections.map((connection) => {
                    const checked = form.objectConnectionIDs.includes(connection.id)
                    return (
                      <label
                        key={connection.id}
                        className={`flex cursor-pointer items-start gap-3 rounded-xl border px-3 py-3 transition ${
                          checked ? 'border-slate-300 bg-slate-50' : 'border-border bg-white hover:bg-panel-soft'
                        }`}
                      >
                        <input
                          type="checkbox"
                          className="mt-1"
                          checked={checked}
                          onChange={() =>
                            setForm((current) =>
                              current
                                ? {
                                    ...current,
                                    objectConnectionIDs: checked
                                      ? current.objectConnectionIDs.filter((id) => id !== connection.id)
                                      : [...current.objectConnectionIDs, connection.id].sort((left, right) => left - right),
                                  }
                                : current,
                            )
                          }
                        />
                        <div className="min-w-0">
                          <div className="flex flex-wrap items-center gap-2">
                            <span className="text-[13px] font-semibold text-ink">{connection.name}</span>
                            <span className="rounded-full border border-border px-2 py-0.5 text-[10px] font-bold uppercase tracking-[0.12em] text-faint">
                              ID {connection.id}
                            </span>
                          </div>
                          <p className="mt-1 text-[12px] text-muted">{formatDBType(connection.db_type)}</p>
                          <p className="mt-1 break-all font-mono text-[11px] text-muted">
                            {connection.host}:{connection.port}
                          </p>
                        </div>
                      </label>
                    )
                  })}
                </div>
              )}
            </div>
          </section>

          <div className="flex justify-end">
            <button
              type="submit"
              disabled={saving}
              className="inline-flex h-10 items-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition hover:bg-slate-800 disabled:opacity-60"
            >
              <Save className="h-4 w-4" />
              {saving ? '儲存中…' : '儲存設定'}
            </button>
          </div>
        </form>
      )}
    </div>
  )
}

function Field({
  label,
  value,
  onChange,
  placeholder,
}: {
  label: string
  value: string
  onChange: (value: string) => void
  placeholder?: string
}) {
  return (
    <label className="grid gap-2 text-[12px] font-semibold text-muted">
      <span>{label}</span>
      <input
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        className="h-10 rounded-lg border border-border bg-white px-3 text-[13px] text-ink outline-none transition focus:border-slate-400"
      />
    </label>
  )
}

function toForm(settings: PlatformSettings): SettingsForm {
  return {
    inventoryEnabled: settings.db_metadata_inventory_enabled,
    inventoryRegions: settings.db_metadata_inventory_regions.join(', '),
    inventoryEngines: settings.db_metadata_inventory_engines.join(', '),
    inventoryIntervalMinutes: String(settings.db_metadata_inventory_sync_interval_minutes),
    objectEnabled: settings.db_metadata_object_enabled,
    objectConnectionIDs: settings.db_metadata_object_enabled_connection_ids,
    objectIntervalMinutes: String(settings.db_metadata_object_sync_interval_minutes),
  }
}

function toPayload(current: PlatformSettings | null, form: SettingsForm): PlatformSettings {
  return {
    sensitive_export_reviewer_user_ids: current?.sensitive_export_reviewer_user_ids ?? [],
    sensitive_query_access_reviewer_user_ids: current?.sensitive_query_access_reviewer_user_ids ?? [],
    db_metadata_inventory_enabled: form.inventoryEnabled,
    db_metadata_inventory_regions: splitCSV(form.inventoryRegions),
    db_metadata_inventory_engines: splitCSV(form.inventoryEngines),
    db_metadata_inventory_sync_interval_minutes: parsePositiveInt(form.inventoryIntervalMinutes, 5),
    db_metadata_object_enabled: form.objectEnabled,
    db_metadata_object_enabled_connection_ids: form.objectConnectionIDs,
    db_metadata_object_sync_interval_minutes: parsePositiveInt(form.objectIntervalMinutes, 60),
  }
}

function splitCSV(value: string) {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function parsePositiveInt(value: string, fallback: number) {
  const parsed = Number.parseInt(value, 10)
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return fallback
  }
  return parsed
}

function formatDBType(dbType: string) {
  if (dbType === 'postgres') {
    return 'PostgreSQL'
  }
  if (dbType === 'redis') {
    return 'Redis'
  }
  return 'MySQL'
}
