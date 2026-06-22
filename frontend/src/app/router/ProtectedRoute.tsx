import { Navigate, Outlet, useLocation } from 'react-router-dom'
import { useAuth } from '@/shared/auth/AuthContext'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'

export function ProtectedRoute() {
  const { status, isAuthenticated } = useAuth()
  const location = useLocation()

  if (status === 'loading') {
    return (
      <div className="flex min-h-screen items-center justify-center bg-page px-6">
        <LoadingBlock message="Loading..." className="min-h-[160px] w-full max-w-sm rounded-card border-border bg-panel" />
      </div>
    )
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace state={{ from: location }} />
  }

  return <Outlet />
}
