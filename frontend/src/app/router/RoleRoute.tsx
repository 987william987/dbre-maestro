import { Navigate, Outlet } from 'react-router-dom'
import { useAuth } from '@/shared/auth/AuthContext'
import type { AuthGroup } from '@/shared/types/auth'

export function RoleRoute({ allowedGroups }: { allowedGroups: AuthGroup[] }) {
  const { user } = useAuth()

  if (!user) {
    return <Navigate to="/login" replace />
  }

  const allowed = user.authGroups.some((group) => allowedGroups.includes(group))
  if (!allowed) {
    return <Navigate to="/tickets" replace />
  }

  return <Outlet />
}
