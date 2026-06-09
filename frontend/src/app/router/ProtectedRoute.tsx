import { Navigate, Outlet, useLocation } from 'react-router-dom'
import { useAuth } from '@/shared/auth/AuthContext'

export function ProtectedRoute() {
  const { status, isAuthenticated } = useAuth()
  const location = useLocation()

  if (status === 'loading') {
    return (
      <div className="min-h-screen bg-page flex items-center justify-center px-6">
        <div className="rounded-card border border-border bg-panel px-6 py-5 shadow-soft">
          <p className="text-sm font-semibold text-ink">正在驗證登入狀態…</p>
          <p className="mt-1 text-xs text-muted">請稍候，系統正在同步目前使用者資訊。</p>
        </div>
      </div>
    )
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace state={{ from: location }} />
  }

  return <Outlet />
}
