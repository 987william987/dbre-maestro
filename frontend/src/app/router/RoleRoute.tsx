import { Navigate, Outlet } from 'react-router-dom'
import { useAuth } from '@/shared/auth/AuthContext'
import { defaultRouteForPermissions } from '@/shared/auth/permissions'

export function RoleRoute({ allowedPermissions }: { allowedPermissions: string[] }) {
  const { user } = useAuth()

  if (!user) {
    return <Navigate to="/login" replace />
  }

  const allowed = user.permissions.some((permission) => allowedPermissions.includes(permission))
  if (!allowed) {
    return <Navigate to={defaultRouteForPermissions(user.permissions)} replace />
  }

  return <Outlet />
}
