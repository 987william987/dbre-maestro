import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { UsersPage } from '@/modules/users/pages/UsersPage'
import { ToastProvider } from '@/shared/ui/ToastContext'

vi.mock('@/modules/users/api', () => ({
  listUsers: vi.fn(),
  getUser: vi.fn(),
  createUser: vi.fn(),
  patchUser: vi.fn(),
  deleteUser: vi.fn(),
  listUserDBConnections: vi.fn(),
}))

vi.mock('@/modules/auth-groups/api', () => ({
  listAuthGroups: vi.fn(),
  getAuthGroup: vi.fn(),
  createAuthGroup: vi.fn(),
  patchAuthGroup: vi.fn(),
  deleteAuthGroup: vi.fn(),
}))

import { createAuthGroup, getAuthGroup, listAuthGroups, patchAuthGroup } from '@/modules/auth-groups/api'
import { createUser, deleteUser, getUser, listUserDBConnections, listUsers, patchUser } from '@/modules/users/api'

const mockedListUsers = vi.mocked(listUsers)
const mockedGetUser = vi.mocked(getUser)
const mockedCreateUser = vi.mocked(createUser)
const mockedPatchUser = vi.mocked(patchUser)
const mockedDeleteUser = vi.mocked(deleteUser)
const mockedListUserDBConnections = vi.mocked(listUserDBConnections)
const mockedListAuthGroups = vi.mocked(listAuthGroups)
const mockedGetAuthGroup = vi.mocked(getAuthGroup)
const mockedCreateAuthGroup = vi.mocked(createAuthGroup)
const mockedPatchAuthGroup = vi.mocked(patchAuthGroup)

function selectOption(label: string, option: string) {
  fireEvent.click(screen.getByRole('button', { name: label }))
  fireEvent.click(screen.getByRole('option', { name: option }))
}

function renderPage() {
  return render(
    <MemoryRouter>
      <ToastProvider>
        <UsersPage />
      </ToastProvider>
    </MemoryRouter>,
  )
}

function seedAuthGroups() {
  mockedListAuthGroups.mockResolvedValue({
    auth_groups: [
      { name: 'developer', label: 'Developer', description: '', system_defined: true, user_count: 0, permission_count: 0, db_connection_count: 0 },
      { name: 'reviewer', label: 'Reviewer', description: '', system_defined: true, user_count: 0, permission_count: 0, db_connection_count: 0 },
      { name: 'dba', label: 'DBA', description: '', system_defined: true, user_count: 0, permission_count: 0, db_connection_count: 0 },
      { name: 'admin', label: 'Admin', description: '', system_defined: true, user_count: 0, permission_count: 0, db_connection_count: 0 },
    ],
  })
  mockedGetAuthGroup.mockImplementation(async (group) => ({
    id: 1,
    name: String(group),
    label: String(group).charAt(0).toUpperCase() + String(group).slice(1),
    description: '',
    system_defined: group !== 'ops',
    protected: group === 'admin',
    users: [],
    permissions: [],
    db_connection_ids: [],
    created_at: '2026-06-10T00:00:00Z',
    updated_at: '2026-06-10T00:00:00Z',
  }))
}

