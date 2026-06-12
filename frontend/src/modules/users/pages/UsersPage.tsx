import { useEffect, useMemo, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { Database, Loader2, Shield, Trash2, UserPlus, Users as UsersIcon, X } from 'lucide-react'
import { createAuthGroup, deleteAuthGroup, getAuthGroup, listAuthGroups, patchAuthGroup } from '@/modules/auth-groups/api'
import { listDBConnections } from '@/modules/db-connections/api'
import { createUser, deleteUser, getUser, listUsers, patchUser } from '@/modules/users/api'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import type { AuthGroup } from '@/shared/types/auth'
import type { AuthGroupDetail } from '@/shared/types/authGroup'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { UserDetail, UserSummary } from '@/shared/types/user'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { useToast } from '@/shared/ui/ToastContext'

type PermissionOption = {
  key: string
  module: string
  action: string
  label: string
  description: string
}

const PERMISSION_METADATA: PermissionOption[] = [
  { key: 'users.read', module: 'Users', action: 'Read', label: 'Users Read', description: '查看 Users / RBAC 工作台內容。' },
  { key: 'users.write', module: 'Users', action: 'Write', label: 'Users Write', description: '管理 user 與 auth group。' },
  { key: 'audit_logs.read', module: 'Audit Logs', action: 'Read', label: 'Audit Logs Read', description: '查看稽核記錄頁面。' },
  { key: 'audit_logs.write', module: 'Audit Logs', action: 'Write', label: 'Audit Logs Write', description: '匯出 audit logs 報表。' },
  { key: 'settings.read', module: 'Settings', action: 'Read', label: 'Settings Read', description: '查看平台設定頁面。' },
  { key: 'settings.write', module: 'Settings', action: 'Write', label: 'Settings Write', description: '修改平台設定。' },
  { key: 'db_connections.read', module: 'DB Connections', action: 'Read', label: 'DB Connections Read', description: '查看資料庫連線清單。' },
  { key: 'db_connections.write', module: 'DB Connections', action: 'Write', label: 'DB Connections Write', description: '新增、修改、刪除資料庫連線。' },
  { key: 'masking_rules.read', module: 'Masking Rules', action: 'Read', label: 'Masking Rules Read', description: '查看遮罩規則與 whitelist。' },
  { key: 'masking_rules.write', module: 'Masking Rules', action: 'Write', label: 'Masking Rules Write', description: '管理遮罩規則與 whitelist。' },
  { key: 'sql_review.read', module: 'SQL Review', action: 'Read', label: 'SQL Review Read', description: '查看 SQL Review 規則。' },
  { key: 'sql_review.write', module: 'SQL Review', action: 'Write', label: 'SQL Review Write', description: '管理 SQL Review 規則。' },
  { key: 'tickets.apply', module: 'Tickets', action: 'Apply', label: 'Tickets Apply', description: '建立 DDL / DML 工單。' },
  { key: 'tickets.review', module: 'Tickets', action: 'Review', label: 'Tickets Review', description: '審批 DDL / DML 工單。' },
  { key: 'tickets.execute', module: 'Tickets', action: 'Execute', label: 'Tickets Execute', description: '執行 DDL / DML 工單。' },
  { key: 'sql_editor.query', module: 'SQL Editor', action: 'Query', label: 'SQL Editor Query', description: '使用 SQL Editor 查詢資料。' },
  { key: 'sql_editor.export', module: 'SQL Editor', action: 'Export', label: 'SQL Editor Export', description: '匯出當前查詢結果。' },
  { key: 'sql_editor.export_review', module: 'SQL Editor', action: 'Export Review', label: 'Export Review', description: '審批 SQL 匯出工單。' },
  { key: 'sql_editor.sensitive_apply', module: 'SQL Editor', action: 'Sensitive Apply', label: 'Sensitive Apply', description: '申請臨時敏感資料查看。' },
  { key: 'sql_editor.sensitive_review', module: 'SQL Editor', action: 'Sensitive Review', label: 'Sensitive Review', description: '審批與撤銷敏感資料查看工單。' },
  { key: 'sql_editor.sensitive_execute', module: 'SQL Editor', action: 'Sensitive Execute', label: 'Sensitive Execute', description: '執行敏感資料查看工單。' },
  { key: 'global.sensitive', module: 'Global', action: 'Sensitive', label: 'Global Sensitive', description: '永久繞過遮罩規則查看敏感資料。' },
]

const PERMISSION_INDEX = new Map(PERMISSION_METADATA.map((item) => [item.key, item] as const))

type ViewMode = 'users' | 'auth-groups'

type DrawerState =
  | { mode: 'create-user' }
  | { mode: 'edit-user'; userId: number }
  | { mode: 'create-auth-group' }
  | { mode: 'edit-auth-group'; authGroupKey: AuthGroup }
  | null

type UserDraft = {
  username: string
  email: string
  password: string
  isActive: boolean
  authGroups: AuthGroup[]
  directPermissions: string[]
  directDBConnectionIDs: number[]
  pendingDelete: boolean
}

type AuthGroupDraft = {
  name: string
  description: string
  userIDs: number[]
  permissions: string[]
  dbConnectionIDs: number[]
  pendingDelete: boolean
}

const EMPTY_USER_DRAFT: UserDraft = {
  username: '',
  email: '',
  password: '',
  isActive: true,
  authGroups: [],
  directPermissions: [],
  directDBConnectionIDs: [],
  pendingDelete: false,
}

const EMPTY_AUTH_GROUP_DRAFT: AuthGroupDraft = {
  name: '',
  description: '',
  userIDs: [],
  permissions: [],
  dbConnectionIDs: [],
  pendingDelete: false,
}

export function UsersPage() {
  const { pushToast } = useToast()
  const [viewMode, setViewMode] = useState<ViewMode>('users')
  const [users, setUsers] = useState<UserSummary[]>([])
  const [authGroups, setAuthGroups] = useState<AuthGroupDetail[]>([])
  const [connections, setConnections] = useState<DBConnection[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [drawerState, setDrawerState] = useState<DrawerState>(null)
  const [drawerLoading, setDrawerLoading] = useState(false)
  const [drawerError, setDrawerError] = useState('')
  const [saving, setSaving] = useState(false)
  const [selectedUser, setSelectedUser] = useState<UserDetail | null>(null)
  const [selectedAuthGroup, setSelectedAuthGroup] = useState<AuthGroupDetail | null>(null)
  const [userDraft, setUserDraft] = useState<UserDraft>(EMPTY_USER_DRAFT)
  const [authGroupDraft, setAuthGroupDraft] = useState<AuthGroupDraft>(EMPTY_AUTH_GROUP_DRAFT)
  const [pendingUserAuthGroup, setPendingUserAuthGroup] = useState<AuthGroup>('')
  const [pendingAuthGroupUserID, setPendingAuthGroupUserID] = useState('')
  const [userPermissionSearch, setUserPermissionSearch] = useState('')
  const [userDBScopeSearch, setUserDBScopeSearch] = useState('')
  const [authGroupPermissionSearch, setAuthGroupPermissionSearch] = useState('')
  const [authGroupDBScopeSearch, setAuthGroupDBScopeSearch] = useState('')

  useEffect(() => {
    void bootstrap()
  }, [])

  async function bootstrap() {
    setLoading(true)
    setError('')
    try {
      const [usersResponse, connectionsResponse, authGroupDetails] = await Promise.all([
        listUsers(),
        listDBConnections(),
        loadAuthGroupDetails(),
      ])
      setUsers(usersResponse.users)
      setConnections(connectionsResponse.connections)
      setAuthGroups(authGroupDetails)
    } catch (loadError) {
      setError(loadError instanceof ApiError ? loadError.message : '載入 RBAC 工作台失敗。')
    } finally {
      setLoading(false)
    }
  }

  async function loadAuthGroupDetails() {
    const summary = await listAuthGroups()
    const details = await Promise.all(summary.auth_groups.map((group) => getAuthGroup(group.name as AuthGroup)))
    return details
  }

  async function reloadAll() {
    const [usersResponse, authGroupDetails] = await Promise.all([listUsers(), loadAuthGroupDetails()])
    setUsers(usersResponse.users)
    setAuthGroups(authGroupDetails)
  }

  const authGroupLabelMap = useMemo(() => {
    return new Map(authGroups.map((group) => [group.name, group.label] as const))
  }, [authGroups])

  const selectedUserIsProtected = selectedUser?.protected === true
  const selectedAuthGroupIsProtected = selectedAuthGroup?.protected === true
  const authGroupOptions = authGroups.map((group) => group.name)

  const availableUserPermissions = useMemo(
    () => filterPermissionCatalog(userPermissionSearch, userDraft.directPermissions),
    [userPermissionSearch, userDraft.directPermissions],
  )
  const availableAuthGroupPermissions = useMemo(
    () => filterPermissionCatalog(authGroupPermissionSearch, authGroupDraft.permissions),
    [authGroupPermissionSearch, authGroupDraft.permissions],
  )
  const availableUserConnections = useMemo(
    () => filterConnections(connections, userDBScopeSearch, userDraft.directDBConnectionIDs),
    [connections, userDBScopeSearch, userDraft.directDBConnectionIDs],
  )
  const availableAuthGroupConnections = useMemo(
    () => filterConnections(connections, authGroupDBScopeSearch, authGroupDraft.dbConnectionIDs),
    [connections, authGroupDBScopeSearch, authGroupDraft.dbConnectionIDs],
  )

  function closeDrawer() {
    setDrawerState(null)
    setDrawerLoading(false)
    setDrawerError('')
    setSaving(false)
    setSelectedUser(null)
    setSelectedAuthGroup(null)
    setUserDraft(EMPTY_USER_DRAFT)
    setAuthGroupDraft(EMPTY_AUTH_GROUP_DRAFT)
    setPendingUserAuthGroup('')
    setPendingAuthGroupUserID('')
    setUserPermissionSearch('')
    setUserDBScopeSearch('')
    setAuthGroupPermissionSearch('')
    setAuthGroupDBScopeSearch('')
  }

  function openCreateUserDrawer() {
    setDrawerState({ mode: 'create-user' })
    setDrawerError('')
    setSelectedUser(null)
    setSelectedAuthGroup(null)
    setUserDraft(EMPTY_USER_DRAFT)
    setPendingUserAuthGroup(authGroupOptions[0] ?? '')
  }

  async function openEditUserDrawer(userId: number) {
    setDrawerState({ mode: 'edit-user', userId })
    setDrawerLoading(true)
    setDrawerError('')
    setSelectedAuthGroup(null)
    try {
      const detail = await getUser(userId)
      setSelectedUser(detail)
      setUserDraft({
        username: detail.username,
        email: detail.email,
        password: '',
        isActive: detail.is_active,
        authGroups: detail.memberships.map((membership) => membership.auth_group),
        directPermissions: detail.direct_permissions ?? [],
        directDBConnectionIDs: detail.direct_db_connection_ids ?? [],
        pendingDelete: false,
      })
      setPendingUserAuthGroup(authGroupOptions.find((group) => !detail.memberships.some((membership) => membership.auth_group === group)) ?? authGroupOptions[0] ?? '')
    } catch (loadError) {
      setDrawerError(loadError instanceof ApiError ? loadError.message : '讀取使用者明細失敗。')
    } finally {
      setDrawerLoading(false)
    }
  }

  function openCreateAuthGroupDrawer() {
    setDrawerState({ mode: 'create-auth-group' })
    setDrawerError('')
    setSelectedUser(null)
    setSelectedAuthGroup(null)
    setAuthGroupDraft(EMPTY_AUTH_GROUP_DRAFT)
    setPendingAuthGroupUserID('')
  }

  async function openEditAuthGroupDrawer(authGroupKey: AuthGroup) {
    setDrawerState({ mode: 'edit-auth-group', authGroupKey })
    setDrawerLoading(true)
    setDrawerError('')
    setSelectedUser(null)
    try {
      const detail = await getAuthGroup(authGroupKey)
      setSelectedAuthGroup(detail)
      setAuthGroupDraft({
        name: detail.label,
        description: detail.description,
        userIDs: detail.users.map((user) => user.id),
        permissions: detail.permissions ?? [],
        dbConnectionIDs: detail.db_connection_ids ?? [],
        pendingDelete: false,
      })
      setPendingAuthGroupUserID('')
    } catch (loadError) {
      setDrawerError(loadError instanceof ApiError ? loadError.message : '讀取 auth group 明細失敗。')
    } finally {
      setDrawerLoading(false)
    }
  }

  function confirmChanges(title: string, lines: string[]) {
    const summary = lines.length > 0 ? lines.map((line) => `- ${line}`).join('\n') : '- 無欄位變更'
    return window.confirm(`${title}\n\n${summary}\n\n是否確認送出？`)
  }

  async function handleSaveUser(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!drawerState) {
      return
    }

    if (drawerState.mode === 'create-user') {
      const createSummary = [
        `Username: ${userDraft.username}`,
        `Email: ${userDraft.email}`,
        `Auth Groups: ${formatAuthGroupList(userDraft.authGroups, authGroupLabelMap)}`,
        `Direct Permissions: ${userDraft.directPermissions.join(', ') || '無'}`,
        `Direct DB Scope: ${formatConnectionIDs(userDraft.directDBConnectionIDs, connections)}`,
      ]
      if (!confirmChanges('建立 User', createSummary)) {
        return
      }

      setSaving(true)
      setDrawerError('')
      try {
        const created = await createUser({
          username: userDraft.username,
          email: userDraft.email,
          password: userDraft.password,
        })
        if (userDraft.authGroups.length > 0 || userDraft.directPermissions.length > 0 || userDraft.directDBConnectionIDs.length > 0) {
          await patchUser(created.id, {
            auth_groups: userDraft.authGroups,
            direct_permissions: userDraft.directPermissions,
            direct_db_connection_ids: userDraft.directDBConnectionIDs,
          })
        }
        await reloadAll()
        pushToast('使用者已建立', 'success')
        await openEditUserDrawer(created.id)
      } catch (saveError) {
        setDrawerError(saveError instanceof ApiError ? saveError.message : '建立使用者失敗。')
      } finally {
        setSaving(false)
      }
      return
    }

    if (drawerState.mode !== 'edit-user' || !selectedUser) {
      return
    }

    const changeSummary = summarizeUserChanges(selectedUser, userDraft, authGroupLabelMap, connections)
    if (changeSummary.length === 0) {
      pushToast('沒有需要儲存的變更', 'success')
      return
    }
    if (!confirmChanges(userDraft.pendingDelete ? '刪除 User' : '儲存 User 變更', changeSummary)) {
      return
    }

    setSaving(true)
    setDrawerError('')
    try {
      if (userDraft.pendingDelete) {
        await deleteUser(drawerState.userId)
        await reloadAll()
        pushToast('使用者已刪除', 'success')
        closeDrawer()
        return
      }

      const payload: {
        username?: string
        email?: string
        password?: string
        is_active?: boolean
        auth_groups?: string[]
        direct_permissions?: string[]
        direct_db_connection_ids?: number[]
      } = {}

      if (selectedUserIsProtected) {
        if (userDraft.password.trim()) {
          payload.password = userDraft.password
        }
      } else {
        payload.username = userDraft.username
        payload.email = userDraft.email
        payload.is_active = userDraft.isActive
        payload.auth_groups = userDraft.authGroups
        payload.direct_permissions = userDraft.directPermissions
        payload.direct_db_connection_ids = userDraft.directDBConnectionIDs
        if (userDraft.password.trim()) {
          payload.password = userDraft.password
        }
      }

      await patchUser(drawerState.userId, payload)
      await reloadAll()
      pushToast('使用者資料已更新', 'success')
      await openEditUserDrawer(drawerState.userId)
    } catch (saveError) {
      setDrawerError(saveError instanceof ApiError ? saveError.message : '更新使用者失敗。')
    } finally {
      setSaving(false)
    }
  }

  async function handleSaveAuthGroup(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!drawerState) {
      return
    }

    if (drawerState.mode === 'create-auth-group') {
      const createSummary = [
        `Name: ${authGroupDraft.name}`,
        `Description: ${authGroupDraft.description || '無'}`,
        `Bound Users: ${formatUserIDs(authGroupDraft.userIDs, users)}`,
        `Permissions: ${authGroupDraft.permissions.join(', ') || '無'}`,
        `DB Scope: ${formatConnectionIDs(authGroupDraft.dbConnectionIDs, connections)}`,
      ]
      if (!confirmChanges('建立 Auth Group', createSummary)) {
        return
      }

      setSaving(true)
      setDrawerError('')
      try {
        const created = await createAuthGroup({
          name: authGroupDraft.name,
          description: authGroupDraft.description,
          user_ids: authGroupDraft.userIDs,
          permissions: authGroupDraft.permissions,
          db_connection_ids: authGroupDraft.dbConnectionIDs,
        })
        await reloadAll()
        pushToast('Auth group 已建立', 'success')
        await openEditAuthGroupDrawer(created.name as AuthGroup)
      } catch (saveError) {
        setDrawerError(saveError instanceof ApiError ? saveError.message : '建立 auth group 失敗。')
      } finally {
        setSaving(false)
      }
      return
    }

    if (drawerState.mode !== 'edit-auth-group' || !selectedAuthGroup) {
      return
    }

    const changeSummary = summarizeAuthGroupChanges(selectedAuthGroup, authGroupDraft, users, connections)
    if (changeSummary.length === 0) {
      pushToast('沒有需要儲存的變更', 'success')
      return
    }
    if (!confirmChanges(authGroupDraft.pendingDelete ? '刪除 Auth Group' : '儲存 Auth Group 變更', changeSummary)) {
      return
    }

    setSaving(true)
    setDrawerError('')
    try {
      if (authGroupDraft.pendingDelete) {
        await deleteAuthGroup(drawerState.authGroupKey)
        await reloadAll()
        pushToast('Auth group 已刪除', 'success')
        closeDrawer()
        return
      }

      await patchAuthGroup(drawerState.authGroupKey, {
        name: authGroupDraft.name,
        description: authGroupDraft.description,
        user_ids: authGroupDraft.userIDs,
        permissions: authGroupDraft.permissions,
        db_connection_ids: authGroupDraft.dbConnectionIDs,
      })
      await reloadAll()
      pushToast('Auth group 已更新', 'success')
      await openEditAuthGroupDrawer(drawerState.authGroupKey)
    } catch (saveError) {
      setDrawerError(saveError instanceof ApiError ? saveError.message : '更新 auth group 失敗。')
    } finally {
      setSaving(false)
    }
  }

  const drawerTitle =
    drawerState?.mode === 'create-user'
      ? '建立 User'
      : drawerState?.mode === 'edit-user'
        ? selectedUser?.username ?? 'User'
        : drawerState?.mode === 'create-auth-group'
          ? '建立 Auth Group'
          : selectedAuthGroup?.label ?? 'Auth Group'

  return (
    <div className="flex h-full flex-col gap-3 p-3 sm:p-4">
      <section className="rounded-xl border border-border bg-panel-soft shadow-soft">
        <div className="border-b border-border/80 px-4 py-3 sm:px-5">
          <div className="max-w-3xl">
            <h2 className="text-[24px] font-bold tracking-[-0.03em] text-ink">使用者管理</h2>
            <p className="mt-2 text-[13px] leading-6 text-muted">
              以 User 與 Auth Group 兩個視角維護權限、直接能力與 DB Scope。所有修改只在最後儲存時生效，並先顯示確認摘要。
            </p>
          </div>
        </div>

        <div className="border-b border-border/80 px-4 py-3 sm:px-5">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex flex-wrap gap-2">
              <ViewButton active={viewMode === 'users'} label="By User" onClick={() => setViewMode('users')} />
              <ViewButton active={viewMode === 'auth-groups'} label="By Auth Group" onClick={() => setViewMode('auth-groups')} />
            </div>
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                onClick={openCreateUserDrawer}
                className="inline-flex h-9 items-center justify-center gap-2 rounded-lg bg-brand px-3 text-[12px] font-bold text-white shadow-soft transition hover:bg-slate-800"
              >
                <UserPlus className="h-4 w-4" />
                建立 User
              </button>
              <button
                type="button"
                onClick={openCreateAuthGroupDrawer}
                className="inline-flex h-9 items-center justify-center gap-2 rounded-lg border border-border bg-panel-soft px-3 text-[12px] font-semibold text-ink transition hover:bg-page"
              >
                <Shield className="h-4 w-4" />
                建立 Auth Group
              </button>
            </div>
          </div>
        </div>

        <div className="px-4 py-3 sm:px-5">
          {loading ? (
            <LoadingBlock message="載入 RBAC 資料中…" className="min-h-[320px] rounded-xl border-border bg-panel" />
          ) : viewMode === 'users' ? (
            <section className="rounded-xl border border-border bg-panel shadow-soft">
              <div className="border-b border-border/80 px-4 py-3">
                <div className="flex items-center gap-2">
                  <UsersIcon className="h-4 w-4 text-accent" />
                  <p className="text-[13px] font-semibold text-ink">Users</p>
                </div>
              </div>
              <div className="overflow-x-auto">
                <table className="min-w-full border-collapse">
                  <thead className="bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                    <tr>
                      <th className="px-3 py-3">Username</th>
                      <th className="px-3 py-3">Auth Groups</th>
                      <th className="px-3 py-3">DB Scope</th>
                      <th className="px-3 py-3">Status</th>
                      <th className="px-3 py-3">Created</th>
                      <th className="px-3 py-3">Updated</th>
                      <th className="px-3 py-3">Action</th>
                    </tr>
                  </thead>
                  <tbody>
                    {users.map((user) => (
                      <tr key={user.id} className="border-t border-border text-[12px] text-ink hover:bg-slate-50/70">
                        <td className="px-3 py-3">
                          <div className="flex items-center gap-2">
                            <div>
                              <p className="font-semibold">{user.username}</p>
                              <p className="mt-0.5 text-[11px] text-muted">{user.email}</p>
                            </div>
                            {user.protected ? <Tag label="protected" tone="danger" /> : null}
                          </div>
                        </td>
                        <td className="px-3 py-3">
                          <div className="flex flex-wrap gap-1.5">
                            {user.auth_groups.length > 0
                              ? user.auth_groups.map((group) => (
                                  <Tag key={group} label={authGroupLabelMap.get(group) ?? group} />
                                ))
                              : <span className="text-[12px] text-muted">—</span>}
                          </div>
                        </td>
                        <td className="px-3 py-3">
                          <div className="flex flex-wrap gap-1.5">
                            {(user.db_connection_ids ?? []).length > 0
                              ? (user.db_connection_ids ?? []).map((connectionId) => (
                                  <Tag key={connectionId} label={getConnectionLabel(connectionId, connections)} />
                                ))
                              : <span className="text-[12px] text-muted">—</span>}
                          </div>
                        </td>
                        <td className="px-3 py-3">
                          <Tag label={user.is_active ? 'active' : 'disabled'} tone={user.is_active ? 'success' : 'danger'} />
                        </td>
                        <td className="px-3 py-3 text-muted">{formatDateTime(user.created_at)}</td>
                        <td className="px-3 py-3 text-muted">{formatDateTime(user.updated_at)}</td>
                        <td className="px-3 py-3">
                          <button
                            type="button"
                            onClick={() => void openEditUserDrawer(user.id)}
                            className="inline-flex h-8 items-center justify-center rounded-md border border-border bg-panel-soft px-3 text-[12px] font-semibold text-ink transition hover:bg-page"
                          >
                            管理
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </section>
          ) : (
            <section className="rounded-xl border border-border bg-panel shadow-soft">
              <div className="border-b border-border/80 px-4 py-3">
                <div className="flex items-center gap-2">
                  <Shield className="h-4 w-4 text-accent" />
                  <p className="text-[13px] font-semibold text-ink">Auth Groups</p>
                </div>
              </div>
              <div className="overflow-x-auto">
                <table className="min-w-full border-collapse">
                  <thead className="bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                    <tr>
                      <th className="px-3 py-3">Auth Group</th>
                      <th className="px-3 py-3">Users</th>
                      <th className="px-3 py-3">Permissions</th>
                      <th className="px-3 py-3">DB Scope</th>
                      <th className="px-3 py-3">Created</th>
                      <th className="px-3 py-3">Updated</th>
                      <th className="px-3 py-3">Action</th>
                    </tr>
                  </thead>
                  <tbody>
                    {authGroups.map((group) => (
                      <tr key={group.name} className="border-t border-border text-[12px] text-ink hover:bg-slate-50/70">
                        <td className="px-3 py-3">
                          <div>
                            <p className="font-semibold">{group.label}</p>
                            <p className="mt-0.5 text-[11px] text-muted">{group.name}</p>
                          </div>
                        </td>
                        <td className="px-3 py-3 text-muted">{group.users.length}</td>
                        <td className="px-3 py-3 text-muted">{group.permissions?.length ?? 0}</td>
                        <td className="px-3 py-3 text-muted">{group.db_connection_ids?.length ?? 0}</td>
                        <td className="px-3 py-3 text-muted">{formatDateTime(group.created_at ?? '')}</td>
                        <td className="px-3 py-3 text-muted">{formatDateTime(group.updated_at ?? '')}</td>
                        <td className="px-3 py-3">
                          <button
                            type="button"
                            onClick={() => void openEditAuthGroupDrawer(group.name as AuthGroup)}
                            className="inline-flex h-8 items-center justify-center rounded-md border border-border bg-panel-soft px-3 text-[12px] font-semibold text-ink transition hover:bg-page"
                          >
                            管理
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </section>
          )}
        </div>
      </section>

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      {drawerState ? (
        <div className="fixed inset-0 z-40 flex justify-end bg-slate-950/28 px-3 py-3 sm:px-4 sm:py-4">
          <div
            role="dialog"
            aria-modal="true"
            aria-labelledby="users-drawer-title"
            className="flex h-full w-full max-w-[720px] flex-col overflow-hidden rounded-xl border border-border bg-panel shadow-[0_22px_60px_rgba(15,23,42,0.18)]"
          >
            <div className="flex items-start justify-between border-b border-border/80 px-5 py-4">
              <div>
                <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-faint">Account Detail</p>
                <h3 id="users-drawer-title" className="mt-1 text-[22px] font-bold tracking-[-0.03em] text-ink">
                  {drawerTitle}
                </h3>
              </div>
              <button
                type="button"
                onClick={closeDrawer}
                className="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-border bg-panel-soft text-muted transition hover:bg-page hover:text-ink"
                aria-label="關閉"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
              {drawerLoading ? (
                <LoadingBlock message="載入明細中…" className="min-h-[240px] rounded-xl border-border bg-panel" />
              ) : drawerState.mode === 'create-user' || (drawerState.mode === 'edit-user' && selectedUser) ? (
                <div className="grid gap-4">
                  <CardSection title="User Profile" icon={<UsersIcon className="h-4 w-4 text-accent" />}>
                    <form className="grid gap-3" onSubmit={handleSaveUser}>
                      <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                        Username
                        <input
                          aria-label="Username"
                          value={userDraft.username}
                          onChange={(event) => setUserDraft((current) => ({ ...current, username: event.target.value }))}
                          disabled={saving || selectedUserIsProtected}
                          className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20 disabled:opacity-60"
                        />
                      </label>
                      <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                        Email
                        <input
                          aria-label="Email"
                          value={userDraft.email}
                          onChange={(event) => setUserDraft((current) => ({ ...current, email: event.target.value }))}
                          disabled={saving || selectedUserIsProtected}
                          className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20 disabled:opacity-60"
                        />
                      </label>
                      <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                        Password
                        <input
                          aria-label="Password"
                          type="password"
                          value={userDraft.password}
                          onChange={(event) => setUserDraft((current) => ({ ...current, password: event.target.value }))}
                          disabled={saving}
                          className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                        />
                      </label>

                      {drawerState.mode === 'edit-user' && selectedUser ? (
                        <div className="grid gap-3 sm:grid-cols-2">
                          <InfoBox label="Created" value={formatDateTime(selectedUser.created_at)} />
                          <InfoBox label="Updated" value={formatDateTime(selectedUser.updated_at)} />
                        </div>
                      ) : null}

                      {selectedUserIsProtected ? (
                        <div className="rounded-lg border border-danger/20 bg-red-50 px-3 py-3 text-[12px] text-danger">
                          初始 admin 只允許在此頁修改密碼，其餘欄位不可調整，也不可停用或刪除。
                        </div>
                      ) : null}

                      <button
                        type="submit"
                        disabled={
                          saving ||
                          !userDraft.username.trim() ||
                          !userDraft.email.trim() ||
                          (drawerState.mode === 'create-user' && !userDraft.password.trim())
                        }
                        className="inline-flex h-10 items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition hover:bg-slate-800 disabled:opacity-50"
                      >
                        {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
                        {drawerState.mode === 'create-user' ? '建立使用者' : userDraft.pendingDelete ? '確認刪除此 User' : '儲存變更'}
                      </button>
                    </form>
                  </CardSection>

                  {drawerState.mode === 'edit-user' && selectedUser ? (
                    <CardSection title="Account Status" icon={<Shield className="h-4 w-4 text-accent" />}>
                      <div className="flex items-center justify-between gap-3 rounded-lg border border-border bg-panel-soft px-3 py-3">
                        <div>
                          <p className="text-[12px] font-semibold text-ink">登入狀態</p>
                          <p className="mt-1 text-[11px] text-muted">停用只會先留在草稿裡，按下最上方儲存後才會真正生效。</p>
                        </div>
                        <button
                          type="button"
                          onClick={() => setUserDraft((current) => ({ ...current, isActive: !current.isActive, pendingDelete: false }))}
                          disabled={saving || selectedUserIsProtected}
                          className={`inline-flex h-10 items-center justify-center rounded-lg px-4 text-[13px] font-semibold transition disabled:opacity-50 ${
                            userDraft.isActive
                              ? 'border border-danger/20 bg-red-50 text-danger hover:bg-red-100'
                              : 'border border-border bg-white text-ink hover:bg-page'
                          }`}
                        >
                          {userDraft.isActive ? '標記為停用' : '標記為啟用'}
                        </button>
                      </div>
                    </CardSection>
                  ) : null}

                  <CardSection title="Auth Groups" icon={<Shield className="h-4 w-4 text-accent" />}>
                    <div className="grid gap-3">
                      <div className="flex flex-wrap gap-2">
                        {userDraft.authGroups.length > 0
                          ? userDraft.authGroups.map((group) => (
                              <ActionTag
                                key={group}
                                label={authGroupLabelMap.get(group) ?? group}
                                meta={group}
                                disabled={saving || selectedUserIsProtected}
                                onRemove={() => setUserDraft((current) => ({
                                  ...current,
                                  authGroups: current.authGroups.filter((item) => item !== group),
                                  pendingDelete: false,
                                }))}
                              />
                            ))
                          : <span className="text-[12px] text-muted">目前沒有綁定 auth group。</span>}
                      </div>
                      <div className="grid gap-3 sm:grid-cols-[1fr_auto]">
                        <select
                          aria-label="User auth group membership selection"
                          value={pendingUserAuthGroup}
                          onChange={(event) => setPendingUserAuthGroup(event.target.value)}
                          disabled={saving || selectedUserIsProtected}
                          className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20 disabled:opacity-60"
                        >
                          <option value="">選擇 auth group</option>
                          {authGroupOptions
                            .filter((group) => !userDraft.authGroups.includes(group))
                            .map((group) => (
                              <option key={group} value={group}>
                                {authGroupLabelMap.get(group) ?? group}
                              </option>
                            ))}
                        </select>
                        <button
                          type="button"
                          onClick={() => {
                            if (!pendingUserAuthGroup || userDraft.authGroups.includes(pendingUserAuthGroup)) {
                              return
                            }
                            setUserDraft((current) => ({
                              ...current,
                              authGroups: [...current.authGroups, pendingUserAuthGroup],
                              pendingDelete: false,
                            }))
                            setPendingUserAuthGroup('')
                          }}
                          disabled={saving || selectedUserIsProtected || !pendingUserAuthGroup}
                          className="inline-flex h-10 items-center justify-center rounded-lg border border-border bg-panel-soft px-4 text-[13px] font-semibold text-ink transition hover:bg-page disabled:opacity-50"
                        >
                          加入群組
                        </button>
                      </div>
                    </div>
                  </CardSection>

                  {drawerState.mode === 'edit-user' ? (
                    <CardSection title="Direct Permissions" icon={<Shield className="h-4 w-4 text-accent" />}>
                      <PermissionGroupBoard
                        title="Current Permissions"
                        description="只作用於這個 user 本人的額外能力。"
                        groupedPermissions={groupPermissions(userDraft.directPermissions)}
                        emptyMessage="目前沒有直接綁定的權限。"
                        removable
                        disabled={saving || selectedUserIsProtected}
                        onRemove={(permissionKey) => setUserDraft((current) => ({
                          ...current,
                          directPermissions: current.directPermissions.filter((item) => item !== permissionKey),
                          pendingDelete: false,
                        }))}
                      />
                      <PermissionSearchPanel
                        title="Add Direct Permission"
                        description="依模組瀏覽尚未綁定到此 user 的能力。"
                        search={userPermissionSearch}
                        onSearchChange={setUserPermissionSearch}
                        permissions={availableUserPermissions}
                        disabled={saving || selectedUserIsProtected}
                        emptyMessage="沒有符合搜尋條件、或全部直接權限都已加入。"
                        onAdd={(permissionKey) => setUserDraft((current) => ({
                          ...current,
                          directPermissions: [...current.directPermissions, permissionKey],
                          pendingDelete: false,
                        }))}
                      />
                    </CardSection>
                  ) : null}

                  {drawerState.mode === 'edit-user' ? (
                    <CardSection title="Direct DB Scope" icon={<Database className="h-4 w-4 text-accent" />}>
                      <DBScopeBoard
                        title="Current DB Scope"
                        description="只對這個 user 額外開放的資料庫連線。"
                        connectionIDs={userDraft.directDBConnectionIDs}
                        resolveConnection={(connectionId) => connections.find((item) => item.id === connectionId)}
                        emptyMessage="目前沒有直接綁定的 DB scope。"
                        removable
                        disabled={saving || selectedUserIsProtected}
                        onRemove={(connectionId) => setUserDraft((current) => ({
                          ...current,
                          directDBConnectionIDs: current.directDBConnectionIDs.filter((item) => item !== connectionId),
                          pendingDelete: false,
                        }))}
                      />
                      <DBScopePanel
                        title="Add Direct DB Scope"
                        description="用名稱、host 或 database 搜尋可直接綁定到此 user 的連線。"
                        search={userDBScopeSearch}
                        onSearchChange={setUserDBScopeSearch}
                        connections={availableUserConnections}
                        emptyMessage="沒有符合搜尋條件、或全部直接 DB scope 都已加入。"
                        disabled={saving || selectedUserIsProtected}
                        onAdd={(connectionId) => setUserDraft((current) => ({
                          ...current,
                          directDBConnectionIDs: [...current.directDBConnectionIDs, connectionId],
                          pendingDelete: false,
                        }))}
                      />
                    </CardSection>
                  ) : null}

                  {drawerState.mode === 'edit-user' && selectedUser ? (
                    <CardSection title="Effective Access" icon={<Database className="h-4 w-4 text-accent" />}>
                      <InfoList
                        title="Effective Permissions"
                        items={(selectedUser.permissions ?? []).map((permissionKey) => getPermissionMeta(permissionKey).label)}
                        emptyMessage="目前沒有有效權限。"
                      />
                      <InfoList
                        title="Effective DB Scope"
                        items={(selectedUser.db_connection_ids ?? []).map((connectionId) => getConnectionLabel(connectionId, connections))}
                        emptyMessage="目前沒有有效 DB scope。"
                      />
                    </CardSection>
                  ) : null}

                  {drawerState.mode === 'edit-user' && !selectedUserIsProtected ? (
                    <CardSection title="Danger Zone" icon={<Trash2 className="h-4 w-4 text-danger" />}>
                      <div className="flex items-center justify-between gap-3 rounded-lg border border-danger/20 bg-red-50 px-3 py-3">
                        <div>
                          <p className="text-[12px] font-semibold text-danger">刪除這個 User</p>
                          <p className="mt-1 text-[11px] text-danger/80">只會先標記為待刪除，真正刪除要在最上方按「儲存變更」後才會執行。</p>
                        </div>
                        <button
                          type="button"
                          onClick={() => setUserDraft((current) => ({ ...current, pendingDelete: !current.pendingDelete }))}
                          disabled={saving}
                          className={`inline-flex h-10 items-center justify-center rounded-lg px-4 text-[13px] font-semibold transition ${
                            userDraft.pendingDelete
                              ? 'border border-border bg-white text-ink hover:bg-page'
                              : 'border border-danger/20 bg-red-100 text-danger hover:bg-red-200'
                          }`}
                        >
                          {userDraft.pendingDelete ? '取消刪除標記' : '標記刪除'}
                        </button>
                      </div>
                    </CardSection>
                  ) : null}

                  {drawerError ? <InlineAlert>{drawerError}</InlineAlert> : null}
                </div>
              ) : drawerState.mode === 'create-auth-group' || (drawerState.mode === 'edit-auth-group' && selectedAuthGroup) ? (
                <div className="grid gap-4">
                  <CardSection title="Auth Group Profile" icon={<Shield className="h-4 w-4 text-accent" />}>
                    <form className="grid gap-3" onSubmit={handleSaveAuthGroup}>
                      <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                        Name
                        <input
                          aria-label="Name"
                          value={authGroupDraft.name}
                          onChange={(event) => setAuthGroupDraft((current) => ({ ...current, name: event.target.value }))}
                          disabled={saving || selectedAuthGroupIsProtected}
                          className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20 disabled:opacity-60"
                        />
                      </label>
                      <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                        Description
                        <input
                          aria-label="Description"
                          value={authGroupDraft.description}
                          onChange={(event) => setAuthGroupDraft((current) => ({ ...current, description: event.target.value }))}
                          disabled={saving || selectedAuthGroupIsProtected}
                          className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20 disabled:opacity-60"
                        />
                      </label>
                      {selectedAuthGroup ? (
                        <div className="grid gap-3 sm:grid-cols-2">
                          <InfoBox label="Created" value={formatDateTime(selectedAuthGroup.created_at ?? '')} />
                          <InfoBox label="Updated" value={formatDateTime(selectedAuthGroup.updated_at ?? '')} />
                        </div>
                      ) : null}
                      <button
                        type="submit"
                        disabled={saving || !authGroupDraft.name.trim()}
                        className="inline-flex h-10 items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition hover:bg-slate-800 disabled:opacity-50"
                      >
                        {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
                        {drawerState.mode === 'create-auth-group' ? '建立 Auth Group' : authGroupDraft.pendingDelete ? '確認刪除此 Auth Group' : '儲存 Auth Group'}
                      </button>
                    </form>
                  </CardSection>

                  <CardSection title="Bound Users" icon={<UsersIcon className="h-4 w-4 text-accent" />}>
                    <div className="flex flex-wrap gap-2">
                      {authGroupDraft.userIDs.length > 0
                        ? authGroupDraft.userIDs.map((userID) => {
                            const user = users.find((item) => item.id === userID) ?? selectedAuthGroup?.users.find((item) => item.id === userID)
                            return (
                              <ActionTag
                                key={userID}
                                label={user?.username ?? `User #${userID}`}
                                meta={user?.email}
                                disabled={saving || user?.protected === true || selectedAuthGroupIsProtected}
                                onRemove={() => setAuthGroupDraft((current) => ({
                                  ...current,
                                  userIDs: current.userIDs.filter((id) => id !== userID),
                                  pendingDelete: false,
                                }))}
                              />
                            )
                          })
                        : <span className="text-[12px] text-muted">目前沒有綁定使用者。</span>}
                    </div>
                    <div className="grid gap-3 sm:grid-cols-[1fr_auto]">
                      <select
                        aria-label="Auth Group user selection"
                        value={pendingAuthGroupUserID}
                        onChange={(event) => setPendingAuthGroupUserID(event.target.value)}
                        disabled={saving || selectedAuthGroupIsProtected}
                        className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20 disabled:opacity-60"
                      >
                        <option value="">選擇 user</option>
                        {users
                          .filter((user) => !authGroupDraft.userIDs.includes(user.id))
                          .map((user) => (
                            <option key={user.id} value={user.id}>
                              {user.username}
                            </option>
                          ))}
                      </select>
                      <button
                        type="button"
                        onClick={() => {
                          const userID = Number(pendingAuthGroupUserID)
                          if (!userID || authGroupDraft.userIDs.includes(userID)) {
                            return
                          }
                          setAuthGroupDraft((current) => ({
                            ...current,
                            userIDs: [...current.userIDs, userID],
                            pendingDelete: false,
                          }))
                          setPendingAuthGroupUserID('')
                        }}
                        disabled={saving || selectedAuthGroupIsProtected || !pendingAuthGroupUserID}
                        className="inline-flex h-10 items-center justify-center rounded-lg border border-border bg-panel-soft px-4 text-[13px] font-semibold text-ink transition hover:bg-page disabled:opacity-50"
                      >
                        加入使用者
                      </button>
                    </div>
                  </CardSection>

                  <CardSection title="Permissions" icon={<Shield className="h-4 w-4 text-accent" />}>
                    <PermissionGroupBoard
                      title="Current Permissions"
                      description="這些能力會套用到整個 auth group。"
                      groupedPermissions={groupPermissions(authGroupDraft.permissions)}
                      emptyMessage="目前沒有 auth group 權限。"
                      removable
                      disabled={saving || selectedAuthGroupIsProtected}
                      onRemove={(permissionKey) => setAuthGroupDraft((current) => ({
                        ...current,
                        permissions: current.permissions.filter((item) => item !== permissionKey),
                        pendingDelete: false,
                      }))}
                    />
                    <PermissionSearchPanel
                      title="Add Permission"
                      description="依模組瀏覽或搜尋尚未綁定的權限。"
                      search={authGroupPermissionSearch}
                      onSearchChange={setAuthGroupPermissionSearch}
                      permissions={availableAuthGroupPermissions}
                      disabled={saving || selectedAuthGroupIsProtected}
                      emptyMessage="沒有符合搜尋條件、或全部權限都已加入。"
                      onAdd={(permissionKey) => setAuthGroupDraft((current) => ({
                        ...current,
                        permissions: [...current.permissions, permissionKey],
                        pendingDelete: false,
                      }))}
                    />
                  </CardSection>

                  <CardSection title="DB Scope" icon={<Database className="h-4 w-4 text-accent" />}>
                    <DBScopeBoard
                      title="Current DB Scope"
                      description="這些資料庫連線會下放給整個 auth group。"
                      connectionIDs={authGroupDraft.dbConnectionIDs}
                      resolveConnection={(connectionId) => connections.find((item) => item.id === connectionId)}
                      emptyMessage="目前沒有 auth group DB scope。"
                      removable
                      disabled={saving || selectedAuthGroupIsProtected}
                      onRemove={(connectionId) => setAuthGroupDraft((current) => ({
                        ...current,
                        dbConnectionIDs: current.dbConnectionIDs.filter((item) => item !== connectionId),
                        pendingDelete: false,
                      }))}
                    />
                    <DBScopePanel
                      title="Add DB Scope"
                      description="用名稱、host 或 database 搜尋可綁定的資料庫資產。"
                      search={authGroupDBScopeSearch}
                      onSearchChange={setAuthGroupDBScopeSearch}
                      connections={availableAuthGroupConnections}
                      emptyMessage="沒有符合搜尋條件、或全部 DB scope 都已加入。"
                      disabled={saving || selectedAuthGroupIsProtected}
                      onAdd={(connectionId) => setAuthGroupDraft((current) => ({
                        ...current,
                        dbConnectionIDs: [...current.dbConnectionIDs, connectionId],
                        pendingDelete: false,
                      }))}
                    />
                  </CardSection>

                  {drawerState.mode === 'edit-auth-group' && !selectedAuthGroupIsProtected ? (
                    <CardSection title="Danger Zone" icon={<Trash2 className="h-4 w-4 text-danger" />}>
                      <div className="flex items-center justify-between gap-3 rounded-lg border border-danger/20 bg-red-50 px-3 py-3">
                        <div>
                          <p className="text-[12px] font-semibold text-danger">刪除這個 Auth Group</p>
                          <p className="mt-1 text-[11px] text-danger/80">只會先標記為待刪除，真正刪除要在最上方儲存後才會執行。</p>
                        </div>
                        <button
                          type="button"
                          onClick={() => setAuthGroupDraft((current) => ({ ...current, pendingDelete: !current.pendingDelete }))}
                          disabled={saving}
                          className={`inline-flex h-10 items-center justify-center rounded-lg px-4 text-[13px] font-semibold transition ${
                            authGroupDraft.pendingDelete
                              ? 'border border-border bg-white text-ink hover:bg-page'
                              : 'border border-danger/20 bg-red-100 text-danger hover:bg-red-200'
                          }`}
                        >
                          {authGroupDraft.pendingDelete ? '取消刪除標記' : '標記刪除'}
                        </button>
                      </div>
                    </CardSection>
                  ) : null}

                  {drawerError ? <InlineAlert>{drawerError}</InlineAlert> : null}
                </div>
              ) : null}
            </div>
          </div>
        </div>
      ) : null}
    </div>
  )
}

function summarizeUserChanges(
  original: UserDetail,
  draft: UserDraft,
  labelMap: Map<string, string>,
  connections: DBConnection[],
) {
  if (draft.pendingDelete) {
    return [`刪除 User: ${original.username}`]
  }

  const lines: string[] = []
  const originalGroups = original.memberships.map((membership) => membership.auth_group)
  const originalDirectPermissions = original.direct_permissions ?? []
  const originalDirectConnectionIDs = original.direct_db_connection_ids ?? []

  if (draft.username !== original.username) {
    lines.push(`Username: ${original.username} -> ${draft.username}`)
  }
  if (draft.email !== original.email) {
    lines.push(`Email: ${original.email} -> ${draft.email}`)
  }
  if (draft.password.trim()) {
    lines.push('Password: 已更新')
  }
  if (draft.isActive !== original.is_active) {
    lines.push(`Status: ${original.is_active ? 'active' : 'disabled'} -> ${draft.isActive ? 'active' : 'disabled'}`)
  }
  if (!equalStringArrays(originalGroups, draft.authGroups)) {
    lines.push(`Auth Groups: ${formatAuthGroupList(draft.authGroups, labelMap)}`)
  }
  if (!equalStringArrays(originalDirectPermissions, draft.directPermissions)) {
    lines.push(`Direct Permissions: ${draft.directPermissions.join(', ') || '無'}`)
  }
  if (!equalNumberArrays(originalDirectConnectionIDs, draft.directDBConnectionIDs)) {
    lines.push(`Direct DB Scope: ${formatConnectionIDs(draft.directDBConnectionIDs, connections)}`)
  }

  return lines
}

function summarizeAuthGroupChanges(
  original: AuthGroupDetail,
  draft: AuthGroupDraft,
  users: UserSummary[],
  connections: DBConnection[],
) {
  if (draft.pendingDelete) {
    return [`刪除 Auth Group: ${original.label}`]
  }

  const lines: string[] = []
  const originalUserIDs = original.users.map((user) => user.id)
  const originalPermissions = original.permissions ?? []
  const originalConnectionIDs = original.db_connection_ids ?? []

  if (draft.name !== original.label) {
    lines.push(`Name: ${original.label} -> ${draft.name}`)
  }
  if (draft.description !== original.description) {
    lines.push('Description: 已更新')
  }
  if (!equalNumberArrays(originalUserIDs, draft.userIDs)) {
    lines.push(`Bound Users: ${formatUserIDs(draft.userIDs, users)}`)
  }
  if (!equalStringArrays(originalPermissions, draft.permissions)) {
    lines.push(`Permissions: ${draft.permissions.join(', ') || '無'}`)
  }
  if (!equalNumberArrays(originalConnectionIDs, draft.dbConnectionIDs)) {
    lines.push(`DB Scope: ${formatConnectionIDs(draft.dbConnectionIDs, connections)}`)
  }

  return lines
}

function formatAuthGroupList(groups: string[], labelMap: Map<string, string>) {
  return groups.length > 0 ? groups.map((group) => labelMap.get(group) ?? group).join(', ') : '無'
}

function formatUserIDs(userIDs: number[], users: UserSummary[]) {
  return userIDs.length > 0 ? userIDs.map((userID) => users.find((user) => user.id === userID)?.username ?? `User #${userID}`).join(', ') : '無'
}

function formatConnectionIDs(connectionIDs: number[], connections: DBConnection[]) {
  return connectionIDs.length > 0 ? connectionIDs.map((id) => getConnectionLabel(id, connections)).join(', ') : '無'
}

function getConnectionLabel(connectionId: number, connections: DBConnection[]) {
  const connection = connections.find((item) => item.id === connectionId)
  return connection ? connection.name : `Connection #${connectionId}`
}

function equalStringArrays(left: string[], right: string[]) {
  return JSON.stringify([...left].sort()) === JSON.stringify([...right].sort())
}

function equalNumberArrays(left: number[], right: number[]) {
  return JSON.stringify([...left].sort((a, b) => a - b)) === JSON.stringify([...right].sort((a, b) => a - b))
}

function getPermissionMeta(permissionKey: string): PermissionOption {
  return PERMISSION_INDEX.get(permissionKey) ?? {
    key: permissionKey,
    module: 'Other',
    action: 'Custom',
    label: permissionKey,
    description: '未定義描述的自訂權限。',
  }
}

function groupPermissions(permissionKeys: string[]) {
  const groups = new Map<string, PermissionOption[]>()
  permissionKeys.forEach((permissionKey) => {
    const meta = getPermissionMeta(permissionKey)
    const current = groups.get(meta.module) ?? []
    current.push(meta)
    groups.set(meta.module, current)
  })
  return Array.from(groups.entries()).map(([module, permissions]) => ({
    module,
    permissions: permissions.sort((left, right) => left.key.localeCompare(right.key)),
  }))
}

function filterPermissionCatalog(search: string, excluded: string[]) {
  const keyword = search.trim().toLowerCase()
  return PERMISSION_METADATA.filter((permission) => {
    if (excluded.includes(permission.key)) {
      return false
    }
    if (!keyword) {
      return true
    }
    const haystack = [permission.key, permission.label, permission.module, permission.action, permission.description].join(' ').toLowerCase()
    return haystack.includes(keyword)
  })
}

function filterConnections(connections: DBConnection[], search: string, excludedIDs: number[]) {
  const keyword = search.trim().toLowerCase()
  return connections.filter((connection) => {
    if (excludedIDs.includes(connection.id)) {
      return false
    }
    if (!keyword) {
      return true
    }
    const haystack = [
      connection.name,
      connection.db_type,
      connection.host,
      connection.database_name ?? '',
      connection.username,
    ].join(' ').toLowerCase()
    return haystack.includes(keyword)
  })
}

function ViewButton({ active, label, onClick }: { active: boolean; label: string; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`inline-flex h-9 items-center justify-center rounded-lg border px-3 text-[12px] font-semibold transition ${
        active ? 'border-brand bg-brand text-white' : 'border-border bg-white text-ink hover:bg-panel-soft'
      }`}
    >
      {label}
    </button>
  )
}

function CardSection({ title, icon, children }: { title: string; icon: ReactNode; children: ReactNode }) {
  return (
    <section className="rounded-xl border border-border bg-panel shadow-soft">
      <div className="border-b border-border/80 px-4 py-3">
        <div className="flex items-center gap-2">
          {icon}
          <p className="text-[13px] font-semibold text-ink">{title}</p>
        </div>
      </div>
      <div className="grid gap-4 px-4 py-4">{children}</div>
    </section>
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

function InfoList({ title, items, emptyMessage }: { title: string; items: string[]; emptyMessage: string }) {
  return (
    <div className="grid gap-2 rounded-lg border border-border bg-panel-soft px-3 py-3">
      <p className="text-[12px] font-semibold text-ink">{title}</p>
      <div className="flex flex-wrap gap-2">
        {items.length > 0 ? items.map((item) => <Tag key={item} label={item} />) : <span className="text-[12px] text-muted">{emptyMessage}</span>}
      </div>
    </div>
  )
}

function Tag({ label, tone = 'default' }: { label: string; tone?: 'default' | 'danger' | 'success' }) {
  return (
    <span
      className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] font-semibold ${
        tone === 'danger'
          ? 'border-danger/20 bg-red-50 text-danger'
          : tone === 'success'
            ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
            : 'border-border bg-panel-soft text-ink'
      }`}
    >
      {label}
    </span>
  )
}

function ActionTag({
  label,
  meta,
  disabled,
  onRemove,
}: {
  label: string
  meta?: string
  disabled?: boolean
  onRemove: () => void
}) {
  return (
    <div className="inline-flex items-center gap-2 rounded-lg border border-border bg-white px-3 py-2">
      <div>
        <p className="text-[12px] font-semibold text-ink">{label}</p>
        {meta ? <p className="text-[10px] text-muted">{meta}</p> : null}
      </div>
      <button
        type="button"
        onClick={onRemove}
        disabled={disabled}
        className="inline-flex h-7 w-7 items-center justify-center rounded-md border border-border bg-panel-soft text-muted transition hover:bg-page hover:text-ink disabled:opacity-40"
        aria-label={`移除 ${label}`}
      >
        <X className="h-3.5 w-3.5" />
      </button>
    </div>
  )
}

function PermissionGroupBoard({
  title,
  description,
  groupedPermissions,
  emptyMessage,
  removable = false,
  disabled = false,
  onRemove,
}: {
  title: string
  description: string
  groupedPermissions: Array<{ module: string; permissions: PermissionOption[] }>
  emptyMessage: string
  removable?: boolean
  disabled?: boolean
  onRemove?: (permissionKey: string) => void
}) {
  return (
    <div className="grid gap-3 rounded-xl border border-border bg-panel-soft/80 px-3 py-3">
      <div>
        <p className="text-[12px] font-semibold text-ink">{title}</p>
        <p className="mt-1 text-[11px] text-muted">{description}</p>
      </div>
      {groupedPermissions.length > 0 ? (
        groupedPermissions.map((group) => (
          <div key={group.module} className="grid gap-2 rounded-lg border border-border/80 bg-white px-3 py-3">
            <p className="text-[11px] font-bold uppercase tracking-[0.14em] text-faint">{group.module}</p>
            <div className="flex flex-wrap gap-2">
              {group.permissions.map((permission) => (
                removable && onRemove ? (
                  <ActionTag
                    key={permission.key}
                    label={permission.label}
                    meta={permission.key}
                    disabled={disabled}
                    onRemove={() => onRemove(permission.key)}
                  />
                ) : (
                  <Tag key={permission.key} label={permission.label} />
                )
              ))}
            </div>
          </div>
        ))
      ) : (
        <span className="text-[12px] text-muted">{emptyMessage}</span>
      )}
    </div>
  )
}

function PermissionSearchPanel({
  title,
  description,
  search,
  onSearchChange,
  permissions,
  disabled,
  emptyMessage,
  onAdd,
}: {
  title: string
  description: string
  search: string
  onSearchChange: (value: string) => void
  permissions: PermissionOption[]
  disabled: boolean
  emptyMessage: string
  onAdd: (permissionKey: string) => void
}) {
  const grouped = permissions.reduce<Record<string, PermissionOption[]>>((accumulator, permission) => {
    accumulator[permission.module] = [...(accumulator[permission.module] ?? []), permission]
    return accumulator
  }, {})

  return (
    <div className="grid gap-3 rounded-xl border border-border bg-panel-soft/80 px-3 py-3">
      <div>
        <p className="text-[12px] font-semibold text-ink">{title}</p>
        <p className="mt-1 text-[11px] text-muted">{description}</p>
      </div>
      <input
        value={search}
        onChange={(event) => onSearchChange(event.target.value)}
        placeholder="搜尋 module、action、permission key"
        className="h-10 rounded-lg border border-border bg-white px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
        disabled={disabled}
      />
      <div className="grid gap-3">
        {Object.entries(grouped).length > 0 ? (
          Object.entries(grouped).map(([module, modulePermissions]) => (
            <div key={module} className="grid gap-2 rounded-lg border border-border/80 bg-white px-3 py-3">
              <p className="text-[11px] font-bold uppercase tracking-[0.14em] text-faint">{module}</p>
              {modulePermissions.map((permission) => (
                <div key={permission.key} className="flex items-start justify-between gap-3 rounded-lg border border-border bg-panel-soft px-3 py-2.5">
                  <div>
                    <p className="text-[12px] font-semibold text-ink">{permission.label}</p>
                    <p className="mt-1 text-[11px] text-muted">{permission.description}</p>
                    <p className="mt-1 font-mono text-[11px] text-faint">{permission.key}</p>
                  </div>
                  <button
                    type="button"
                    onClick={() => onAdd(permission.key)}
                    disabled={disabled}
                    className="inline-flex h-8 items-center justify-center rounded-md border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:opacity-50"
                  >
                    加入
                  </button>
                </div>
              ))}
            </div>
          ))
        ) : (
          <span className="text-[12px] text-muted">{emptyMessage}</span>
        )}
      </div>
    </div>
  )
}

function DBScopeBoard({
  title,
  description,
  connectionIDs,
  resolveConnection,
  emptyMessage,
  removable = false,
  disabled = false,
  onRemove,
}: {
  title: string
  description: string
  connectionIDs: number[]
  resolveConnection: (connectionId: number) => DBConnection | undefined
  emptyMessage: string
  removable?: boolean
  disabled?: boolean
  onRemove?: (connectionId: number) => void
}) {
  return (
    <div className="grid gap-3 rounded-xl border border-border bg-panel-soft/80 px-3 py-3">
      <div>
        <p className="text-[12px] font-semibold text-ink">{title}</p>
        <p className="mt-1 text-[11px] text-muted">{description}</p>
      </div>
      <div className="flex flex-wrap gap-2">
        {connectionIDs.length > 0 ? connectionIDs.map((connectionId) => {
          const connection = resolveConnection(connectionId)
          const label = connection?.name ?? `Connection #${connectionId}`
          const meta = connection ? `${connection.db_type} / ${connection.host}` : undefined
          return removable && onRemove ? (
            <ActionTag key={connectionId} label={label} meta={meta} disabled={disabled} onRemove={() => onRemove(connectionId)} />
          ) : (
            <Tag key={connectionId} label={label} />
          )
        }) : <span className="text-[12px] text-muted">{emptyMessage}</span>}
      </div>
    </div>
  )
}

function DBScopePanel({
  title,
  description,
  search,
  onSearchChange,
  connections,
  emptyMessage,
  disabled,
  onAdd,
}: {
  title: string
  description: string
  search: string
  onSearchChange: (value: string) => void
  connections: DBConnection[]
  emptyMessage: string
  disabled: boolean
  onAdd: (connectionId: number) => void
}) {
  return (
    <div className="grid gap-3 rounded-xl border border-border bg-panel-soft/80 px-3 py-3">
      <div>
        <p className="text-[12px] font-semibold text-ink">{title}</p>
        <p className="mt-1 text-[11px] text-muted">{description}</p>
      </div>
      <input
        value={search}
        onChange={(event) => onSearchChange(event.target.value)}
        placeholder="搜尋 connection name、host、database"
        className="h-10 rounded-lg border border-border bg-white px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
        disabled={disabled}
      />
      <div className="grid gap-2">
        {connections.length > 0 ? connections.map((connection) => (
          <div key={connection.id} className="flex items-start justify-between gap-3 rounded-lg border border-border bg-white px-3 py-2.5">
            <div>
              <p className="text-[12px] font-semibold text-ink">{connection.name}</p>
              <p className="mt-1 text-[11px] text-muted">{connection.db_type} / {connection.host}</p>
            </div>
            <button
              type="button"
              onClick={() => onAdd(connection.id)}
              disabled={disabled}
              className="inline-flex h-8 items-center justify-center rounded-md border border-border bg-panel-soft px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:opacity-50"
            >
              加入
            </button>
          </div>
        )) : <span className="text-[12px] text-muted">{emptyMessage}</span>}
      </div>
    </div>
  )
}
