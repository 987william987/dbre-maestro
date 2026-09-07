import { useEffect, useMemo, useState } from 'react'
import { Navigate, useParams } from 'react-router-dom'
import { getDBConnectionOverview, listDBConnectionAccounts, listDBConnectionDatabases, type DBAccountGrantSnapshot, type DBAccountScanStatus, type DBAccountSnapshot, type DBDatabaseSummary } from '@/modules/db-connections/api'
import { ApiError } from '@/shared/api/client'
import { useAuth } from '@/shared/auth/AuthContext'
import { formatDateTime } from '@/shared/lib/format'
import type { DBConnection } from '@/shared/types/dbConnection'
import { DataTable, DataTableBody, DataTableCell, DataTableHead, DataTableHeaderCell, DataTableRow, DataTableScroll, DataTableSurface } from '@/shared/ui/DataTable'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { PageTabs } from '@/shared/ui/PageTabs'

type DetailView = 'overview' | 'databases' | 'accounts'

const VIEW_PERMISSIONS: Record<DetailView, string> = {
  overview: 'db_connections.overview',
  databases: 'db_connections.databases',
  accounts: 'db_connections.accounts',
}

export function DBConnectionDetailPage({ view }: { view: DetailView }) {
  const { id } = useParams()
  const connectionID = Number(id)
  const { user } = useAuth()
  const [connection, setConnection] = useState<DBConnection | null>(null)
  const [databases, setDatabases] = useState<DBDatabaseSummary[]>([])
  const [accounts, setAccounts] = useState<DBAccountSnapshot[]>([])
  const [grants, setGrants] = useState<DBAccountGrantSnapshot[]>([])
  const [scanStatus, setScanStatus] = useState<DBAccountScanStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const tabs = useMemo(() => (Object.keys(VIEW_PERMISSIONS) as DetailView[])
    .filter((item) => user?.permissions.includes(VIEW_PERMISSIONS[item]))
    .map((item) => ({ key: item, label: item[0].toUpperCase() + item.slice(1), to: `/db-connections/${connectionID}/${item}` })), [connectionID, user?.permissions])

  useEffect(() => {
    if (!Number.isInteger(connectionID) || connectionID <= 0) { setError('Invalid database connection.'); setLoading(false); return }
    setLoading(true)
    setError('')
    const request = view === 'overview'
      ? getDBConnectionOverview(connectionID).then(setConnection)
      : view === 'databases'
        ? listDBConnectionDatabases(connectionID).then((response) => { setConnection(response.connection); setDatabases(response.items) })
        : listDBConnectionAccounts(connectionID).then((response) => { setConnection(response.connection); setAccounts(response.accounts); setGrants(response.grants); setScanStatus(response.scan_status ?? null) })
    void request.catch((cause) => setError(cause instanceof ApiError ? cause.message : 'Failed to load database connection details.')).finally(() => setLoading(false))
  }, [connectionID, view])

  if (!user?.permissions.includes(VIEW_PERMISSIONS[view])) return <Navigate to="/db-connections" replace />

  return (
    <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
      <div>
        <p className="text-[12px] text-muted">DB Connections / {connection?.name ?? `#${connectionID}`}</p>
        <h2 className="mt-1 text-[18px] font-semibold text-ink">{connection?.name ?? 'Connection Detail'}</h2>
      </div>
      <PageTabs items={tabs} />
      {error ? <InlineAlert>{error}</InlineAlert> : null}
      {loading ? <LoadingBlock message="Loading connection details..." className="min-h-[280px] rounded-xl" /> : null}
      {!loading && connection && view === 'overview' ? <Overview connection={connection} /> : null}
      {!loading && view === 'databases' ? <Databases items={databases} /> : null}
      {!loading && view === 'accounts' ? <Accounts engine={connection?.db_type} accounts={accounts} grants={grants} status={scanStatus} /> : null}
    </div>
  )
}

function Overview({ connection }: { connection: DBConnection }) {
  const credentialRoles = connection.credentials?.map((credential) => `${credential.credential_role} (${credential.username})`).join(', ') || 'Not configured'
  const rows = [
    ['Engine', connection.db_type], ['Readonly endpoint', `${connection.readonly_host || connection.host}:${connection.readonly_port || connection.port}`],
    ['Readwrite endpoint', `${connection.readwrite_host || connection.host}:${connection.readwrite_port || connection.port}`], ['SSL mode', connection.ssl_mode],
    ['Credential roles', credentialRoles],
    ['Last test', connection.last_test_status || 'Not tested'], ['Last tested at', connection.last_tested_at ? formatDateTime(connection.last_tested_at) : 'Never'],
  ]
  return <div className="grid gap-3 md:grid-cols-2">{rows.map(([label, value]) => <div key={label} className="rounded-lg border border-border bg-panel px-4 py-3"><p className="text-[11px] font-medium uppercase text-muted">{label}</p><p className="mt-1 break-all text-[13px] font-semibold text-ink">{value}</p></div>)}</div>
}

