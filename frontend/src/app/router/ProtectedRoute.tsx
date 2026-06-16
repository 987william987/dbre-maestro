import { Navigate, Outlet, useLocation } from 'react-router-dom'
import { useAuth } from '@/shared/auth/AuthContext'

export function ProtectedRoute() {
  const { status, isAuthenticated } = useAuth()
  const location = useLocation()

  if (status === 'loading') {
    return (
      <div className="min-h-screen bg-page flex items-center justify-center px-6">
        <div className="rounded-card border border-border bg-panel px-6 py-5 shadow-soft">
          <p className="text-sm font-semibold text-ink">Verifying your session…</p>
          <p className="mt-1 text-xs text-muted">Please wait while we sync your account details.</p>
        </div>
      </div>
    )
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace state={{ from: location }} />
  }

  return <Outlet />
}
