import { Navigate, Outlet } from 'react-router-dom'
import { useAuth } from '@/shared/auth/AuthContext'

export function RoleRoute({ allowedPermissions }: { allowedPermissions: string[] }) {
  const { user } = useAuth()

  if (!user) {
    return <Navigate to="/login" replace />
  }

  const allowed = user.permissions.some((permission) => allowedPermissions.includes(permission))
  if (!allowed) {
    return <Navigate to="/tickets" replace />
  }

  return <Outlet />
}
