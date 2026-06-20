import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Save } from 'lucide-react'
import { getSettings, listSettingsDBConnections, patchSettings } from '@/modules/settings/api'
import { ApiError } from '@/shared/api/client'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { PlatformSettings } from '@/shared/types/settings'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { PageIntro } from '@/shared/ui/PageIntro'
import { Switch } from '@/shared/ui/Switch'
import { useToast } from '@/shared/ui/ToastContext'

type SettingsForm = {
  larkAppID: string
  larkAppSecret: string
  larkAppSecretConfigured: boolean
  requireNonSensitiveExportReview: boolean
  sqlEditorAppTimeoutSeconds: string
  sqlEditorMySQLMaxExecutionTimeMs: string
  sqlEditorPostgresStatementTimeoutMs: string
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
          setError(loadError instanceof ApiError ? loadError.message : 'Failed to load platform settings.')
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
      pushToast('Platform settings updated.', 'success')
    } catch (saveError) {
      setError(saveError instanceof ApiError ? saveError.message : 'Failed to update platform settings.')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
      <PageIntro
        title="Platform Settings"
        description="Manage SQL Editor timeout policy and DB metadata scan settings. AWS access still relies on the runtime IAM role, and database credentials are still managed in DB Connections."
      />

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      {loading || !form ? (
        <LoadingBlock message="Loading platform settings..." className="min-h-[320px] rounded-xl border-border bg-panel" />
      ) : (
        <form onSubmit={handleSubmit} className="grid gap-3">
          <section className="rounded-xl border border-border bg-panel shadow-soft">
            <div className="border-b border-border/80 px-4 py-3">
              <p className="text-[14px] font-semibold text-ink">Lark Notifications</p>
              <p className="mt-1 text-[12px] leading-5 text-muted">Configure Lark app credentials for ticket notifications. Directed delivery uses each user&apos;s configured Lark Open ID. Leave App Secret blank to keep the existing secret.</p>
            </div>
            <div className="grid gap-4 px-4 py-4 md:grid-cols-2">
              <Field
                label="Lark App ID"
                value={form.larkAppID}
                onChange={(value) => setForm((current) => current ? { ...current, larkAppID: value } : current)}
                placeholder="cli_xxxxxxxxxxxxx"
              />
              <Field
                label={`Lark App Secret${form.larkAppSecretConfigured ? ' (Configured)' : ''}`}
                value={form.larkAppSecret}
                onChange={(value) => setForm((current) => current ? { ...current, larkAppSecret: value } : current)}
                placeholder={form.larkAppSecretConfigured ? 'Leave blank to keep existing secret' : 'Enter app secret'}
                type="password"
              />
            </div>
          </section>

          <section className="rounded-xl border border-border bg-panel shadow-soft">
            <div className="border-b border-border/80 px-4 py-3">
              <p className="text-[14px] font-semibold text-ink">SQL Editor Timeout</p>
              <p className="mt-1 text-[12px] leading-5 text-muted">These values apply only to SQL Editor `/api/query`. The app timeout caps the request lifetime, while MySQL and PostgreSQL values are applied at the session level before each query.</p>
            </div>
            <div className="grid gap-4 px-4 py-4 md:grid-cols-3">
              <Field
                label="App timeout (seconds)"
                value={form.sqlEditorAppTimeoutSeconds}
                onChange={(value) => setForm((current) => current ? { ...current, sqlEditorAppTimeoutSeconds: value } : current)}
              />
              <Field
                label="MySQL max_execution_time (ms)"
                value={form.sqlEditorMySQLMaxExecutionTimeMs}
                onChange={(value) => setForm((current) => current ? { ...current, sqlEditorMySQLMaxExecutionTimeMs: value } : current)}
              />
              <Field
                label="PostgreSQL statement_timeout (ms)"
                value={form.sqlEditorPostgresStatementTimeoutMs}
                onChange={(value) => setForm((current) => current ? { ...current, sqlEditorPostgresStatementTimeoutMs: value } : current)}
              />
            </div>
          </section>

          <section className="rounded-xl border border-border bg-panel shadow-soft">
            <div className="border-b border-border/80 px-4 py-3">
              <p className="text-[14px] font-semibold text-ink">Export Approval</p>
              <p className="mt-1 text-[12px] leading-5 text-muted">Sensitive exports always require approval. This setting only controls non-sensitive SQL Editor exports.</p>
            </div>
            <div className="px-4 py-4">
              <label className="flex items-start gap-3 text-[13px] font-medium text-ink">
                <span className="pt-0.5">
                  <Switch
                    ariaLabel="Require approval for non-sensitive exports"
                    checked={form.requireNonSensitiveExportReview}
                    onChange={(checked) => setForm((current) => current ? { ...current, requireNonSensitiveExportReview: checked } : current)}
                  />
                </span>
                <span>
                  <span className="block">Require approval for non-sensitive exports</span>
                  <span className="mt-1 block text-[12px] font-normal leading-5 text-muted">When disabled, non-sensitive exports are auto-approved but still create export tickets for audit records.</span>
                </span>
              </label>
            </div>
          </section>

          <section className="rounded-xl border border-border bg-panel shadow-soft">
            <div className="border-b border-border/80 px-4 py-3">
              <p className="text-[14px] font-semibold text-ink">Inventory Scan</p>
              <p className="mt-1 text-[12px] leading-5 text-muted">Pull a cloud inventory snapshot from AWS APIs on a recurring interval. This view is not intended for real-time status.</p>
            </div>
            <div className="grid gap-4 px-4 py-4 md:grid-cols-2">
              <label className="flex items-center gap-2 text-[13px] font-medium text-ink">
                <Switch
                  ariaLabel="Enable inventory scanning"
                  checked={form.inventoryEnabled}
                  onChange={(checked) => setForm((current) => current ? { ...current, inventoryEnabled: checked } : current)}
                />
                Enable inventory scan
              </label>
              <Field
                label="Sync interval (minutes)"
                value={form.inventoryIntervalMinutes}
                onChange={(value) => setForm((current) => current ? { ...current, inventoryIntervalMinutes: value } : current)}
              />
              <Field
                label="Regions"
                value={form.inventoryRegions}
                onChange={(value) => setForm((current) => current ? { ...current, inventoryRegions: value } : current)}
                placeholder="ap-northeast-1, ap-southeast-1"
              />
              <Field
                label="Engines"
                value={form.inventoryEngines}
                onChange={(value) => setForm((current) => current ? { ...current, inventoryEngines: value } : current)}
                placeholder="aurora-mysql, aurora-postgresql, redis"
              />
            </div>
          </section>

          <section className="rounded-xl border border-border bg-panel shadow-soft">
            <div className="border-b border-border/80 px-4 py-3">
              <p className="text-[14px] font-semibold text-ink">Object Scan</p>
              <p className="mt-1 text-[12px] leading-5 text-muted">Capture object snapshots on a recurring interval for the selected DB connections.</p>
            </div>
            <div className="grid gap-4 px-4 py-4 md:grid-cols-2">
              <label className="flex items-center gap-2 text-[13px] font-medium text-ink">
                <Switch
                  ariaLabel="Enable object scanning"
                  checked={form.objectEnabled}
                  onChange={(checked) => setForm((current) => current ? { ...current, objectEnabled: checked } : current)}
                />
                Enable object scan
              </label>
              <Field
                label="Sync interval (minutes)"
                value={form.objectIntervalMinutes}
                onChange={(value) => setForm((current) => current ? { ...current, objectIntervalMinutes: value } : current)}
              />
            </div>
            <div className="border-t border-border/80 px-4 py-4">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <p className="text-[13px] font-semibold text-ink">Included DB Connections</p>
                  <p className="mt-1 text-[12px] leading-5 text-muted">Choose which connections should be included in object snapshots.</p>
                </div>
                <div className="rounded-full border border-border bg-panel-soft px-3 py-1 text-[11px] font-semibold text-muted">
                  {form.objectConnectionIDs.length} selected
                </div>
              </div>

              {connections.length === 0 ? (
                <div className="mt-4 rounded-lg border border-dashed border-border bg-panel-soft px-4 py-4 text-[12px] text-muted">
                  No DB connections are available for selection.
                </div>
              ) : (
                <div className="mt-5 grid gap-4 md:grid-cols-2 xl:grid-cols-3">
                  {connections.map((connection) => {
                    const checked = form.objectConnectionIDs.includes(connection.id)
                    return (
                      <label
                        key={connection.id}
                        className={`flex cursor-pointer items-start gap-4 rounded-xl border px-4 py-4 transition ${
                          checked ? 'border-slate-300 bg-slate-50' : 'border-border bg-white hover:bg-panel-soft'
                        }`}
                      >
                        <div className="pt-1">
                          <Switch
                            ariaLabel={`${connection.name} selected for object scan`}
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
                        </div>
                        <div className="min-w-0">
                          <div className="flex flex-wrap items-center gap-2">
                            <p className="text-[13px] font-semibold leading-6 text-ink">{connection.name}</p>
                            <span className="rounded-full border border-border bg-panel-soft px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted">
                              {formatDBType(connection.db_type)}
                            </span>
                          </div>
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
              className="inline-flex h-10 items-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-60"
            >
              <Save className="h-4 w-4" />
              {saving ? 'Saving...' : 'Save Settings'}
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
  type = 'text',
}: {
  label: string
  value: string
  onChange: (value: string) => void
  placeholder?: string
  type?: string
}) {
  return (
    <label className="grid gap-2 text-[12px] font-semibold text-muted">
      <span>{label}</span>
      <input
        type={type}
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
    larkAppID: settings.lark_app_id,
    larkAppSecret: '',
    larkAppSecretConfigured: settings.lark_app_secret_configured,
    requireNonSensitiveExportReview: settings.require_non_sensitive_export_review,
    sqlEditorAppTimeoutSeconds: String(settings.sql_editor_app_timeout_seconds),
    sqlEditorMySQLMaxExecutionTimeMs: String(settings.sql_editor_mysql_max_execution_time_ms),
    sqlEditorPostgresStatementTimeoutMs: String(settings.sql_editor_postgres_statement_timeout_ms),
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
    require_non_sensitive_export_review: form.requireNonSensitiveExportReview,
    lark_app_id: form.larkAppID.trim(),
    lark_app_secret: form.larkAppSecret,
    lark_app_secret_configured: form.larkAppSecretConfigured,
    sql_editor_app_timeout_seconds: parsePositiveInt(form.sqlEditorAppTimeoutSeconds, 30),
    sql_editor_mysql_max_execution_time_ms: parsePositiveInt(form.sqlEditorMySQLMaxExecutionTimeMs, 25000),
    sql_editor_postgres_statement_timeout_ms: parsePositiveInt(form.sqlEditorPostgresStatementTimeoutMs, 25000),
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
