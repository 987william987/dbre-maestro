export type AuthGroup = 'developer' | 'reviewer' | 'dba' | 'admin'

export type CurrentUser = {
  id: number
  username: string
  authGroups: AuthGroup[]
}

export type AuthStatus = 'loading' | 'authenticated' | 'anonymous'
