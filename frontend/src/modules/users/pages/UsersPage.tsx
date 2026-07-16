import { useEffect, useMemo, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { Database, Info, KeyRound, Loader2, Plus, RefreshCw, Shield, Trash2, UserPlus, Users as UsersIcon, X } from 'lucide-react'
import { Link, useNavigate } from 'react-router-dom'
import { createAuthGroup, deleteAuthGroup, getAuthGroup, listAuthGroups, patchAuthGroup } from '@/modules/auth-groups/api'
import { getDBConnectionBindings } from '@/modules/db-connections/api'
import { listMetadata } from '@/modules/sql-editor/api'
import { createQueryAccessRule, createUser, deleteUser, getUser, listQueryAccessRules, listUserDBConnections, listUserSessions, listUsers, patchUser, resetUserMFA, revokeQueryAccessRule, revokeUserSession, revokeUserSessions, updateQueryAccessRule } from '@/modules/users/api'
import type { QueryAccessRule } from '@/modules/users/api'
import type { AccountSession } from '@/modules/account/api'
import { ApiError } from '@/shared/api/client'
import { useAuth } from '@/shared/auth/AuthContext'
import { formatDateTime } from '@/shared/lib/format'
import type { AuthGroup, CurrentUser } from '@/shared/types/auth'
import type { AuthGroupDetail } from '@/shared/types/authGroup'
import type { DBConnection, DBConnectionBindings } from '@/shared/types/dbConnection'
import type { UserDetail, UserSummary } from '@/shared/types/user'
import type { QueryAccessEffect } from '@/shared/types/ticket'
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog'
import { DropdownSelect } from '@/shared/ui/DropdownSelect'
import type { DropdownOptionGroup } from '@/shared/ui/DropdownSelect'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { Pagination } from '@/shared/ui/Pagination'
import { PageTabs } from '@/shared/ui/PageTabs'
import { SearchInput } from '@/shared/ui/SearchInput'
import { useToast } from '@/shared/ui/ToastContext'
import {
  DataTable,
  DataTableBody,
  DataTableCell,
  DataTableHead,
  DataTableHeaderCell,
  DataTableRow,
  DataTableScroll,
  DataTableSurface,
} from '@/shared/ui/DataTable'

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
  { key: 'tickets.read', module: 'Tickets', action: 'Read', label: 'Tickets Read', description: 'Enter the Tickets workspace and view tickets within the allowed visibility scope.' },
  { key: 'tickets.apply', module: 'Tickets', action: 'Apply', label: 'Tickets Apply', description: 'Create DDL, DML, Redis, and Query Access tickets.' },
  { key: 'tickets.review', module: 'Tickets', action: 'Review', label: 'Tickets Review', description: 'Review DDL, DML, Redis, and Query Access tickets when assigned by approval policy.' },
  { key: 'tickets.execute', module: 'Tickets', action: 'Execute', label: 'Tickets Execute', description: 'Execute approved DDL, DML, and Redis tickets.' },
  { key: 'sql_editor.read', module: 'SQL Editor', action: 'Read', label: 'SQL Editor Read', description: 'Enter the SQL Editor workspace.' },
  { key: 'sql_editor.query', module: 'SQL Editor', action: 'Query', label: 'SQL Editor Query', description: 'Run queries and browse database objects in SQL Editor.' },
  { key: 'sql_editor.export', module: 'SQL Editor', action: 'Export', label: 'SQL Editor Export', description: 'Export the current query result.' },
  { key: 'sql_editor.export_review', module: 'SQL Editor', action: 'Export Review', label: 'Export Review', description: 'Review SQL export requests.' },
  { key: 'sql_editor.sensitive_apply', module: 'SQL Editor', action: 'Sensitive Apply', label: 'Sensitive Apply', description: 'Request temporary sensitive data access.' },
  { key: 'sql_editor.sensitive_review', module: 'SQL Editor', action: 'Sensitive Review', label: 'Sensitive Review', description: 'Review or revoke sensitive data access requests.' },
  { key: 'sql_editor.sensitive_execute', module: 'SQL Editor', action: 'Sensitive Execute', label: 'Sensitive Execute', description: 'Execute sensitive data access requests.' },
  { key: 'scheduled_sql_reports.read', module: 'Scheduled SQL Reports', action: 'Read', label: 'Scheduled Reports Read', description: 'Enter Scheduled SQL Reports and view report definitions and run history.' },
  { key: 'scheduled_sql_reports.write', module: 'Scheduled SQL Reports', action: 'Write', label: 'Scheduled Reports Write', description: 'Create, update, enable, disable, and delete scheduled SQL reports.' },
  { key: 'global.sensitive', module: 'Global', action: 'Sensitive', label: 'Global Sensitive', description: 'Bypass masking rules permanently to view sensitive data.' },
]
const PAGE_SIZE = 20
const SESSION_PAGE_SIZE = 5
const CUSTOM_SCOPE_VALUE = '__custom__'

const PERMISSION_INDEX = new Map(PERMISSION_METADATA.map((item) => [item.key, item] as const))

type ViewMode = 'users' | 'auth-groups' | 'resources' | 'query-access'

type DrawerState =
  | { mode: 'create-user' }
  | { mode: 'edit-user'; userId: number }
  | { mode: 'create-auth-group' }
  | { mode: 'edit-auth-group'; authGroupKey: AuthGroup }
  | null

