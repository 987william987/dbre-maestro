import { useEffect, useMemo, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { Database, Loader2, Shield, Trash2, UserPlus, Users as UsersIcon, X } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { createAuthGroup, deleteAuthGroup, getAuthGroup, listAuthGroups, patchAuthGroup } from '@/modules/auth-groups/api'
import { createUser, deleteUser, getUser, listUserDBConnections, listUsers, patchUser } from '@/modules/users/api'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import type { AuthGroup } from '@/shared/types/auth'
import type { AuthGroupDetail } from '@/shared/types/authGroup'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { UserDetail, UserSummary } from '@/shared/types/user'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { PageIntro } from '@/shared/ui/PageIntro'
import { Pagination } from '@/shared/ui/Pagination'
import { PageTabs } from '@/shared/ui/PageTabs'
import { useToast } from '@/shared/ui/ToastContext'

type PermissionOption = {
  key: string
  module: string
  action: string
  label: string
  description: string
}

const PERMISSION_METADATA: PermissionOption[] = [
  { key: 'users.read', module: 'Users', action: 'Read', label: 'Users Read', description: 'View the Users and RBAC workspace.' },
  { key: 'users.write', module: 'Users', action: 'Write', label: 'Users Write', description: 'Manage users and auth groups.' },
  { key: 'audit_logs.read', module: 'Audit Logs', action: 'Read', label: 'Audit Logs Read', description: 'View the audit log page.' },
  { key: 'audit_logs.write', module: 'Audit Logs', action: 'Write', label: 'Audit Logs Write', description: 'Export audit log reports.' },
  { key: 'settings.read', module: 'Settings', action: 'Read', label: 'Settings Read', description: 'View the settings page.' },
  { key: 'settings.write', module: 'Settings', action: 'Write', label: 'Settings Write', description: 'Modify platform settings.' },
  { key: 'db_connections.read', module: 'DB Connections', action: 'Read', label: 'DB Connections Read', description: 'View the database connection list.' },
  { key: 'db_connections.write', module: 'DB Connections', action: 'Write', label: 'DB Connections Write', description: 'Create, update, and delete database connections.' },
  { key: 'db_metadata.read', module: 'DB Metadata', action: 'Read', label: 'DB Metadata Read', description: 'View cloud inventory and database object snapshots.' },
  { key: 'masking_rules.read', module: 'Masking Rules', action: 'Read', label: 'Masking Rules Read', description: 'View masking rules and whitelist entries.' },
  { key: 'masking_rules.write', module: 'Masking Rules', action: 'Write', label: 'Masking Rules Write', description: 'Manage masking rules and whitelist entries.' },
  { key: 'sql_review.read', module: 'SQL Review', action: 'Read', label: 'SQL Review Read', description: 'View SQL review rules.' },
  { key: 'sql_review.write', module: 'SQL Review', action: 'Write', label: 'SQL Review Write', description: 'Manage SQL review rules.' },
  { key: 'tickets.apply', module: 'Tickets', action: 'Apply', label: 'Tickets Apply', description: 'Create DDL and DML tickets.' },
  { key: 'tickets.review', module: 'Tickets', action: 'Review', label: 'Tickets Review', description: 'Review DDL and DML tickets.' },
  { key: 'tickets.execute', module: 'Tickets', action: 'Execute', label: 'Tickets Execute', description: 'Execute DDL and DML tickets.' },
  { key: 'sql_editor.query', module: 'SQL Editor', action: 'Query', label: 'SQL Editor Query', description: 'Run queries in SQL Editor.' },
  { key: 'sql_editor.export', module: 'SQL Editor', action: 'Export', label: 'SQL Editor Export', description: 'Export the current query result.' },
  { key: 'sql_editor.export_review', module: 'SQL Editor', action: 'Export Review', label: 'Export Review', description: 'Review SQL export requests.' },
  { key: 'sql_editor.sensitive_apply', module: 'SQL Editor', action: 'Sensitive Apply', label: 'Sensitive Apply', description: 'Request temporary sensitive data access.' },
  { key: 'sql_editor.sensitive_review', module: 'SQL Editor', action: 'Sensitive Review', label: 'Sensitive Review', description: 'Review or revoke sensitive data access requests.' },
  { key: 'sql_editor.sensitive_execute', module: 'SQL Editor', action: 'Sensitive Execute', label: 'Sensitive Execute', description: 'Execute sensitive data access requests.' },
  { key: 'global.sensitive', module: 'Global', action: 'Sensitive', label: 'Global Sensitive', description: 'Bypass masking rules permanently to view sensitive data.' },
]
const PAGE_SIZE = 20

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

export function UsersPage({ initialView = 'users' }: { initialView?: ViewMode }) {
  const { pushToast } = useToast()
  const navigate = useNavigate()
  const [viewMode, setViewMode] = useState<ViewMode>(initialView)
  const [usersOffset, setUsersOffset] = useState(0)
  const [authGroupsOffset, setAuthGroupsOffset] = useState(0)
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

  useEffect(() => {
    setViewMode(initialView)
  }, [initialView])

  async function bootstrap() {
    setLoading(true)
    setError('')
    try {
      const [usersResponse, connectionsResponse, authGroupDetails] = await Promise.all([
        listUsers(),
        listUserDBConnections(),
        loadAuthGroupDetails(),
      ])
      setUsers(usersResponse.users)
      setConnections(connectionsResponse.connections)
      setAuthGroups(authGroupDetails)
    } catch (loadError) {
      setError(loadError instanceof ApiError ? loadError.message : 'Failed to load the RBAC workspace.')
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
      setDrawerError(loadError instanceof ApiError ? loadError.message : 'Failed to load user details.')
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
      setDrawerError(loadError instanceof ApiError ? loadError.message : 'Failed to load auth group details.')
    } finally {
      setDrawerLoading(false)
    }
  }

  function confirmChanges(title: string, lines: string[]) {
    const summary = lines.length > 0 ? lines.map((line) => `- ${line}`).join('\n') : '- No field changes'
    return window.confirm(`${title}\n\n${summary}\n\nDo you want to continue?`)
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
        `Direct Permissions: ${userDraft.directPermissions.join(', ') || 'None'}`,
        `Direct DB Scope: ${formatConnectionIDs(userDraft.directDBConnectionIDs, connections)}`,
      ]
      if (!confirmChanges('Create User', createSummary)) {
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
        pushToast('User created', 'success')
        await openEditUserDrawer(created.id)
      } catch (saveError) {
        setDrawerError(saveError instanceof ApiError ? saveError.message : 'Failed to create the user.')
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
      pushToast('No changes to save', 'success')
      return
    }
    if (!confirmChanges(userDraft.pendingDelete ? 'Delete User' : 'Save User Changes', changeSummary)) {
      return
    }

    setSaving(true)
    setDrawerError('')
    try {
      if (userDraft.pendingDelete) {
        await deleteUser(drawerState.userId)
        await reloadAll()
        pushToast('User deleted', 'success')
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
      pushToast('User updated', 'success')
      await openEditUserDrawer(drawerState.userId)
    } catch (saveError) {
      setDrawerError(saveError instanceof ApiError ? saveError.message : 'Failed to update the user.')
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
        `Description: ${authGroupDraft.description || 'None'}`,
        `Bound Users: ${formatUserIDs(authGroupDraft.userIDs, users)}`,
        `Permissions: ${authGroupDraft.permissions.join(', ') || 'None'}`,
        `DB Scope: ${formatConnectionIDs(authGroupDraft.dbConnectionIDs, connections)}`,
      ]
      if (!confirmChanges('Create Auth Group', createSummary)) {
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
        pushToast('Auth group created', 'success')
        await openEditAuthGroupDrawer(created.name as AuthGroup)
      } catch (saveError) {
        setDrawerError(saveError instanceof ApiError ? saveError.message : 'Failed to create the auth group.')
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
      pushToast('No changes to save', 'success')
      return
    }
    if (!confirmChanges(authGroupDraft.pendingDelete ? 'Delete Auth Group' : 'Save Auth Group Changes', changeSummary)) {
      return
    }

    setSaving(true)
    setDrawerError('')
    try {
      if (authGroupDraft.pendingDelete) {
        await deleteAuthGroup(drawerState.authGroupKey)
        await reloadAll()
        pushToast('Auth group deleted', 'success')
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
      pushToast('Auth group updated', 'success')
      await openEditAuthGroupDrawer(drawerState.authGroupKey)
    } catch (saveError) {
      setDrawerError(saveError instanceof ApiError ? saveError.message : 'Failed to update the auth group.')
    } finally {
      setSaving(false)
    }
  }

  const drawerTitle =
    drawerState?.mode === 'create-user'
      ? 'Create User'
      : drawerState?.mode === 'edit-user'
        ? selectedUser?.username ?? 'User'
        : drawerState?.mode === 'create-auth-group'
          ? 'Create Auth Group'
          : selectedAuthGroup?.label ?? 'Auth Group'
  const pagedUsers = useMemo(() => users.slice(usersOffset, usersOffset + PAGE_SIZE), [users, usersOffset])
  const pagedAuthGroups = useMemo(
    () => authGroups.slice(authGroupsOffset, authGroupsOffset + PAGE_SIZE),
    [authGroups, authGroupsOffset],
  )

  useEffect(() => {
    if (usersOffset > 0 && usersOffset >= users.length) {
      setUsersOffset(Math.max(0, Math.floor((Math.max(users.length - 1, 0)) / PAGE_SIZE) * PAGE_SIZE))
    }
  }, [users.length, usersOffset])

  useEffect(() => {
    if (authGroupsOffset > 0 && authGroupsOffset >= authGroups.length) {
      setAuthGroupsOffset(Math.max(0, Math.floor((Math.max(authGroups.length - 1, 0)) / PAGE_SIZE) * PAGE_SIZE))
    }
  }, [authGroups.length, authGroupsOffset])

  return (
    <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
      <PageIntro
        title="User Management"
        description="Manage users and auth groups with their permissions, direct capabilities, and DB scope. All changes take effect only after saving, with a confirmation summary shown first."
        actions={
          <>
            <button
              type="button"
              onClick={openCreateUserDrawer}
              className="inline-flex h-9 items-center justify-center gap-2 rounded-lg bg-brand px-3 text-[12px] font-bold text-white shadow-soft transition hover:bg-slate-800"
            >
              <UserPlus className="h-4 w-4" />
              Create User
            </button>
            <button
              type="button"
              onClick={openCreateAuthGroupDrawer}
              className="inline-flex h-9 items-center justify-center gap-2 rounded-lg border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-panel-soft"
            >
              <Shield className="h-4 w-4" />
              Create Auth Group
            </button>
          </>
        }
      />

      <PageTabs
        items={[
          {
            key: 'users',
            label: 'Users',
            active: viewMode === 'users',
            onClick: () => {
              setViewMode('users')
              navigate('/users')
            },
          },
          {
            key: 'auth-groups',
            label: 'Auth Groups',
            active: viewMode === 'auth-groups',
            onClick: () => {
              setViewMode('auth-groups')
              navigate('/users/groups')
            },
          },
        ]}
      />

      {loading ? (
        <LoadingBlock message="Loading RBAC workspace..." className="min-h-[320px] rounded-xl border-border bg-panel" />
      ) : viewMode === 'users' ? (
            <section className="overflow-hidden rounded-xl border border-border bg-panel shadow-soft">
              <div className="overflow-x-auto">
                <table className="min-w-full border-collapse">
                  <thead className="bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                    <tr>
                      <th className="whitespace-nowrap px-3 py-3">Username</th>
                      <th className="whitespace-nowrap px-3 py-3">Auth Groups</th>
                      <th className="whitespace-nowrap px-3 py-3">DB Scope</th>
                      <th className="whitespace-nowrap px-3 py-3">Status</th>
                      <th className="whitespace-nowrap px-3 py-3">Created</th>
                      <th className="whitespace-nowrap px-3 py-3">Updated</th>
                      <th className="whitespace-nowrap px-3 py-3">Action</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pagedUsers.map((user) => (
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
                          <div className="flex flex-wrap items-center gap-1.5">
                            {(user.db_connection_ids ?? []).length > 0 ? (
                              <>
                                {(user.db_connection_ids ?? []).slice(0, 2).map((connectionId) => (
                                  <Tag key={connectionId} label={getConnectionLabel(connectionId, connections)} />
                                ))}
                                {(user.db_connection_ids ?? []).length > 2 ? (
                                  <span
                                    className="whitespace-nowrap text-[11px] font-semibold text-muted"
                                    title={(user.db_connection_ids ?? []).slice(2).map((id) => getConnectionLabel(id, connections)).join(', ')}
                                  >
                                    +{(user.db_connection_ids ?? []).length - 2}
                                  </span>
                                ) : null}
                              </>
                            ) : (
                              <span className="text-[12px] text-muted">—</span>
                            )}
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
                            className="inline-flex h-8 items-center justify-center whitespace-nowrap rounded-md border border-border bg-panel-soft px-3 text-[12px] font-semibold text-ink transition hover:bg-page"
                          >
                            Manage
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </section>
          ) : (
            <section className="overflow-hidden rounded-xl border border-border bg-panel shadow-soft">
              <div className="overflow-x-auto">
                <table className="min-w-full border-collapse">
                  <thead className="bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                    <tr>
                      <th className="whitespace-nowrap px-3 py-3">Auth Group</th>
                      <th className="whitespace-nowrap px-3 py-3">Users</th>
                      <th className="whitespace-nowrap px-3 py-3">Permissions</th>
                      <th className="whitespace-nowrap px-3 py-3">DB Scope</th>
                      <th className="whitespace-nowrap px-3 py-3">Created</th>
                      <th className="whitespace-nowrap px-3 py-3">Updated</th>
                      <th className="whitespace-nowrap px-3 py-3">Action</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pagedAuthGroups.map((group) => (
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
                            className="inline-flex h-8 items-center justify-center whitespace-nowrap rounded-md border border-border bg-panel-soft px-3 text-[12px] font-semibold text-ink transition hover:bg-page"
                          >
                            Manage
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </section>
          )}

      <Pagination
        offset={viewMode === 'users' ? usersOffset : authGroupsOffset}
        pageSize={PAGE_SIZE}
        count={viewMode === 'users' ? pagedUsers.length : pagedAuthGroups.length}
        total={viewMode === 'users' ? users.length : authGroups.length}
        onChange={viewMode === 'users' ? setUsersOffset : setAuthGroupsOffset}
      />

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
                aria-label="Close"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
              {drawerLoading ? (
                <LoadingBlock message="Loading details..." className="min-h-[240px] rounded-xl border-border bg-panel" />
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
                          The initial admin can only change the password here. Other fields cannot be edited, disabled, or deleted.
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
                        {drawerState.mode === 'create-user' ? 'Confirm Create' : userDraft.pendingDelete ? 'Confirm Delete' : 'Save Changes'}
                      </button>
                    </form>
                  </CardSection>

                  {drawerState.mode === 'edit-user' && selectedUser ? (
                    <CardSection title="Account Status" icon={<Shield className="h-4 w-4 text-accent" />}>
                      <div className="flex items-center justify-between gap-3 rounded-lg border border-border bg-panel-soft px-3 py-3">
                        <div>
                          <p className="text-[12px] font-semibold text-ink">Sign-in Status</p>
                          <p className="mt-1 text-[11px] text-muted">Disabling only updates the draft. The change is applied after you save at the top.</p>
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
                          {userDraft.isActive ? 'Mark Disabled' : 'Mark Enabled'}
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
                          : <span className="text-[12px] text-muted">No auth groups assigned yet.</span>}
                      </div>
                      <div className="grid gap-3 sm:grid-cols-[1fr_auto]">
                        <DropdownSelect
                          ariaLabel="User auth group membership selection"
                          value={pendingUserAuthGroup}
                          onChange={setPendingUserAuthGroup}
                          disabled={saving || selectedUserIsProtected}
                          options={[
                            { value: '', label: 'Select auth group' },
                            ...authGroupOptions
                              .filter((group) => !userDraft.authGroups.includes(group))
                              .map((group) => ({
                                value: group,
                                label: authGroupLabelMap.get(group) ?? group,
                              })),
                          ]}
                        />
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
                          Add to Group
                        </button>
                      </div>
                    </div>
                  </CardSection>

                  {drawerState.mode === 'edit-user' ? (
                    <CardSection title="Direct Permissions" icon={<Shield className="h-4 w-4 text-accent" />}>
                      <PermissionGroupBoard
                        title="Current Permissions"
                        description="Extra capabilities granted only to this user."
                        groupedPermissions={groupPermissions(userDraft.directPermissions)}
                        emptyMessage="No direct permissions assigned yet."
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
                        description="Browse unassigned capabilities for this user by module."
                        search={userPermissionSearch}
                        onSearchChange={setUserPermissionSearch}
                        permissions={availableUserPermissions}
                        disabled={saving || selectedUserIsProtected}
                        emptyMessage="No matches found, or all direct permissions are already assigned."
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
                        description="Database connections granted only to this user."
                        connectionIDs={userDraft.directDBConnectionIDs}
                        resolveConnection={(connectionId) => connections.find((item) => item.id === connectionId)}
                        emptyMessage="No direct DB scope assigned yet."
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
                        description="Search assignable connections by name, host, or database."
                        search={userDBScopeSearch}
                        onSearchChange={setUserDBScopeSearch}
                        connections={availableUserConnections}
                        emptyMessage="No matches found, or all direct DB scope entries are already assigned."
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
                        emptyMessage="No effective permissions."
                      />
                      <InfoList
                        title="Effective DB Scope"
                        items={(selectedUser.db_connection_ids ?? []).map((connectionId) => getConnectionLabel(connectionId, connections))}
                        emptyMessage="No effective DB scope."
                      />
                    </CardSection>
                  ) : null}

                  {drawerState.mode === 'edit-user' && !selectedUserIsProtected ? (
                    <CardSection title="Danger Zone" icon={<Trash2 className="h-4 w-4 text-danger" />}>
                      <div className="flex items-center justify-between gap-3 rounded-lg border border-danger/20 bg-red-50 px-3 py-3">
                        <div>
                          <p className="text-[12px] font-semibold text-danger">Delete This User</p>
                          <p className="mt-1 text-[11px] text-danger/80">This only marks the user for deletion. The deletion runs after you confirm Save Changes at the top.</p>
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
                          {userDraft.pendingDelete ? 'Cancel Delete' : 'Mark Delete'}
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
                        {drawerState.mode === 'create-auth-group' ? 'Create Auth Group' : authGroupDraft.pendingDelete ? 'Confirm Delete' : 'Save Auth Group'}
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
                        : <span className="text-[12px] text-muted">No users assigned yet.</span>}
                    </div>
                    <div className="grid gap-3 sm:grid-cols-[1fr_auto]">
                      <DropdownSelect
                        ariaLabel="Auth Group user selection"
                        value={pendingAuthGroupUserID}
                        onChange={setPendingAuthGroupUserID}
                        disabled={saving || selectedAuthGroupIsProtected}
                        options={[
                          { value: '', label: 'Select user' },
                          ...users
                            .filter((user) => !authGroupDraft.userIDs.includes(user.id))
                            .map((user) => ({
                              value: String(user.id),
                              label: user.username,
                            })),
                        ]}
                      />
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
                        Add User
                      </button>
                    </div>
                  </CardSection>

                  <CardSection title="Permissions" icon={<Shield className="h-4 w-4 text-accent" />}>
                    <PermissionGroupBoard
                      title="Current Permissions"
                      description="These capabilities apply to the entire auth group."
                      groupedPermissions={groupPermissions(authGroupDraft.permissions)}
                      emptyMessage="No auth group permissions yet."
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
                      description="Browse or search permissions that are not yet assigned."
                      search={authGroupPermissionSearch}
                      onSearchChange={setAuthGroupPermissionSearch}
                      permissions={availableAuthGroupPermissions}
                      disabled={saving || selectedAuthGroupIsProtected}
                      emptyMessage="No matches found, or all permissions are already assigned."
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
                      description="These database connections are inherited by the entire auth group."
                      connectionIDs={authGroupDraft.dbConnectionIDs}
                      resolveConnection={(connectionId) => connections.find((item) => item.id === connectionId)}
                      emptyMessage="No auth group DB scope yet."
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
                      description="Search assignable database assets by name, host, or database."
                      search={authGroupDBScopeSearch}
                      onSearchChange={setAuthGroupDBScopeSearch}
                      connections={availableAuthGroupConnections}
                      emptyMessage="No matches found, or all DB scope entries are already assigned."
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
                          <p className="text-[12px] font-semibold text-danger">Delete This Auth Group</p>
                          <p className="mt-1 text-[11px] text-danger/80">This only marks the auth group for deletion. The deletion runs after you save at the top.</p>
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
                          {authGroupDraft.pendingDelete ? 'Cancel Delete' : 'Mark Delete'}
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
    return [`Delete User: ${original.username}`]
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
    lines.push('Password: updated')
  }
  if (draft.isActive !== original.is_active) {
    lines.push(`Status: ${original.is_active ? 'active' : 'disabled'} -> ${draft.isActive ? 'active' : 'disabled'}`)
  }
  if (!equalStringArrays(originalGroups, draft.authGroups)) {
    lines.push(`Auth Groups: ${formatAuthGroupList(draft.authGroups, labelMap)}`)
  }
  if (!equalStringArrays(originalDirectPermissions, draft.directPermissions)) {
    lines.push(`Direct Permissions: ${draft.directPermissions.join(', ') || 'None'}`)
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
    return [`Delete Auth Group: ${original.label}`]
  }

  const lines: string[] = []
  const originalUserIDs = original.users.map((user) => user.id)
  const originalPermissions = original.permissions ?? []
  const originalConnectionIDs = original.db_connection_ids ?? []

  if (draft.name !== original.label) {
    lines.push(`Name: ${original.label} -> ${draft.name}`)
  }
  if (draft.description !== original.description) {
    lines.push('Description: updated')
  }
  if (!equalNumberArrays(originalUserIDs, draft.userIDs)) {
    lines.push(`Bound Users: ${formatUserIDs(draft.userIDs, users)}`)
  }
  if (!equalStringArrays(originalPermissions, draft.permissions)) {
    lines.push(`Permissions: ${draft.permissions.join(', ') || 'None'}`)
  }
  if (!equalNumberArrays(originalConnectionIDs, draft.dbConnectionIDs)) {
    lines.push(`DB Scope: ${formatConnectionIDs(draft.dbConnectionIDs, connections)}`)
  }

  return lines
}

function formatAuthGroupList(groups: string[], labelMap: Map<string, string>) {
  return groups.length > 0 ? groups.map((group) => labelMap.get(group) ?? group).join(', ') : 'None'
}

function formatUserIDs(userIDs: number[], users: UserSummary[]) {
  return userIDs.length > 0 ? userIDs.map((userID) => users.find((user) => user.id === userID)?.username ?? `User #${userID}`).join(', ') : 'None'
}

function formatConnectionIDs(connectionIDs: number[], connections: DBConnection[]) {
  return connectionIDs.length > 0 ? connectionIDs.map((id) => getConnectionLabel(id, connections)).join(', ') : 'None'
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
    description: 'Custom permission without a predefined description.',
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
        aria-label={`Remove ${label}`}
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
        placeholder="Search module, action, or permission key"
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
                    Add
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
        placeholder="Search connection name, host, database"
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
              Add
            </button>
          </div>
        )) : <span className="text-[12px] text-muted">{emptyMessage}</span>}
      </div>
    </div>
  )
}