describe('UsersPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    mockedListUsers.mockResolvedValue({ users: [] })
    mockedListUserDBConnections.mockResolvedValue({ connections: [] })
    seedAuthGroups()
  })

  it('列表會顯示 auth group label', async () => {
    mockedListUsers.mockResolvedValue({
      users: [
        {
          id: 2,
          username: 'alice',
          email: 'alice@example.com',
          lark_recipient: '',
          auth_groups: ['developer', 'dba'],
          db_connection_ids: [],
          protected: false,
          is_active: true,
          created_at: '2026-06-10T00:00:00Z',
          updated_at: '2026-06-10T00:00:00Z',
        },
      ],
    })

    renderPage()

    const email = await screen.findByText('alice@example.com')
    const row = email.closest('tr')

    expect(row).not.toBeNull()
    expect(within(row as HTMLTableRowElement).getByText('Developer')).toBeInTheDocument()
    expect(within(row as HTMLTableRowElement).getByText('Dba')).toBeInTheDocument()
  })

  it('建立使用者時可一次提交初始 auth groups', async () => {
    mockedCreateUser.mockResolvedValue({
      id: 8,
      username: 'alice',
      email: 'alice@example.com',
      lark_recipient: '',
    })
    mockedGetUser.mockResolvedValue({
      id: 8,
      username: 'alice',
      email: 'alice@example.com',
      lark_recipient: '',
      protected: false,
      is_active: true,
      created_at: '2026-06-10T00:00:00Z',
      updated_at: '2026-06-10T00:00:00Z',
      memberships: [{ id: 1, user_id: 8, auth_group: 'developer', granted_by: 1, expires_at: null, created_at: '2026-06-10T00:00:00Z' }],
      permissions: [],
      db_connection_ids: [],
      direct_permissions: [],
      direct_db_connection_ids: [],
    })

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: 'Create User' }))
    fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'alice' } })
    fireEvent.change(screen.getByLabelText('Email'), { target: { value: 'alice@example.com' } })
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'Secret123!' } })
    fireEvent.change(screen.getByLabelText('User auth group membership selection'), { target: { value: 'developer' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add to Group' }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm Create' }))

    await waitFor(() => {
      expect(mockedCreateUser).toHaveBeenCalledWith({
        username: 'alice',
        email: 'alice@example.com',
        lark_recipient: '',
        password: 'Secret123!',
      })
    })
    await waitFor(() => {
      expect(mockedPatchUser).toHaveBeenCalledWith(8, {
        auth_groups: ['developer'],
        direct_permissions: [],
        direct_db_connection_ids: [],
      })
    })
  })

  it('使用者編輯只在儲存時才送出 patch', async () => {
    mockedPatchUser.mockClear()
    mockedListUsers.mockResolvedValue({
      users: [
        {
          id: 3,
          username: 'bob',
          email: 'bob@example.com',
          lark_recipient: '',
          auth_groups: ['reviewer'],
          db_connection_ids: [],
          protected: false,
          is_active: true,
          created_at: '2026-06-10T00:00:00Z',
          updated_at: '2026-06-10T00:00:00Z',
        },
      ],
    })
    mockedGetUser.mockResolvedValue({
      id: 3,
      username: 'bob',
      email: 'bob@example.com',
      lark_recipient: '',
      protected: false,
      is_active: true,
      created_at: '2026-06-10T00:00:00Z',
      updated_at: '2026-06-10T00:00:00Z',
      memberships: [{ id: 1, user_id: 3, auth_group: 'reviewer', granted_by: 1, expires_at: null, created_at: '2026-06-10T00:00:00Z' }],
      permissions: [],
      db_connection_ids: [],
      direct_permissions: [],
      direct_db_connection_ids: [],
    })

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: 'Manage' }))
    fireEvent.change(await screen.findByLabelText('Username'), { target: { value: 'bobby' } })
    fireEvent.change(screen.getByLabelText('Email'), { target: { value: 'bobby@example.com' } })

    expect(mockedPatchUser).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))

    await waitFor(() => {
      expect(mockedPatchUser).toHaveBeenCalledWith(3, {
        username: 'bobby',
        email: 'bobby@example.com',
        lark_recipient: '',
        is_active: true,
        auth_groups: ['reviewer'],
        direct_permissions: [],
        direct_db_connection_ids: [],
      })
    })
  })

  it('protected admin 只能修改密碼', async () => {
    mockedListUsers.mockResolvedValue({
      users: [
        {
          id: 1,
          username: 'admin',
          email: 'admin@example.com',
          lark_recipient: '',
          auth_groups: ['admin'],
          db_connection_ids: [],
          protected: true,
          is_active: true,
          created_at: '2026-06-10T00:00:00Z',
          updated_at: '2026-06-10T00:00:00Z',
        },
      ],
    })
    mockedGetUser.mockResolvedValue({
      id: 1,
      username: 'admin',
      email: 'admin@example.com',
      lark_recipient: '',
      protected: true,
      is_active: true,
      created_at: '2026-06-10T00:00:00Z',
      updated_at: '2026-06-10T00:00:00Z',
      memberships: [{ id: 1, user_id: 1, auth_group: 'admin', granted_by: 1, expires_at: null, created_at: '2026-06-10T00:00:00Z' }],
      permissions: ['users.write'],
      db_connection_ids: [],
      direct_permissions: [],
      direct_db_connection_ids: [],
    })

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: 'Manage' }))
    const username = await screen.findByLabelText('Username')
    const email = screen.getByLabelText('Email')
    const password = screen.getByLabelText('Password')

    expect(username).toBeDisabled()
    expect(email).toBeDisabled()
    expect(screen.queryByRole('button', { name: 'Mark Delete' })).not.toBeInTheDocument()

    fireEvent.change(password, { target: { value: 'NewSecret123!' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))

    await waitFor(() => {
      expect(mockedPatchUser).toHaveBeenCalledWith(1, { password: 'NewSecret123!' })
    })
  })

  it('建立 auth group 不需要 group key 且可帶 users / permissions / db scope', async () => {
    mockedListUserDBConnections.mockResolvedValue({
      connections: [
        {
          id: 11,
          name: 'analytics',
          db_type: 'mysql',
          host: 'db.local',
          port: 3306,
          username: 'reader',
          database_name: 'analytics',
          encryption_key_version: 1,
          ssl_mode: 'disable',
          created_by: 1,
          created_at: '',
          updated_at: '',
        },
      ],
    })
    mockedListUsers.mockResolvedValue({
      users: [
        {
          id: 7,
          username: 'amy',
          email: 'amy@example.com',
          lark_recipient: '',
          auth_groups: [],
          db_connection_ids: [],
          protected: false,
          is_active: true,
          created_at: '2026-06-10T00:00:00Z',
          updated_at: '2026-06-10T00:00:00Z',
        },
      ],
    })
    mockedCreateAuthGroup.mockResolvedValue({
      id: 9,
      name: 'ops',
      label: 'Ops',
      description: 'operations',
      system_defined: false,
      protected: false,
      users: [],
      permissions: [],
      db_connection_ids: [],
      created_at: '2026-06-10T00:00:00Z',
      updated_at: '2026-06-10T00:00:00Z',
    })

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: 'Create Auth Group' }))
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Ops' } })
    fireEvent.change(screen.getByLabelText('Description'), { target: { value: 'operations' } })
    selectOption('Auth Group user selection', 'amy')
    fireEvent.click(screen.getByRole('button', { name: 'Add User' }))
    fireEvent.click(screen.getAllByRole('button', { name: 'Add' })[0])
    fireEvent.change(screen.getByPlaceholderText('Search connection name, host, database'), { target: { value: 'analytics' } })
    const addButtons = screen.getAllByRole('button', { name: 'Add' })
    fireEvent.click(addButtons[addButtons.length - 1])
    fireEvent.click(screen.getAllByRole('button', { name: 'Create Auth Group' })[1])

    await waitFor(() => {
      expect(mockedCreateAuthGroup).toHaveBeenCalledWith({
        name: 'Ops',
        description: 'operations',
        user_ids: [7],
        permissions: ['users.read'],
        db_connection_ids: [11],
      })
    })
  })

  it('auth group 編輯顯示 created / updated 並用 patch 儲存', async () => {
    mockedGetAuthGroup.mockImplementation(async (group) => ({
      id: 4,
      name: String(group),
      label: 'DBA',
      description: 'db admins',
      system_defined: true,
      protected: false,
      users: [],
      permissions: ['tickets.execute'],
      db_connection_ids: [],
      created_at: '2026-06-10T00:00:00Z',
      updated_at: '2026-06-11T00:00:00Z',
    }))

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: 'Auth Groups' }))
    fireEvent.click((await screen.findAllByRole('button', { name: 'Manage' }))[0])
    expect((await screen.findAllByText('Created')).length).toBeGreaterThan(0)
    expect(screen.getAllByText('Updated').length).toBeGreaterThan(0)

    fireEvent.change(screen.getByLabelText('Description'), { target: { value: 'db admin team' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save Auth Group' }))

    await waitFor(() => {
      expect(mockedPatchAuthGroup).toHaveBeenCalledWith('developer', {
        name: 'DBA',
        description: 'db admin team',
        user_ids: [],
        permissions: ['tickets.execute'],
        db_connection_ids: [],
      })
    })
  })

  it('使用者刪除會先標記，最後儲存才真的送出', async () => {
    mockedListUsers.mockResolvedValue({
      users: [
        {
          id: 6,
          username: 'dave',
          email: 'dave@example.com',
          lark_recipient: '',
          auth_groups: ['developer'],
          db_connection_ids: [],
          protected: false,
          is_active: true,
          created_at: '2026-06-10T00:00:00Z',
          updated_at: '2026-06-10T00:00:00Z',
        },
      ],
    })
    mockedGetUser.mockResolvedValue({
      id: 6,
      username: 'dave',
      email: 'dave@example.com',
      lark_recipient: '',
      protected: false,
      is_active: true,
      created_at: '2026-06-10T00:00:00Z',
      updated_at: '2026-06-10T00:00:00Z',
      memberships: [{ id: 1, user_id: 6, auth_group: 'developer', granted_by: 1, expires_at: null, created_at: '2026-06-10T00:00:00Z' }],
      permissions: [],
      db_connection_ids: [],
      direct_permissions: [],
      direct_db_connection_ids: [],
    })

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: 'Manage' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Mark Delete' }))

    expect(mockedDeleteUser).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Confirm Delete' }))

    await waitFor(() => {
      expect(mockedDeleteUser).toHaveBeenCalledWith(6)
    })
  })

  it('paginates the users list', async () => {
    mockedListUsers.mockResolvedValue({
      users: Array.from({ length: 21 }, (_, index) => ({
        id: index + 1,
        username: `user-${index + 1}`,
        email: `user-${index + 1}@example.com`,
        lark_recipient: '',
        auth_groups: [],
        db_connection_ids: [],
        protected: false,
        is_active: true,
        created_at: '2026-06-10T00:00:00Z',
        updated_at: '2026-06-10T00:00:00Z',
      })),
    })

    renderPage()

    await waitFor(() => expect(screen.getByText('user-1@example.com')).toBeInTheDocument())
    expect(screen.queryByText('user-21@example.com')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Next' }))

    await waitFor(() => expect(screen.getByText('user-21@example.com')).toBeInTheDocument())
    expect(screen.queryByText('user-1@example.com')).not.toBeInTheDocument()
  })
})