function Databases({ items }: { items: DBDatabaseSummary[] }) {
  return <DataTableSurface>{items.length === 0 ? <Empty text="No database snapshot is available for this connection." /> : <DataTableScroll><DataTable><DataTableHead><tr><DataTableHeaderCell>Database</DataTableHeaderCell><DataTableHeaderCell>Tables</DataTableHeaderCell><DataTableHeaderCell>Data Size</DataTableHeaderCell><DataTableHeaderCell>Index Size</DataTableHeaderCell><DataTableHeaderCell>Character Set</DataTableHeaderCell><DataTableHeaderCell>Collation</DataTableHeaderCell><DataTableHeaderCell>Snapshot</DataTableHeaderCell></tr></DataTableHead><DataTableBody>{items.map((item) => <DataTableRow key={item.database_name}><DataTableCell>{item.database_name}</DataTableCell><DataTableCell>{item.table_count}</DataTableCell><DataTableCell>{formatBytes(item.data_size_bytes)}</DataTableCell><DataTableCell>{formatBytes(item.index_size_bytes)}</DataTableCell><DataTableCell>{item.character_set_name || '—'}</DataTableCell><DataTableCell>{item.collation_name || '—'}</DataTableCell><DataTableCell>{formatDateTime(item.snapshot_at)}</DataTableCell></DataTableRow>)}</DataTableBody></DataTable></DataTableScroll>}</DataTableSurface>
}

function Accounts({ engine, accounts, grants, status }: { engine?: string; accounts: DBAccountSnapshot[]; grants: DBAccountGrantSnapshot[]; status: DBAccountScanStatus | null }) {
  const grantsByPrincipal = useMemo(() => grants.reduce<Record<string, DBAccountGrantSnapshot[]>>((result, grant) => { (result[grant.principal_key] ??= []).push(grant); return result }, {}), [grants])
  const isPostgres = engine === 'postgres' || engine === 'postgresql'
  const visibleAccounts = isPostgres ? accounts.filter((account) => !account.principal_name.startsWith('pg_')) : accounts
  return <div className="grid gap-3">{status?.status === 'failed' ? <InlineAlert>Last account scan failed: {status.error_message || 'Insufficient catalog privileges.'}</InlineAlert> : null}<DataTableSurface>{visibleAccounts.length === 0 ? <Empty text="No account snapshot is available for this connection." /> : <DataTableScroll><DataTable><DataTableHead><tr><DataTableHeaderCell>Account</DataTableHeaderCell><DataTableHeaderCell>Type</DataTableHeaderCell><DataTableHeaderCell>Login</DataTableHeaderCell><DataTableHeaderCell>Superuser</DataTableHeaderCell><DataTableHeaderCell>Attributes</DataTableHeaderCell><DataTableHeaderCell>Valid Until</DataTableHeaderCell><DataTableHeaderCell>Grants</DataTableHeaderCell></tr></DataTableHead><DataTableBody>{visibleAccounts.map((account) => <DataTableRow key={account.principal_key}><DataTableCell><span className="font-medium">{account.principal_name}</span>{account.principal_host ? <span className="text-muted">@{account.principal_host}</span> : null}</DataTableCell><DataTableCell>{account.principal_type}</DataTableCell><DataTableCell>{account.can_login && !account.is_locked ? 'Can login' : 'Cannot login'}</DataTableCell><DataTableCell>{account.is_superuser ? 'Yes' : 'No'}</DataTableCell><DataTableCell>{isPostgres ? formatPostgresRoleAttributes(account) : '—'}</DataTableCell><DataTableCell>{account.valid_until ? formatDateTime(account.valid_until) : isPostgres ? 'Infinity' : '—'}</DataTableCell><DataTableCell><GrantList engine={engine} grants={grantsByPrincipal[account.principal_key] ?? []} /></DataTableCell></DataTableRow>)}</DataTableBody></DataTable></DataTableScroll>}</DataTableSurface></div>
}

function formatPostgresRoleAttributes(account: DBAccountSnapshot) {
  const attributes = []
  if (!account.inherits_roles) attributes.push('No inheritance')
  if (account.can_create_role) attributes.push('Create role')
  if (account.can_create_database) attributes.push('Create DB')
  if (account.can_replicate) attributes.push('Replication')
  if (account.can_bypass_rls) attributes.push('Bypass RLS')
  return attributes.join(', ') || '—'
}

function GrantList({ engine, grants }: { engine?: string; grants: DBAccountGrantSnapshot[] }) {
  if (grants.length === 0) return <span className="text-muted">No visible grants</span>
  const nativeStatements = grants.filter((grant) => grant.grant_statement)
  if (engine === 'mysql' && nativeStatements.length === 0) return <span className="text-muted">Awaiting native grant snapshot</span>
  const visibleGrants = nativeStatements.length > 0 ? nativeStatements : grants
  return <div className="max-w-[720px] space-y-2 font-mono text-[11px] leading-5">{visibleGrants.map((grant, index) => <p key={`${grant.grant_kind}-${index}`} className="break-words">{grant.grant_statement || (grant.grant_kind === 'role_membership' ? `GRANT ${grant.granted_role} TO ${grant.principal_key}${grant.is_grantable ? ' WITH ADMIN OPTION' : ''}` : `GRANT ${grant.privilege_type} ON ${[grant.database_name, grant.schema_name, grant.object_name].filter(Boolean).join('.') || grant.scope_type} TO ${grant.principal_key}${grant.is_grantable ? ' WITH GRANT OPTION' : ''}`)}</p>)}</div>
}

function Empty({ text }: { text: string }) { return <div className="m-4 flex min-h-48 items-center justify-center rounded-lg border border-dashed border-border bg-panel-soft text-[13px] text-muted">{text}</div> }
function formatBytes(bytes: number) { if (bytes <= 0) return '0 B'; const units = ['B', 'KB', 'MB', 'GB', 'TB']; const power = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1); return `${(bytes / 1024 ** power).toFixed(power === 0 ? 0 : 2)} ${units[power]}` }