type UserDraft = {
  username: string
  email: string
  larkRecipient: string
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

type ConfirmState =
  | {
      kind: 'create-user' | 'update-user' | 'delete-user' | 'create-auth-group' | 'update-auth-group' | 'delete-auth-group'
      title: string
      lines: string[]
      confirmLabel: string
      tone?: 'default' | 'danger'
    }
  | null

const EMPTY_USER_DRAFT: UserDraft = {
  username: '',
  email: '',
  larkRecipient: '',
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

const QUERY_ACCESS_DURATION_OPTIONS = [
  { value: String(24 * 60), label: '1 day' },
  { value: String(7 * 24 * 60), label: '1 week' },
  { value: String(30 * 24 * 60), label: '1 month' },
  { value: String(365 * 24 * 60), label: '1 year' },
  { value: String(3 * 365 * 24 * 60), label: '3 years' },
]

type QueryAccessRuleDraft = {
  subjectType: 'user' | 'auth_group'
  subjectID: string
  effect: QueryAccessEffect
  connectionID: string
  databasePattern: string
  tablePattern: string
  durationMinutes: string
}

const EMPTY_QUERY_ACCESS_RULE_DRAFT: QueryAccessRuleDraft = {
  subjectType: 'user',
  subjectID: '',
  effect: 'allow',
  connectionID: '',
  databasePattern: '*',
  tablePattern: '*',
  durationMinutes: String(24 * 60),
}

function scopeSelectValue(pattern: string, options: string[]) {
  if (pattern === '*') {
    return '*'
  }
  return options.includes(pattern) ? pattern : CUSTOM_SCOPE_VALUE
}

function formatConnectionGroupLabel(dbType: string) {
  switch (dbType) {
    case 'mysql':
      return 'MySQL'
    case 'postgres':
      return 'PgSQL'
    case 'redis':
      return 'Redis'
    default:
      return dbType.toUpperCase()
  }
}

function getConnectionGroupOrder(dbType: string) {
  switch (dbType) {
    case 'mysql':
      return 1
    case 'postgres':
      return 2
    case 'redis':
      return 3
    default:
      return 99
  }
}

function groupConnectionOptions(connections: DBConnection[]): DropdownOptionGroup[] {
  const groups = new Map<string, DBConnection[]>()
  connections.forEach((connection) => {
    groups.set(connection.db_type, [...(groups.get(connection.db_type) ?? []), connection])
  })

  return Array.from(groups.entries())
    .sort(([leftType], [rightType]) => {
      const orderDiff = getConnectionGroupOrder(leftType) - getConnectionGroupOrder(rightType)
      return orderDiff || formatConnectionGroupLabel(leftType).localeCompare(formatConnectionGroupLabel(rightType))
    })
    .map(([dbType, groupConnections]) => ({
      label: formatConnectionGroupLabel(dbType),
      options: [...groupConnections]
        .sort((left, right) => left.name.localeCompare(right.name))
        .map((connection) => ({
          value: String(connection.id),
          label: getConnectionLabel(connection.id, connections),
        })),
    }))
}

function minutesUntil(value?: string | null) {
  if (!value) {
    return String(24 * 60)
  }
  const minutes = Math.ceil((new Date(value).getTime() - Date.now()) / 60000)
  return String(Math.max(1, minutes))
}

export function UsersPage({ initialView = 'users' }: { initialView?: ViewMode }) {
  const { user: currentUser } = useAuth()
  const { pushToast } = useToast()
  const navigate = useNavigate()
  const [viewMode, setViewMode] = useState<ViewMode>(initialView)
  const [usersOffset, setUsersOffset] = useState(0)
  const [authGroupsOffset, setAuthGroupsOffset] = useState(0)
  const [resourcesOffset, setResourcesOffset] = useState(0)
  const [users, setUsers] = useState<UserSummary[]>([])
  const [authGroups, setAuthGroups] = useState<AuthGroupDetail[]>([])
  const [connections, setConnections] = useState<DBConnection[]>([])
  const [connectionBindings, setConnectionBindings] = useState<Record<number, DBConnectionBindings>>({})
  const [queryAccessRules, setQueryAccessRules] = useState<QueryAccessRule[]>([])
  const [queryAccessOffset, setQueryAccessOffset] = useState(0)
  const [queryAccessRuleDraft, setQueryAccessRuleDraft] = useState<QueryAccessRuleDraft>(EMPTY_QUERY_ACCESS_RULE_DRAFT)
  const [editingQueryAccessRuleID, setEditingQueryAccessRuleID] = useState<number | null>(null)
  const [queryAccessDatabases, setQueryAccessDatabases] = useState<string[]>([])
  const [queryAccessTables, setQueryAccessTables] = useState<string[]>([])
  const [queryAccessMetadataLoading, setQueryAccessMetadataLoading] = useState(false)
  const [savingQueryAccessRule, setSavingQueryAccessRule] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [drawerState, setDrawerState] = useState<DrawerState>(null)
  const [drawerLoading, setDrawerLoading] = useState(false)
  const [drawerError, setDrawerError] = useState('')
  const [saving, setSaving] = useState(false)
  const [selectedUser, setSelectedUser] = useState<UserDetail | null>(null)
  const [selectedUserSessions, setSelectedUserSessions] = useState<AccountSession[]>([])
  const [selectedUserSessionsOffset, setSelectedUserSessionsOffset] = useState(0)
  const [sessionsLoading, setSessionsLoading] = useState(false)
  const [sessionsActing, setSessionsActing] = useState<number | 'all' | null>(null)
  const [mfaResetting, setMFAResetting] = useState(false)
  const [selectedAuthGroup, setSelectedAuthGroup] = useState<AuthGroupDetail | null>(null)
  const [userDraft, setUserDraft] = useState<UserDraft>(EMPTY_USER_DRAFT)
  const [authGroupDraft, setAuthGroupDraft] = useState<AuthGroupDraft>(EMPTY_AUTH_GROUP_DRAFT)
  const [pendingUserAuthGroup, setPendingUserAuthGroup] = useState<AuthGroup>('')
  const [pendingAuthGroupUserID, setPendingAuthGroupUserID] = useState('')
  const [userPermissionSearch, setUserPermissionSearch] = useState('')
  const [userDBScopeSearch, setUserDBScopeSearch] = useState('')
  const [authGroupPermissionSearch, setAuthGroupPermissionSearch] = useState('')
  const [authGroupDBScopeSearch, setAuthGroupDBScopeSearch] = useState('')
  const [confirmState, setConfirmState] = useState<ConfirmState>(null)

  useEffect(() => {
    void bootstrap()
  }, [])

  useEffect(() => {
    setViewMode(initialView)
  }, [initialView])

  useEffect(() => {
    const connectionID = Number(queryAccessRuleDraft.connectionID)
    if (!connectionID) {
      setQueryAccessDatabases([])
      setQueryAccessTables([])
      return
    }
    let active = true
    setQueryAccessMetadataLoading(true)
    listMetadata(connectionID)
      .then((response) => {
        if (!active) {
          return
        }
        const databases = response.items
          .filter((item) => item.kind === 'database' || item.kind === 'schema' || item.kind === 'redis_db')
          .map((item) => item.database || item.schema || item.name)
          .filter((name, index, values) => name.trim() !== '' && values.indexOf(name) === index)
        setQueryAccessDatabases(databases)
      })
      .catch(() => {
        if (active) {
          setQueryAccessDatabases([])
        }
      })
      .finally(() => {
        if (active) {
          setQueryAccessMetadataLoading(false)
        }
      })
    return () => {
      active = false
    }
  }, [queryAccessRuleDraft.connectionID])

  useEffect(() => {
    const connectionID = Number(queryAccessRuleDraft.connectionID)
    const database = queryAccessRuleDraft.databasePattern.trim()
    if (!connectionID || database === '' || database === '*') {
      setQueryAccessTables([])
      return
    }
    let active = true
    setQueryAccessMetadataLoading(true)
    listMetadata(connectionID, { database })
      .then((response) => {
        if (!active) {
          return
        }
        const tables = response.items
          .filter((item) => item.kind === 'table')
          .map((item) => item.name)
          .filter((name, index, values) => name.trim() !== '' && values.indexOf(name) === index)
        setQueryAccessTables(tables)
      })
      .catch(() => {
        if (active) {
          setQueryAccessTables([])
        }
      })
      .finally(() => {
        if (active) {
          setQueryAccessMetadataLoading(false)
        }
      })
    return () => {
      active = false
    }
  }, [queryAccessRuleDraft.connectionID, queryAccessRuleDraft.databasePattern])

  async function bootstrap() {
    setLoading(true)
    setError('')
    try {
      const [usersResponse, connectionsResponse, authGroupDetails] = await Promise.all([
        listUsers(),
        listUserDBConnections(),
        loadAuthGroupDetails(),
      ])
      const queryAccessRulesResponse = await listQueryAccessRules()
      const bindingsEntries = await Promise.all(
        connectionsResponse.connections.map(async (connection) => {
          const bindings = await getDBConnectionBindings(connection.id)
          return [connection.id, bindings] as const
        }),
      )
      setUsers(usersResponse.users)
      setConnections(connectionsResponse.connections)
      setAuthGroups(authGroupDetails)
      setQueryAccessRules(queryAccessRulesResponse.rules)
      setConnectionBindings(Object.fromEntries(bindingsEntries))
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
    const [usersResponse, authGroupDetails, queryAccessRulesResponse] = await Promise.all([listUsers(), loadAuthGroupDetails(), listQueryAccessRules()])
    setUsers(usersResponse.users)
    setAuthGroups(authGroupDetails)
    setQueryAccessRules(queryAccessRulesResponse.rules)
  }

  const authGroupLabelMap = useMemo(() => {
    return new Map(authGroups.map((group) => [group.name, group.label] as const))
  }, [authGroups])

  const selectedUserIsProtected = selectedUser?.protected === true
  const selectedAuthGroupIsProtected = selectedAuthGroup?.protected === true
  const currentUserCanManageProtectedAccess = canManageProtectedAccess(currentUser)
  const selectedUserSecurityLocked = selectedUserIsProtected && !currentUserCanManageProtectedAccess
  const selectedAuthGroupSecurityLocked = selectedAuthGroupIsProtected && !currentUserCanManageProtectedAccess
  const protectedAuthGroupNames = useMemo(() => new Set(authGroups.filter((group) => group.protected === true).map((group) => group.name)), [authGroups])
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
    setSelectedUserSessions([])
    setSelectedUserSessionsOffset(0)
    setSessionsLoading(false)
    setSessionsActing(null)
    setMFAResetting(false)
    setSelectedAuthGroup(null)
    setUserDraft(EMPTY_USER_DRAFT)
    setAuthGroupDraft(EMPTY_AUTH_GROUP_DRAFT)
    setPendingUserAuthGroup('')
    setPendingAuthGroupUserID('')
    setUserPermissionSearch('')
    setUserDBScopeSearch('')
    setAuthGroupPermissionSearch('')
    setAuthGroupDBScopeSearch('')
    setConfirmState(null)
  }

  function openCreateUserDrawer() {
    setDrawerState({ mode: 'create-user' })
    setDrawerError('')
    setSelectedUser(null)
    setSelectedUserSessions([])
    setSelectedUserSessionsOffset(0)
    setSelectedAuthGroup(null)
    setConfirmState(null)
    setUserDraft(EMPTY_USER_DRAFT)
    setPendingUserAuthGroup(authGroupOptions[0] ?? '')
  }

  async function openEditUserDrawer(userId: number) {
    setDrawerState({ mode: 'edit-user', userId })
    setDrawerLoading(true)
    setDrawerError('')
    setConfirmState(null)
    setSelectedUser(null)
    setSelectedUserSessions([])
    setSelectedAuthGroup(null)
    try {
      const [detail, sessionsResponse] = await Promise.all([getUser(userId), listUserSessions(userId)])
      setSelectedUser(detail)
      setSelectedUserSessions(sessionsResponse.sessions)
      setSelectedUserSessionsOffset(0)
      setUserDraft({
        username: detail.username,
        email: detail.email,
        larkRecipient: detail.lark_recipient,
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

  async function refreshSelectedUserSessions(userId: number) {
    setSessionsLoading(true)
    setDrawerError('')
    try {
      const response = await listUserSessions(userId)
      setSelectedUserSessions(response.sessions)
      setSelectedUserSessionsOffset((current) => Math.min(current, Math.max(0, Math.floor(Math.max(response.sessions.length - 1, 0) / SESSION_PAGE_SIZE) * SESSION_PAGE_SIZE)))
    } catch (loadError) {
      setDrawerError(loadError instanceof ApiError ? loadError.message : 'Failed to load user sessions.')
    } finally {
      setSessionsLoading(false)
    }
  }

  async function handleRevokeUserSession(sessionID: number) {
    if (drawerState?.mode !== 'edit-user') {
      return
    }
    setSessionsActing(sessionID)
    setDrawerError('')
    try {
      await revokeUserSession(drawerState.userId, sessionID)
      await refreshSelectedUserSessions(drawerState.userId)
      pushToast('Session revoked', 'success')
    } catch (revokeError) {
      setDrawerError(revokeError instanceof ApiError ? revokeError.message : 'Failed to revoke user session.')
    } finally {
      setSessionsActing(null)
    }
  }

  async function handleRevokeUserSessions() {
    if (drawerState?.mode !== 'edit-user') {
      return
    }
    setSessionsActing('all')
    setDrawerError('')
    try {
      await revokeUserSessions(drawerState.userId)
      await refreshSelectedUserSessions(drawerState.userId)
      pushToast('All user sessions revoked', 'success')
    } catch (revokeError) {
      setDrawerError(revokeError instanceof ApiError ? revokeError.message : 'Failed to revoke user sessions.')
    } finally {
      setSessionsActing(null)
    }
  }

  async function handleResetUserMFA() {
    if (drawerState?.mode !== 'edit-user') {
      return
    }
    setMFAResetting(true)
    setDrawerError('')
    try {
      await resetUserMFA(drawerState.userId)
      await refreshSelectedUserSessions(drawerState.userId)
      pushToast('MFA reset. The user must set up MFA on next sign-in.', 'success')
    } catch (resetError) {
      setDrawerError(resetError instanceof ApiError ? resetError.message : 'Failed to reset user MFA.')
    } finally {
      setMFAResetting(false)
    }
  }

  function openCreateAuthGroupDrawer() {
    setDrawerState({ mode: 'create-auth-group' })
    setDrawerError('')
    setSelectedUser(null)
    setSelectedAuthGroup(null)
    setConfirmState(null)
    setAuthGroupDraft(EMPTY_AUTH_GROUP_DRAFT)
    setPendingAuthGroupUserID('')
  }

  async function openEditAuthGroupDrawer(authGroupKey: AuthGroup) {
    setDrawerState({ mode: 'edit-auth-group', authGroupKey })
    setDrawerLoading(true)
    setDrawerError('')
    setConfirmState(null)
    setSelectedUser(null)
    setSelectedAuthGroup(null)
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

  async function handleSaveUser(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!drawerState) {
      return
    }

    if (drawerState.mode === 'create-user') {
      const createSummary = [
        `Username: ${userDraft.username}`,
        `Email: ${userDraft.email}`,
        `Lark Open ID: ${userDraft.larkRecipient || 'Not set'}`,
        `Auth Groups: ${formatAuthGroupList(userDraft.authGroups, authGroupLabelMap)}`,
        `Direct Permissions: ${formatPermissionList(userDraft.directPermissions)}`,
        `Direct DB Scope: ${formatConnectionIDs(userDraft.directDBConnectionIDs, connections)}`,
      ]
      setConfirmState({
        kind: 'create-user',
        title: 'Confirm Create User',
        lines: createSummary,
        confirmLabel: 'Create User',
      })
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
    setConfirmState({
      kind: userDraft.pendingDelete ? 'delete-user' : 'update-user',
      title: userDraft.pendingDelete ? 'Confirm Delete User' : 'Confirm Save User Changes',
      lines: changeSummary,
      confirmLabel: userDraft.pendingDelete ? 'Delete User' : 'Save Changes',
      tone: userDraft.pendingDelete ? 'danger' : 'default',
    })
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
        `Permissions: ${formatPermissionList(authGroupDraft.permissions)}`,
        `DB Scope: ${formatConnectionIDs(authGroupDraft.dbConnectionIDs, connections)}`,
      ]
      setConfirmState({
        kind: 'create-auth-group',
        title: 'Confirm Create Auth Group',
        lines: createSummary,
        confirmLabel: 'Create Auth Group',
      })
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
    setConfirmState({
      kind: authGroupDraft.pendingDelete ? 'delete-auth-group' : 'update-auth-group',
      title: authGroupDraft.pendingDelete ? 'Confirm Delete Auth Group' : 'Confirm Save Auth Group Changes',
      lines: changeSummary,
      confirmLabel: authGroupDraft.pendingDelete ? 'Delete Auth Group' : 'Save Changes',
      tone: authGroupDraft.pendingDelete ? 'danger' : 'default',
    })
  }

  async function handleConfirmSubmit() {
    if (!confirmState || !drawerState) {
      return
    }

    setSaving(true)
    setDrawerError('')

    try {
      if (confirmState.kind === 'create-user') {
        const created = await createUser({
          username: userDraft.username,
          email: userDraft.email,
          lark_recipient: userDraft.larkRecipient.trim(),
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
        setConfirmState(null)
        await openEditUserDrawer(created.id)
        return
      }

      if (confirmState.kind === 'update-user' || confirmState.kind === 'delete-user') {
        if (drawerState.mode !== 'edit-user' || !selectedUser) {
          return
        }
        if (confirmState.kind === 'delete-user') {
          await deleteUser(drawerState.userId)
          await reloadAll()
          pushToast('User deleted', 'success')
          setConfirmState(null)
          closeDrawer()
          return
        }

        const payload: {
          username?: string
          email?: string
          lark_recipient?: string
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
          payload.lark_recipient = userDraft.larkRecipient.trim()
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
        setConfirmState(null)
        await openEditUserDrawer(drawerState.userId)
        return
      }

      if (confirmState.kind === 'create-auth-group') {
        const created = await createAuthGroup({
          name: authGroupDraft.name,
          description: authGroupDraft.description,
          user_ids: authGroupDraft.userIDs,
          permissions: authGroupDraft.permissions,
          db_connection_ids: authGroupDraft.dbConnectionIDs,
        })
        await reloadAll()
        pushToast('Auth group created', 'success')
        setConfirmState(null)
        await openEditAuthGroupDrawer(created.name as AuthGroup)
        return
      }

      if (confirmState.kind === 'update-auth-group' || confirmState.kind === 'delete-auth-group') {
        if (drawerState.mode !== 'edit-auth-group' || !selectedAuthGroup) {
          return
        }
        if (confirmState.kind === 'delete-auth-group') {
          await deleteAuthGroup(drawerState.authGroupKey)
          await reloadAll()
          pushToast('Auth group deleted', 'success')
          setConfirmState(null)
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
        setConfirmState(null)
        await openEditAuthGroupDrawer(drawerState.authGroupKey)
      }
    } catch (saveError) {
      const message =
        saveError instanceof ApiError
          ? saveError.message
          : confirmState.kind === 'create-user'
            ? 'Failed to create the user.'
            : confirmState.kind === 'update-user'
              ? 'Failed to update the user.'
              : confirmState.kind === 'delete-user'
                ? 'Failed to delete the user.'
                : confirmState.kind === 'create-auth-group'
                  ? 'Failed to create the auth group.'
                  : confirmState.kind === 'update-auth-group'
                    ? 'Failed to update the auth group.'
                    : 'Failed to delete the auth group.'
      setConfirmState(null)
      setDrawerError(message)
      pushToast(message, 'error', { placement: 'center', durationMs: 4200 })
    } finally {
      setSaving(false)
    }
  }

  async function handleCreateQueryAccessRule(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (
      queryAccessRuleDraft.subjectID === '' ||
      queryAccessRuleDraft.connectionID === '' ||
      queryAccessRuleDraft.databasePattern.trim() === '' ||
      queryAccessRuleDraft.tablePattern.trim() === ''
    ) {
      setError('Subject, DB connection, database pattern, and table pattern are required.')
      return
    }
    setSavingQueryAccessRule(true)
    setError('')
    try {
      const payload = {
        subject_type: queryAccessRuleDraft.subjectType,
        subject_id: Number(queryAccessRuleDraft.subjectID),
        effect: queryAccessRuleDraft.effect,
        connection_id: Number(queryAccessRuleDraft.connectionID),
        database_pattern: queryAccessRuleDraft.databasePattern.trim(),
        table_pattern: queryAccessRuleDraft.tablePattern.trim(),
        duration_minutes: Number(queryAccessRuleDraft.durationMinutes),
      }
      if (editingQueryAccessRuleID == null) {
        await createQueryAccessRule(payload)
      } else {
        await updateQueryAccessRule(editingQueryAccessRuleID, payload)
      }
      await reloadAll()
      setQueryAccessRuleDraft(EMPTY_QUERY_ACCESS_RULE_DRAFT)
      setEditingQueryAccessRuleID(null)
      pushToast(editingQueryAccessRuleID == null ? 'Query access rule created' : 'Query access rule updated', 'success')
    } catch (createError) {
      setError(createError instanceof ApiError ? createError.message : 'Failed to save query access rule.')
    } finally {
      setSavingQueryAccessRule(false)
    }
  }

  function handleEditQueryAccessRule(rule: QueryAccessRule) {
    setEditingQueryAccessRuleID(rule.id)
    setQueryAccessRuleDraft({
      subjectType: rule.subject_type,
      subjectID: String(rule.subject_id),
      effect: rule.effect,
      connectionID: String(rule.connection_id),
      databasePattern: rule.database_pattern,
      tablePattern: rule.table_pattern,
      durationMinutes: minutesUntil(rule.expires_at),
    })
    setError('')
  }

  function handleCancelEditQueryAccessRule() {
    setEditingQueryAccessRuleID(null)
    setQueryAccessRuleDraft(EMPTY_QUERY_ACCESS_RULE_DRAFT)
  }

  async function handleRevokeQueryAccessRule(ruleID: number) {
    setSavingQueryAccessRule(true)
    setError('')
    try {
      await revokeQueryAccessRule(ruleID)
      await reloadAll()
      pushToast('Query access rule revoked', 'success')
    } catch (revokeError) {
      setError(revokeError instanceof ApiError ? revokeError.message : 'Failed to revoke query access rule.')
    } finally {
      setSavingQueryAccessRule(false)
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
  const pagedResources = useMemo(
    () => connections.slice(resourcesOffset, resourcesOffset + PAGE_SIZE),
    [connections, resourcesOffset],
  )
  const pagedQueryAccessRules = useMemo(
    () => queryAccessRules.slice(queryAccessOffset, queryAccessOffset + PAGE_SIZE),
    [queryAccessOffset, queryAccessRules],
  )
  const databaseScopeValue = scopeSelectValue(queryAccessRuleDraft.databasePattern, queryAccessDatabases)
  const tableScopeValue = scopeSelectValue(queryAccessRuleDraft.tablePattern, queryAccessTables)
  const databaseScopeOptions = [
    { value: '*', label: 'All databases (*)' },
    ...queryAccessDatabases.map((database) => ({ value: database, label: database })),
    { value: CUSTOM_SCOPE_VALUE, label: 'Custom...' },
  ]
  const tableScopeOptions = [
    { value: '*', label: 'All tables (*)' },
    ...queryAccessTables.map((table) => ({ value: table, label: table })),
    { value: CUSTOM_SCOPE_VALUE, label: 'Custom...' },
  ]
  const groupedQueryAccessConnectionOptions = useMemo(() => groupConnectionOptions(connections), [connections])
  const pagedSelectedUserSessions = useMemo(
    () => selectedUserSessions.slice(selectedUserSessionsOffset, selectedUserSessionsOffset + SESSION_PAGE_SIZE),
    [selectedUserSessions, selectedUserSessionsOffset],
  )
  const currentPageCount = viewMode === 'users'
    ? pagedUsers.length
    : viewMode === 'auth-groups'
      ? pagedAuthGroups.length
      : viewMode === 'resources'
        ? pagedResources.length
        : pagedQueryAccessRules.length
  const currentTotal = viewMode === 'users'
    ? users.length
    : viewMode === 'auth-groups'
      ? authGroups.length
      : viewMode === 'resources'
        ? connections.length
        : queryAccessRules.length
  const currentOffset = viewMode === 'users'
    ? usersOffset
    : viewMode === 'auth-groups'
      ? authGroupsOffset
      : viewMode === 'resources'
        ? resourcesOffset
        : queryAccessOffset
  const currentOffsetSetter = viewMode === 'users'
    ? setUsersOffset
    : viewMode === 'auth-groups'
      ? setAuthGroupsOffset
      : viewMode === 'resources'
        ? setResourcesOffset
        : setQueryAccessOffset

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

  useEffect(() => {
    if (resourcesOffset > 0 && resourcesOffset >= connections.length) {
      setResourcesOffset(Math.max(0, Math.floor((Math.max(connections.length - 1, 0)) / PAGE_SIZE) * PAGE_SIZE))
    }
  }, [connections.length, resourcesOffset])

  useEffect(() => {
    if (queryAccessOffset > 0 && queryAccessOffset >= queryAccessRules.length) {
      setQueryAccessOffset(Math.max(0, Math.floor((Math.max(queryAccessRules.length - 1, 0)) / PAGE_SIZE) * PAGE_SIZE))
    }
  }, [queryAccessOffset, queryAccessRules.length])

  return (
    <>
      <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
      <div className="flex flex-wrap items-end justify-between gap-3 border-b border-border px-1">
        <PageTabs
          className="min-w-0 flex-1 border-b-0 px-0"
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
            {
              key: 'resources',
              label: 'Resources',
              active: viewMode === 'resources',
              onClick: () => {
                setViewMode('resources')
                navigate('/users/resources')
              },
            },
            {
              key: 'query-access',
              label: 'Query Access',
              active: viewMode === 'query-access',
              onClick: () => {
                setViewMode('query-access')
                navigate('/users/query-access')
              },
            },
          ]}
        />
        <div className="flex flex-wrap justify-end gap-2 pb-2">
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
        </div>
      </div>

      {loading ? (
        <LoadingBlock message="Loading RBAC workspace..." className="min-h-[320px] rounded-xl border-border bg-panel" />
      ) : viewMode === 'users' ? (
            <DataTableSurface>
              <DataTableScroll>
                <DataTable>
                  <DataTableHead>
                    <tr>
                      <DataTableHeaderCell>Username</DataTableHeaderCell>
                      <DataTableHeaderCell>Auth Groups</DataTableHeaderCell>
                      <DataTableHeaderCell>Permissions</DataTableHeaderCell>
                      <DataTableHeaderCell>DB Scope</DataTableHeaderCell>
                      <DataTableHeaderCell>Status</DataTableHeaderCell>
                      <DataTableHeaderCell>Created</DataTableHeaderCell>
                      <DataTableHeaderCell>Updated</DataTableHeaderCell>
                      <DataTableHeaderCell>Action</DataTableHeaderCell>
                    </tr>
                  </DataTableHead>
                  <DataTableBody>
                    {pagedUsers.map((user) => {
                      const userHasAllPermissions = hasAllPermissionsUser(user, authGroups)
                      return (
                      <DataTableRow key={user.id}>
                        <DataTableCell>
                          <div className="flex items-center gap-2">
                            <div>
                              <p>{user.username}</p>
                            </div>
                            {user.protected ? <Tag label="protected" tone="danger" /> : null}
                          </div>
                        </DataTableCell>
                        <DataTableCell>
                          <BindingTags items={user.auth_groups.map((group) => authGroupLabelMap.get(group) ?? group)} emptyLabel="—" />
                        </DataTableCell>
                        <DataTableCell>
                          <BindingTags
                            items={userHasAllPermissions ? ['All Permissions'] : (user.direct_permissions ?? []).map((permission) => getPermissionMeta(permission).label)}
                            emptyLabel="—"
                          />
                        </DataTableCell>
                        <DataTableCell>
                          <BindingTags
                            items={userHasAllPermissions ? ['All DB Scope'] : (user.db_connection_ids ?? []).map((connectionId) => getConnectionLabel(connectionId, connections))}
                            emptyLabel="—"
                          />
                        </DataTableCell>
                        <DataTableCell>
                          <Tag label={user.is_active ? 'active' : 'disabled'} tone={user.is_active ? 'success' : 'danger'} />
                        </DataTableCell>
                        <DataTableCell>{formatDateTime(user.created_at)}</DataTableCell>
                        <DataTableCell>{formatDateTime(user.updated_at)}</DataTableCell>
                        <DataTableCell>
                          <button
                            type="button"
                            onClick={() => void openEditUserDrawer(user.id)}
                            className="inline-flex h-8 items-center justify-center whitespace-nowrap rounded-md border border-border bg-panel-soft px-3 text-[12px] font-semibold text-ink transition hover:bg-page"
                          >
                            Manage
                          </button>
                        </DataTableCell>
                      </DataTableRow>
                      )
                    })}
                  </DataTableBody>
                </DataTable>
              </DataTableScroll>
            </DataTableSurface>
          ) : viewMode === 'auth-groups' ? (
            <DataTableSurface>
              <DataTableScroll>
                <DataTable>
                  <DataTableHead>
                    <tr>
                      <DataTableHeaderCell>Auth Group</DataTableHeaderCell>
                      <DataTableHeaderCell>Users</DataTableHeaderCell>
                      <DataTableHeaderCell>Permissions</DataTableHeaderCell>
                      <DataTableHeaderCell>DB Scope</DataTableHeaderCell>
                      <DataTableHeaderCell>Created</DataTableHeaderCell>
                      <DataTableHeaderCell>Updated</DataTableHeaderCell>
                      <DataTableHeaderCell>Action</DataTableHeaderCell>
                    </tr>
                  </DataTableHead>
                  <DataTableBody>
                    {pagedAuthGroups.map((group) => (
                      <DataTableRow key={group.name}>
                        <DataTableCell>
                          <p>{group.label}</p>
                        </DataTableCell>
                        <DataTableCell>
                          <BindingTags items={group.users.map((user) => user.username)} emptyLabel="—" />
                        </DataTableCell>
                        <DataTableCell>
                          <BindingTags
                            items={group.all_permissions ? ['All Permissions'] : (group.permissions ?? []).map((permission) => getPermissionMeta(permission).label)}
                            emptyLabel="—"
                          />
                        </DataTableCell>
                        <DataTableCell>
                          <BindingTags
                            items={group.all_permissions ? ['All DB Scope'] : (group.db_connection_ids ?? []).map((connectionId) => getConnectionLabel(connectionId, connections))}
                            emptyLabel="—"
                          />
                        </DataTableCell>
                        <DataTableCell>{formatDateTime(group.created_at ?? '')}</DataTableCell>
                        <DataTableCell>{formatDateTime(group.updated_at ?? '')}</DataTableCell>
                        <DataTableCell>
                          <button
                            type="button"
                            onClick={() => void openEditAuthGroupDrawer(group.name as AuthGroup)}
                            className="inline-flex h-8 items-center justify-center whitespace-nowrap rounded-md border border-border bg-panel-soft px-3 text-[12px] font-semibold text-ink transition hover:bg-page"
                          >
                            Manage
                          </button>
                        </DataTableCell>
                      </DataTableRow>
                    ))}
                  </DataTableBody>
                </DataTable>
              </DataTableScroll>
            </DataTableSurface>
          ) : viewMode === 'resources' ? (
            <DataTableSurface>
              <DataTableScroll>
                <DataTable>
                  <DataTableHead>
                    <tr>
                      <DataTableHeaderCell>Resource</DataTableHeaderCell>
                      <DataTableHeaderCell>Type</DataTableHeaderCell>
                      <DataTableHeaderCell>Direct Users</DataTableHeaderCell>
                      <DataTableHeaderCell>Auth Groups</DataTableHeaderCell>
                      <DataTableHeaderCell>Effective Users</DataTableHeaderCell>
                    </tr>
                  </DataTableHead>
                  <DataTableBody>
                    {pagedResources.map((connection) => {
                      const bindings = connectionBindings[connection.id]
                      return (
                        <DataTableRow key={connection.id}>
                          <DataTableCell>
                            <p className="whitespace-nowrap">{connection.name}</p>
                          </DataTableCell>
                          <DataTableCell>{formatDBType(connection.db_type)}</DataTableCell>
                          <DataTableCell>
                            <BindingTags items={bindings?.direct_users.map((user) => user.username) ?? []} emptyLabel="—" maxVisible={5} />
                          </DataTableCell>
                          <DataTableCell>
                            <BindingTags items={bindings?.auth_groups.map((group) => group.name || group.group_key) ?? []} emptyLabel="—" maxVisible={5} />
                          </DataTableCell>
                          <DataTableCell>
                            <BindingTags items={bindings?.effective_users.map((user) => user.username) ?? []} emptyLabel="—" maxVisible={5} />
                          </DataTableCell>
                        </DataTableRow>
                      )
                    })}
                  </DataTableBody>
                </DataTable>
              </DataTableScroll>
            </DataTableSurface>
          ) : (
            <section className="grid gap-3">
              <form className="rounded-xl border border-border bg-panel p-4 shadow-soft" onSubmit={handleCreateQueryAccessRule}>
                <div className="mb-4 flex items-center gap-2">
                  <KeyRound className="h-4 w-4 text-accent" />
                  <div>
                    <p className="text-[13px] font-semibold text-ink">{editingQueryAccessRuleID == null ? 'Manual Query Access Rule' : `Edit Query Access Rule #${editingQueryAccessRuleID}`}</p>
                    <p className="text-[12px] text-muted">Create fallback rules for a user or auth group. * means all; other values are exact names, not fuzzy matches.</p>
                  </div>
                </div>
                <div className="grid gap-3 xl:grid-cols-[120px_minmax(150px,220px)_100px_120px_auto]">
                  <label className="grid min-w-0 gap-1.5 text-[11px] font-semibold uppercase tracking-wide text-faint">
                    Subject Type
                    <DropdownSelect
                      ariaLabel="Query Access Subject Type"
                      value={queryAccessRuleDraft.subjectType}
                      onChange={(value) => setQueryAccessRuleDraft((current) => ({ ...current, subjectType: value as 'user' | 'auth_group', subjectID: '' }))}
                      disabled={savingQueryAccessRule}
                      options={[
                        { value: 'user', label: 'User' },
                        { value: 'auth_group', label: 'Auth Group' },
                      ]}
                    />
                  </label>
                  <label className="grid min-w-0 gap-1.5 text-[11px] font-semibold uppercase tracking-wide text-faint xl:max-w-[220px]">
                    Subject
                    <DropdownSelect
                      ariaLabel="Query Access Subject"
                      value={queryAccessRuleDraft.subjectID}
                      onChange={(value) => setQueryAccessRuleDraft((current) => ({ ...current, subjectID: value }))}
                      disabled={savingQueryAccessRule}
                      options={[
                        { value: '', label: 'Not Selected' },
                        ...(queryAccessRuleDraft.subjectType === 'user'
                          ? users.map((user) => ({ value: String(user.id), label: user.username }))
                          : authGroups.map((group) => ({ value: String(group.id ?? ''), label: group.label })).filter((item) => item.value !== '')),
                      ]}
                    />
                  </label>
                  <label className="grid min-w-0 gap-1.5 text-[11px] font-semibold uppercase tracking-wide text-faint">
                    Effect
                    <DropdownSelect
                      ariaLabel="Query Access Effect"
                      value={queryAccessRuleDraft.effect}
                      onChange={(value) => setQueryAccessRuleDraft((current) => ({ ...current, effect: value as QueryAccessEffect }))}
                      disabled={savingQueryAccessRule}
                      options={[
                        { value: 'allow', label: 'Allow' },
                        { value: 'deny', label: 'Deny' },
                      ]}
                    />
                  </label>
                  <label className="grid min-w-0 gap-1.5 text-[11px] font-semibold uppercase tracking-wide text-faint">
                    Duration
                    <DropdownSelect
                      ariaLabel="Query Access Duration"
                      value={queryAccessRuleDraft.durationMinutes}
                      onChange={(value) => setQueryAccessRuleDraft((current) => ({ ...current, durationMinutes: value }))}
                      disabled={savingQueryAccessRule}
                      options={QUERY_ACCESS_DURATION_OPTIONS}
                    />
                  </label>
                  <div className="flex items-end gap-2 xl:justify-self-start">
                    {editingQueryAccessRuleID != null ? (
                      <button
                        type="button"
                        onClick={handleCancelEditQueryAccessRule}
                        disabled={savingQueryAccessRule}
                        className="inline-flex h-9 items-center justify-center rounded-lg border border-border bg-white px-3 text-[12px] font-bold text-ink shadow-soft transition hover:bg-panel-soft disabled:cursor-not-allowed disabled:opacity-60"
                      >
                        Cancel
                      </button>
                    ) : null}
                    <button
                      type="submit"
                      disabled={savingQueryAccessRule}
                      className="inline-flex h-9 items-center justify-center gap-2 rounded-lg bg-brand px-3 text-[12px] font-bold text-white shadow-soft transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-60"
                    >
                      <Plus className="h-4 w-4" />
                      {editingQueryAccessRuleID == null ? 'Add' : 'Save'}
                    </button>
                  </div>
                </div>
                <div className="mt-3 grid gap-3 xl:grid-cols-[minmax(260px,0.9fr)_minmax(260px,1fr)_minmax(260px,1fr)]">
                  <label className="grid min-w-0 gap-1.5 text-[11px] font-semibold uppercase tracking-wide text-faint">
                    DB Connection
                    <DropdownSelect
                      ariaLabel="Query Access DB Connection"
                      value={queryAccessRuleDraft.connectionID}
                      onChange={(value) => setQueryAccessRuleDraft((current) => ({ ...current, connectionID: value, databasePattern: '*', tablePattern: '*' }))}
                      disabled={savingQueryAccessRule}
                      options={[
                        { value: '', label: 'Not Selected' },
                        ...groupedQueryAccessConnectionOptions,
                      ]}
                      menuClassName="max-h-[360px] overflow-y-auto"
                    />
                  </label>
                  <label className="grid min-w-0 gap-1.5 text-[11px] font-semibold uppercase tracking-wide text-faint">
                    Database
                    <DropdownSelect
                      ariaLabel="Query Access Database"
                      value={databaseScopeValue}
                      onChange={(value) => setQueryAccessRuleDraft((current) => ({
                        ...current,
                        databasePattern: value === CUSTOM_SCOPE_VALUE ? '' : value,
                        tablePattern: '*',
                      }))}
                      disabled={savingQueryAccessRule || queryAccessRuleDraft.connectionID === ''}
                      options={databaseScopeOptions}
                      menuClassName="max-h-[320px] overflow-y-auto"
                    />
                    {databaseScopeValue === CUSTOM_SCOPE_VALUE ? (
                      <input
                        value={queryAccessRuleDraft.databasePattern}
                        onChange={(event) => setQueryAccessRuleDraft((current) => ({ ...current, databasePattern: event.target.value, tablePattern: '*' }))}
                        className="h-9 rounded-lg border border-border bg-panel-soft px-3 text-[13px] font-medium normal-case tracking-normal text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                        placeholder="Exact database name"
                        disabled={savingQueryAccessRule}
                      />
                    ) : null}
                  </label>
                  <label className="grid min-w-0 gap-1.5 text-[11px] font-semibold uppercase tracking-wide text-faint">
                    Table
                    <DropdownSelect
                      ariaLabel="Query Access Table"
                      value={tableScopeValue}
                      onChange={(value) => setQueryAccessRuleDraft((current) => ({ ...current, tablePattern: value === CUSTOM_SCOPE_VALUE ? '' : value }))}
                      disabled={savingQueryAccessRule || queryAccessRuleDraft.connectionID === '' || queryAccessRuleDraft.databasePattern.trim() === ''}
                      options={tableScopeOptions}
                      menuClassName="max-h-[320px] overflow-y-auto"
                    />
                    {tableScopeValue === CUSTOM_SCOPE_VALUE ? (
                      <input
                        value={queryAccessRuleDraft.tablePattern}
                        onChange={(event) => setQueryAccessRuleDraft((current) => ({ ...current, tablePattern: event.target.value }))}
                        className="h-9 rounded-lg border border-border bg-panel-soft px-3 text-[13px] font-medium normal-case tracking-normal text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
                        placeholder="Exact table name"
                        disabled={savingQueryAccessRule}
                      />
                    ) : null}
                  </label>
                </div>
                {queryAccessMetadataLoading ? <p className="mt-3 text-[11px] text-muted">Loading metadata options...</p> : null}
              </form>

              <DataTableSurface>
                <DataTableScroll>
                  <DataTable>
                    <DataTableHead>
                      <tr>
                        <DataTableHeaderCell>Subject</DataTableHeaderCell>
                        <DataTableHeaderCell>Subject Type</DataTableHeaderCell>
                        <DataTableHeaderCell>Effect</DataTableHeaderCell>
                        <DataTableHeaderCell>DB Scope</DataTableHeaderCell>
                        <DataTableHeaderCell>Source</DataTableHeaderCell>
                        <DataTableHeaderCell>Expires</DataTableHeaderCell>
                        <DataTableHeaderCell>Status</DataTableHeaderCell>
                        <DataTableHeaderCell>Action</DataTableHeaderCell>
                      </tr>
                    </DataTableHead>
                    <DataTableBody>
                      {pagedQueryAccessRules.map((rule) => {
                        const active = !rule.revoked_at && (!rule.expires_at || new Date(rule.expires_at).getTime() > Date.now())
                        return (
                          <DataTableRow key={rule.id}>
                            <DataTableCell>
                              {formatQueryAccessSubject(rule, users, authGroups)}
                            </DataTableCell>
                            <DataTableCell>
                              {formatQueryAccessSubjectType(rule.subject_type)}
                            </DataTableCell>
                            <DataTableCell>
                              <Tag label={rule.effect} tone={rule.effect === 'deny' ? 'danger' : 'success'} />
                            </DataTableCell>
                            <DataTableCell>
                              {getConnectionLabel(rule.connection_id, connections)} / {rule.database_pattern === '*' ? 'All databases' : rule.database_pattern} / {rule.table_pattern === '*' ? 'All tables' : rule.table_pattern}
                            </DataTableCell>
                            <DataTableCell>{formatQueryAccessSource(rule)}</DataTableCell>
                            <DataTableCell>{rule.expires_at ? formatDateTime(rule.expires_at) : 'Never'}</DataTableCell>
                            <DataTableCell>
                              <Tag label={active ? 'active' : rule.revoked_at ? 'revoked' : 'expired'} tone={active ? 'success' : 'default'} />
                            </DataTableCell>
                            <DataTableCell>
                              <div className="flex flex-wrap items-center gap-2">
                                <button
                                  type="button"
                                  onClick={() => handleEditQueryAccessRule(rule)}
                                  disabled={!active || savingQueryAccessRule}
                                  className="inline-flex h-8 items-center justify-center whitespace-nowrap rounded-md border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-panel-soft disabled:cursor-not-allowed disabled:opacity-50"
                                >
                                  Edit
                                </button>
                              <button
                                type="button"
                                onClick={() => void handleRevokeQueryAccessRule(rule.id)}
                                disabled={!active || savingQueryAccessRule}
                                className="inline-flex h-8 items-center justify-center whitespace-nowrap rounded-md border border-border bg-panel-soft px-3 text-[12px] font-semibold text-ink transition hover:bg-page disabled:cursor-not-allowed disabled:opacity-50"
                              >
                                Revoke
                              </button>
                              </div>
                            </DataTableCell>
                          </DataTableRow>
                        )
                      })}
                    </DataTableBody>
                  </DataTable>
                </DataTableScroll>
              </DataTableSurface>
            </section>
          )}

      <Pagination
        offset={currentOffset}
        pageSize={PAGE_SIZE}
        count={currentPageCount}
        total={currentTotal}
        onChange={currentOffsetSetter}
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
              ) : drawerState.mode === 'create-user' || drawerState.mode === 'edit-user' ? (
                <form className="grid gap-4" onSubmit={handleSaveUser}>
                  <CardSection title="User Profile" icon={<UsersIcon className="h-4 w-4 text-accent" />}>
                    <div className="grid gap-3">
                      <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                        Username
                        <input
                          aria-label="Username"
                          value={userDraft.username}
                          onChange={(event) => setUserDraft((current) => ({ ...current, username: event.target.value }))}
                          disabled={saving || selectedUserSecurityLocked}
                          className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20 disabled:opacity-60"
                        />
                      </label>
                      <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                        Email
                        <input
                          aria-label="Email"
                          value={userDraft.email}
                          onChange={(event) => setUserDraft((current) => ({ ...current, email: event.target.value }))}
                          disabled={saving || selectedUserSecurityLocked}
                          className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20 disabled:opacity-60"
                        />
                      </label>
                      <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                        <span className="flex items-center gap-1.5">
                          <span>Lark Open ID</span>
                          <span className="group relative inline-flex">
                            <span
                              className="inline-flex h-4 w-4 items-center justify-center rounded-full text-faint transition hover:text-muted"
                              aria-label="Lark Open ID help"
                              tabIndex={0}
                            >
                              <Info className="h-3.5 w-3.5" />
                            </span>
                            <span className="pointer-events-none absolute left-0 top-[calc(100%+8px)] z-20 hidden w-64 rounded-md border border-border bg-white px-3 py-2 text-[11px] font-medium normal-case tracking-normal text-muted shadow-soft group-hover:block group-focus-within:block">
                              After a new user is activated, an admin must manually bind a deliverable Lark Open ID or ticket notifications will not be delivered.
                            </span>
                          </span>
                        </span>
                        <input
                          aria-label="Lark Open ID"
                          value={userDraft.larkRecipient}
                          onChange={(event) => setUserDraft((current) => ({ ...current, larkRecipient: event.target.value }))}
                          disabled={saving || selectedUserSecurityLocked}
                          placeholder="Enter an open_id, for example ou_xxxxxxxxxxxxx"
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
                          disabled={saving || selectedUserSecurityLocked}
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
                          {currentUserCanManageProtectedAccess
                            ? 'The initial admin is protected. Only all-permissions admins can change high-risk fields.'
                            : 'The initial admin is protected. Your account cannot change password, MFA, status, groups, permissions, DB scope, or delete this user.'}
                        </div>
                      ) : null}

                    </div>
                  </CardSection>

                  <CardSection title="Auth Groups" icon={<Shield className="h-4 w-4 text-accent" />}>
                    <div className="grid gap-3">
                      <div className="flex flex-wrap gap-2">
                        {userDraft.authGroups.length > 0
                          ? userDraft.authGroups.map((group) => (
                              <ActionTag
                                key={group}
                                label={authGroupLabelMap.get(group) ?? group}
                                disabled={saving || selectedUserSecurityLocked || (!currentUserCanManageProtectedAccess && protectedAuthGroupNames.has(group))}
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
                          disabled={saving || selectedUserSecurityLocked}
                          options={[
                            { value: '', label: 'Select auth group' },
                            ...authGroupOptions
                              .filter((group) => !userDraft.authGroups.includes(group))
                              .filter((group) => currentUserCanManageProtectedAccess || !protectedAuthGroupNames.has(group))
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
                          disabled={saving || selectedUserSecurityLocked || !pendingUserAuthGroup}
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
                        disabled={saving || selectedUserSecurityLocked}
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
                        disabled={saving || selectedUserSecurityLocked}
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
                        disabled={saving || selectedUserSecurityLocked}
                        onRemove={(connectionId) => setUserDraft((current) => ({
                          ...current,
                          directDBConnectionIDs: current.directDBConnectionIDs.filter((item) => item !== connectionId),
                          pendingDelete: false,
                        }))}
                      />
                      <DBScopePanel
                        title="Add Direct DB Scope"
                        description="Search assignable connections by name, type, or database."
                        search={userDBScopeSearch}
                        onSearchChange={setUserDBScopeSearch}
                        connections={availableUserConnections}
                        emptyMessage="No matches found, or all direct DB scope entries are already assigned."
                        disabled={saving || selectedUserSecurityLocked}
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

                  {drawerState.mode === 'edit-user' && selectedUser ? (
                    <CardSection title="Sessions" icon={<Shield className="h-4 w-4 text-accent" />}>
                      <div className="flex flex-wrap items-center justify-between gap-3">
                        <div>
                          <p className="text-[12px] font-semibold text-ink">Refresh Sessions</p>
                          <p className="mt-1 text-[11px] text-muted">Review and revoke browser sessions for this user.</p>
                        </div>
                        <div className="flex items-center gap-2">
                          <button
                            type="button"
                            onClick={() => void refreshSelectedUserSessions(drawerState.userId)}
                            disabled={sessionsLoading || sessionsActing !== null}
                            className="inline-flex h-9 items-center gap-2 rounded-md border border-border bg-white px-3 text-[12px] font-semibold text-ink transition hover:bg-panel-soft disabled:opacity-50"
                          >
                            <RefreshCw className={`h-3.5 w-3.5 ${sessionsLoading ? 'animate-spin' : ''}`} />
                            Refresh
                          </button>
                          <button
                            type="button"
                            onClick={() => void handleRevokeUserSessions()}
                            disabled={sessionsLoading || sessionsActing !== null || selectedUserSessions.every((session) => session.revoked_at != null)}
                            className="inline-flex h-9 items-center gap-2 rounded-md border border-danger/20 bg-red-50 px-3 text-[12px] font-semibold text-danger transition hover:bg-red-100 disabled:opacity-50"
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                            Revoke All
                          </button>
                        </div>
                      </div>

                      {sessionsLoading ? (
                        <LoadingBlock message="Loading sessions..." className="min-h-[120px] rounded-lg border-border bg-panel-soft" />
                      ) : selectedUserSessions.length === 0 ? (
                        <div className="rounded-lg border border-border bg-panel-soft px-3 py-3 text-[12px] text-muted">No sessions found.</div>
                      ) : (
                        <div className="grid gap-3">
                          <div className="overflow-x-auto rounded-lg border border-border">
                            <DataTable>
                              <DataTableHead>
                                <tr>
                                  <DataTableHeaderCell>Session</DataTableHeaderCell>
                                  <DataTableHeaderCell>IP</DataTableHeaderCell>
                                  <DataTableHeaderCell>Created</DataTableHeaderCell>
                                  <DataTableHeaderCell>Expires</DataTableHeaderCell>
                                  <DataTableHeaderCell>Status</DataTableHeaderCell>
                                  <DataTableHeaderCell className="text-right">Actions</DataTableHeaderCell>
                                </tr>
                              </DataTableHead>
                              <DataTableBody>
                                {pagedSelectedUserSessions.map((session) => {
                                  const revoked = session.revoked_at != null
                                  return (
                                    <DataTableRow key={session.id} className="align-top">
                                      <DataTableCell className="max-w-[260px]">
                                        <p>Session #{session.id}</p>
                                        <p className="mt-1 truncate text-[11px] text-muted" title={session.user_agent ?? ''}>
                                          {session.user_agent || 'Unknown user agent'}
                                        </p>
                                      </DataTableCell>
                                      <DataTableCell className="whitespace-nowrap text-muted">{session.ip_address || '-'}</DataTableCell>
                                      <DataTableCell className="whitespace-nowrap text-muted">{formatDateTime(session.created_at)}</DataTableCell>
                                      <DataTableCell className="whitespace-nowrap text-muted">{formatDateTime(session.expires_at)}</DataTableCell>
                                      <DataTableCell className="whitespace-nowrap">
                                        <Tag label={revoked ? 'Revoked' : 'Active'} tone={revoked ? 'default' : 'success'} />
                                      </DataTableCell>
                                      <DataTableCell className="whitespace-nowrap text-right">
                                        <button
                                          type="button"
                                          onClick={() => void handleRevokeUserSession(session.id)}
                                          disabled={sessionsActing !== null || revoked}
                                          className="inline-flex h-8 items-center gap-2 rounded-md border border-border bg-white px-2.5 text-[12px] font-semibold text-ink transition hover:bg-panel-soft disabled:opacity-50"
                                        >
                                          <Trash2 className="h-3.5 w-3.5" />
                                          Revoke
                                        </button>
                                      </DataTableCell>
                                    </DataTableRow>
                                  )
                                })}
                              </DataTableBody>
                            </DataTable>
                          </div>
                          <Pagination
                            total={selectedUserSessions.length}
                            pageSize={SESSION_PAGE_SIZE}
                            offset={selectedUserSessionsOffset}
                            count={pagedSelectedUserSessions.length}
                            onChange={setSelectedUserSessionsOffset}
                          />
                        </div>
                      )}
                    </CardSection>
                  ) : null}

                  {drawerState.mode === 'edit-user' && !selectedUserSecurityLocked ? (
                    <CardSection title="Account Controls" icon={<Shield className="h-4 w-4 text-accent" />}>
                      <div className="grid gap-3">
                        <div className="flex items-center justify-between gap-3 rounded-lg border border-border bg-panel-soft px-3 py-3">
                          <div>
                            <p className="text-[12px] font-semibold text-ink">Reset MFA</p>
                            <p className="mt-1 text-[11px] text-muted">Clears this user's authenticator setup and revokes all sessions.</p>
                          </div>
                          <button
                            type="button"
                            onClick={() => void handleResetUserMFA()}
                            disabled={saving || mfaResetting}
                            className="inline-flex h-10 items-center justify-center rounded-lg border border-border bg-white px-4 text-[13px] font-semibold text-ink transition hover:bg-page disabled:opacity-50"
                          >
                            {mfaResetting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                            Reset MFA
                          </button>
                        </div>
                        <div className="flex items-center justify-between gap-3 rounded-lg border border-amber-200 bg-amber-50 px-3 py-3">
                          <div>
                            <p className="text-[12px] font-semibold text-amber-800">Sign-in Status</p>
                            <p className="mt-1 text-[11px] text-amber-700/90">Disabling only updates the draft. The change is applied after you save at the bottom.</p>
                          </div>
                          <button
                            type="button"
                            onClick={() => setUserDraft((current) => ({ ...current, isActive: !current.isActive, pendingDelete: false }))}
                            disabled={saving}
                            className={`inline-flex h-10 items-center justify-center rounded-lg px-4 text-[13px] font-semibold transition disabled:opacity-50 ${
                              userDraft.isActive
                                ? 'border border-amber-300 bg-amber-100 text-amber-800 hover:bg-amber-200'
                                : 'border border-amber-200 bg-white text-amber-800 hover:bg-amber-50'
                            }`}
                          >
                            {userDraft.isActive ? 'Mark Disabled' : 'Mark Enabled'}
                          </button>
                        </div>
                        <div className="flex items-center justify-between gap-3 rounded-lg border border-danger/20 bg-red-50 px-3 py-3">
                          <div>
                            <p className="text-[12px] font-semibold text-danger">Delete This User</p>
                            <p className="mt-1 text-[11px] text-danger/80">This only marks the user for deletion. The deletion runs after you confirm Save Changes at the bottom.</p>
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
                      </div>
                    </CardSection>
                  ) : null}

                  <div className="flex justify-end">
                    <button
                      type="submit"
                      disabled={
                        saving ||
                        selectedUserSecurityLocked ||
                        !userDraft.username.trim() ||
                        !userDraft.email.trim() ||
                        (drawerState.mode === 'create-user' && !userDraft.password.trim())
                      }
                      className="inline-flex h-10 min-w-[180px] items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition hover:bg-slate-800 disabled:opacity-50"
                    >
                      {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
                      {drawerState.mode === 'create-user' ? 'Confirm Create' : userDraft.pendingDelete ? 'Confirm Delete' : 'Save Changes'}
                    </button>
                  </div>

                  {drawerError ? <InlineAlert>{drawerError}</InlineAlert> : null}
                </form>
              ) : drawerState.mode === 'create-auth-group' || drawerState.mode === 'edit-auth-group' ? (
                <form className="grid gap-4" onSubmit={handleSaveAuthGroup}>
                  <CardSection title="Auth Group Profile" icon={<Shield className="h-4 w-4 text-accent" />}>
                    <div className="grid gap-3">
                      <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                        Name
                        <input
                          aria-label="Name"
                          value={authGroupDraft.name}
                          onChange={(event) => setAuthGroupDraft((current) => ({ ...current, name: event.target.value }))}
                          disabled={saving || selectedAuthGroupSecurityLocked}
                          className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20 disabled:opacity-60"
                        />
                      </label>
                      <label className="grid gap-1.5 text-[12px] font-medium text-muted">
                        Description
                        <input
                          aria-label="Description"
                          value={authGroupDraft.description}
                          onChange={(event) => setAuthGroupDraft((current) => ({ ...current, description: event.target.value }))}
                          disabled={saving || selectedAuthGroupSecurityLocked}
                          className="h-10 rounded-lg border border-border bg-panel-soft px-3 text-[13px] text-ink outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20 disabled:opacity-60"
                        />
                      </label>
                      {selectedAuthGroup ? (
                        <div className="grid gap-3 sm:grid-cols-2">
                          <InfoBox label="Created" value={formatDateTime(selectedAuthGroup.created_at ?? '')} />
                          <InfoBox label="Updated" value={formatDateTime(selectedAuthGroup.updated_at ?? '')} />
                        </div>
                      ) : null}
                    </div>
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
                                disabled={saving || (user?.protected === true && !currentUserCanManageProtectedAccess) || selectedAuthGroupSecurityLocked}
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
                        disabled={saving || selectedAuthGroupSecurityLocked}
                        options={[
                          { value: '', label: 'Select user' },
                          ...users
                            .filter((user) => !authGroupDraft.userIDs.includes(user.id))
                            .filter((user) => currentUserCanManageProtectedAccess || user.protected !== true)
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
                        disabled={saving || selectedAuthGroupSecurityLocked || !pendingAuthGroupUserID}
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
                      disabled={saving || selectedAuthGroupSecurityLocked}
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
                      disabled={saving || selectedAuthGroupSecurityLocked}
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
                      disabled={saving || selectedAuthGroupSecurityLocked}
                      onRemove={(connectionId) => setAuthGroupDraft((current) => ({
                        ...current,
                        dbConnectionIDs: current.dbConnectionIDs.filter((item) => item !== connectionId),
                        pendingDelete: false,
                      }))}
                    />
                    <DBScopePanel
                      title="Add DB Scope"
                      description="Search assignable database assets by name, type, or database."
                      search={authGroupDBScopeSearch}
                      onSearchChange={setAuthGroupDBScopeSearch}
                      connections={availableAuthGroupConnections}
                      emptyMessage="No matches found, or all DB scope entries are already assigned."
                      disabled={saving || selectedAuthGroupSecurityLocked}
                      onAdd={(connectionId) => setAuthGroupDraft((current) => ({
                        ...current,
                        dbConnectionIDs: [...current.dbConnectionIDs, connectionId],
                        pendingDelete: false,
                      }))}
                    />
                  </CardSection>

                  {drawerState.mode === 'edit-auth-group' && !selectedAuthGroupIsProtected ? (
                    <CardSection title="Account Controls" icon={<Shield className="h-4 w-4 text-accent" />}>
                      <div className="grid gap-3">
                        <div className="flex items-center justify-between gap-3 rounded-lg border border-danger/20 bg-red-50 px-3 py-3">
                          <div>
                            <p className="text-[12px] font-semibold text-danger">Delete This Auth Group</p>
                            <p className="mt-1 text-[11px] text-danger/80">This only marks the auth group for deletion. The deletion runs after you confirm Save Changes at the bottom.</p>
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
                      </div>
                    </CardSection>
                  ) : null}

                  <div className="flex justify-end">
                    <button
                      type="submit"
                      disabled={saving || selectedAuthGroupSecurityLocked || !authGroupDraft.name.trim()}
                      className="inline-flex h-10 min-w-[180px] items-center justify-center gap-2 rounded-lg bg-brand px-4 text-[13px] font-bold text-white shadow-soft transition hover:bg-slate-800 disabled:opacity-50"
                    >
                      {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
                      {drawerState.mode === 'create-auth-group' ? 'Create Auth Group' : authGroupDraft.pendingDelete ? 'Confirm Delete' : 'Save Auth Group'}
                    </button>
                  </div>

                  {drawerError ? <InlineAlert>{drawerError}</InlineAlert> : null}
                </form>
              ) : null}
            </div>
          </div>
        </div>
      ) : null}
      </div>
      <ConfirmDialog
        open={confirmState !== null}
        title={confirmState?.title ?? 'Confirm Changes'}
        description={confirmState ? <ConfirmSummary lines={confirmState.lines} /> : null}
        confirmLabel={confirmState?.confirmLabel ?? 'Confirm'}
        cancelLabel="Cancel"
        tone={confirmState?.tone ?? 'default'}
        loading={saving}
        panelClassName="max-w-2xl"
        onCancel={() => {
          if (!saving) {
            setConfirmState(null)
          }
        }}
        onConfirm={() => {
          void handleConfirmSubmit()
        }}
      />
    </>
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
  if (draft.larkRecipient !== original.lark_recipient) {
    lines.push(`Lark Open ID: ${original.lark_recipient || 'Not set'} -> ${draft.larkRecipient || 'Not set'}`)
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
    lines.push(`Direct Permissions: ${formatPermissionList(draft.directPermissions)}`)
  }
  if (!equalNumberArrays(originalDirectConnectionIDs, draft.directDBConnectionIDs)) {
    lines.push(`Direct DB Scope: ${formatConnectionIDs(draft.directDBConnectionIDs, connections)}`)
  }

  return lines
}

function canManageProtectedAccess(user: CurrentUser | null) {
  if (!user) {
    return false
  }
  if (user.protected || user.authGroupDetails.some((group) => group.is_protected)) {
    return true
  }
  const permissions = new Set(user.permissions)
  return PERMISSION_METADATA.every((permission) => permissions.has(permission.key))
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
    lines.push(`Permissions: ${formatPermissionList(draft.permissions)}`)
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

function formatPermissionList(permissionKeys: string[]) {
  return permissionKeys.length > 0 ? permissionKeys.map((permissionKey) => getPermissionMeta(permissionKey).label).join(', ') : 'None'
}

function getConnectionLabel(connectionId: number, connections: DBConnection[]) {
  const connection = connections.find((item) => item.id === connectionId)
  return connection ? connection.name : `Connection #${connectionId}`
}

function formatDBType(dbType: string) {
  switch (dbType) {
    case 'postgres':
      return 'PostgreSQL'
    case 'redis':
      return 'Redis'
    default:
      return 'MySQL'
  }
}

function formatQueryAccessSubject(rule: QueryAccessRule, users: UserSummary[], authGroups: AuthGroupDetail[]) {
  if (rule.subject_type === 'user') {
    return users.find((user) => user.id === rule.subject_id)?.username ?? `User #${rule.subject_id}`
  }
  const group = authGroups.find((item) => item.id === rule.subject_id)
  return group?.label ?? group?.name ?? `Auth Group #${rule.subject_id}`
}

function formatQueryAccessSubjectType(subjectType: QueryAccessRule['subject_type']) {
  return subjectType === 'auth_group' ? 'Auth Group' : 'User'
}

function formatQueryAccessSource(rule: QueryAccessRule) {
  if (rule.granted_via === 'ticket') {
    if (rule.source_ticket_no) {
      return (
        <Link
          to={`/tickets/${rule.source_ticket_no}`}
          className="font-mono text-[12px] text-accent underline-offset-2 transition hover:underline"
          title={rule.source_ticket_no}
        >
          {rule.source_ticket_no}
        </Link>
      )
    }
    return rule.source_ticket_id ? `Ticket grant #${rule.source_ticket_id}` : 'Ticket grant'
  }
  if (rule.granted_via === 'manual') {
    return 'Manual rule'
  }
  return rule.granted_via
}

function hasAllPermissionsUser(user: UserSummary, authGroups: AuthGroupDetail[]) {
  return user.protected || user.auth_groups.some((groupName) => authGroups.some((group) => group.name === groupName && group.all_permissions))
}

function BindingTags({ items, emptyLabel, maxVisible = 2 }: { items: string[]; emptyLabel: string; maxVisible?: number }) {
  const [expanded, setExpanded] = useState(false)

  if (items.length === 0) {
    return <span className="text-[12px] text-muted">{emptyLabel}</span>
  }

  const visibleItems = expanded ? items : items.slice(0, maxVisible)
  const hiddenCount = Math.max(0, items.length - visibleItems.length)

  return (
    <div className="flex flex-wrap gap-1.5">
      {visibleItems.map((item) => (
        <Tag key={item} label={item} title={item} />
      ))}
      {hiddenCount > 0 ? (
        <button
          type="button"
          onClick={() => setExpanded(true)}
          className="inline-flex items-center rounded-full border border-border bg-white px-2 py-0.5 text-[10px] font-semibold text-muted transition hover:bg-panel-soft hover:text-ink"
          title={items.slice(maxVisible).join(', ')}
        >
          +{hiddenCount}
        </button>
      ) : expanded && items.length > maxVisible ? (
        <button
          type="button"
          onClick={() => setExpanded(false)}
          className="inline-flex items-center rounded-full border border-border bg-white px-2 py-0.5 text-[10px] font-semibold text-muted transition hover:bg-panel-soft hover:text-ink"
        >
          Less
        </button>
      ) : null}
    </div>
  )
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

function Tag({ label, tone = 'default', title }: { label: string; tone?: 'default' | 'danger' | 'success'; title?: string }) {
  return (
    <span
      title={title}
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
      <SearchInput
        value={search}
        onChange={(event) => onSearchChange(event.target.value)}
        placeholder="Search module, action, or permission key"
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
          const meta = connection?.db_type
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
      <SearchInput
        value={search}
        onChange={(event) => onSearchChange(event.target.value)}
        placeholder="Search connection name, type, database"
        disabled={disabled}
      />
      <div className="grid gap-2">
        {connections.length > 0 ? connections.map((connection) => (
          <div key={connection.id} className="flex items-start justify-between gap-3 rounded-lg border border-border bg-white px-3 py-2.5">
            <div>
              <p className="text-[12px] font-semibold text-ink">{connection.name}</p>
              <p className="mt-1 text-[11px] text-muted">{connection.db_type}</p>
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

function ConfirmSummary({ lines }: { lines: string[] }) {
  return (
    <div className="space-y-3">
      <div className="rounded-xl border border-border bg-panel-soft/80 p-4">
        <p className="text-[11px] font-semibold uppercase tracking-[0.12em] text-faint">Change Summary</p>
        <div className="mt-3 grid gap-2">
          {lines.length > 0 ? (
            lines.map((line) => (
              <div key={line} className="rounded-lg border border-border/80 bg-white px-3 py-2 text-[13px] text-ink">
                {line}
              </div>
            ))
          ) : (
            <div className="rounded-lg border border-border/80 bg-white px-3 py-2 text-[13px] text-muted">No field changes.</div>
          )}
        </div>
      </div>
      <p className="text-[12px] text-muted">Please confirm these changes before continuing.</p>
    </div>
  )
}
