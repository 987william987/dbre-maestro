export type AuthGroup = string

export type CurrentAuthGroup = {
  id: number
  group_key: string
  name: string
  is_system: boolean
  is_protected: boolean
}

export type CurrentUser = {
  id: number
  username: string
  authGroups: AuthGroup[]
  authGroupDetails: CurrentAuthGroup[]
  permissions: string[]
  dbConnectionIds: number[]
  protected: boolean
  isActive: boolean
  authMethod?: string
  authProvider?: string
}

export type AuthStatus = 'loading' | 'authenticated' | 'anonymous'
