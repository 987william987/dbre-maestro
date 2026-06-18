import { Suspense, lazy } from 'react'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { AppShell } from '@/app/layout/AppShell'
import { ProtectedRoute } from '@/app/router/ProtectedRoute'
import { RoleRoute } from '@/app/router/RoleRoute'
import { LoginPage } from '@/modules/auth/pages/LoginPage'
import { SetupWizard } from '@/pages/setup/SetupWizard'
import { defaultRouteForPermissions, TICKET_WORKSPACE_PERMISSIONS } from '@/shared/auth/permissions'
import { useAuth } from '@/shared/auth/AuthContext'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'

const AuditLogsPage = lazy(() => import('@/modules/audit/pages/AuditLogsPage').then((module) => ({ default: module.AuditLogsPage })))
const DBConnectionsPage = lazy(() => import('@/modules/db-connections/pages/DBConnectionsPage').then((module) => ({ default: module.DBConnectionsPage })))
const DBMetadataInventoryPage = lazy(() => import('@/modules/db-metadata/pages/DBMetadataInventoryPage').then((module) => ({ default: module.DBMetadataInventoryPage })))
const DBMetadataObjectsPage = lazy(() => import('@/modules/db-metadata/pages/DBMetadataObjectsPage').then((module) => ({ default: module.DBMetadataObjectsPage })))
const MaskingDSLGuidePage = lazy(() => import('@/modules/masking-rules/pages/MaskingDSLGuidePage').then((module) => ({ default: module.MaskingDSLGuidePage })))
const MaskingRulesPage = lazy(() => import('@/modules/masking-rules/pages/MaskingRulesPage').then((module) => ({ default: module.MaskingRulesPage })))
const SettingsPage = lazy(() => import('@/modules/settings/pages/SettingsPage').then((module) => ({ default: module.SettingsPage })))
const SQLEditorPage = lazy(() => import('@/modules/sql-editor/pages/SQLEditorPage').then((module) => ({ default: module.SQLEditorPage })))
const SQLReviewRulesPage = lazy(() => import('@/modules/sql-review-rules/pages/SQLReviewRulesPage').then((module) => ({ default: module.SQLReviewRulesPage })))
const TicketDetailPage = lazy(() => import('@/modules/tickets/pages/TicketDetailPage').then((module) => ({ default: module.TicketDetailPage })))
const NewTicketPage = lazy(() => import('@/modules/tickets/pages/NewTicketPage').then((module) => ({ default: module.NewTicketPage })))
const TicketsPage = lazy(() => import('@/modules/tickets/pages/TicketsPage').then((module) => ({ default: module.TicketsPage })))
const UsersPage = lazy(() => import('@/modules/users/pages/UsersPage').then((module) => ({ default: module.UsersPage })))

function HomeRedirect() {
  const { user } = useAuth()
  return <Navigate to={defaultRouteForPermissions(user?.permissions ?? [])} replace />
}

function RouteLoadingFallback() {
  return (
    <div className="p-3 sm:p-4">
      <LoadingBlock message="Loading page..." className="min-h-[320px] rounded-xl border-border bg-panel" />
    </div>
  )
}

export default function App() {
  return (
    <BrowserRouter future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
      <Suspense fallback={<RouteLoadingFallback />}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/setup" element={<SetupWizard />} />
          <Route element={<ProtectedRoute />}>
            <Route element={<AppShell />}>
              <Route path="/" element={<HomeRedirect />} />
              <Route element={<RoleRoute allowedPermissions={[...TICKET_WORKSPACE_PERMISSIONS]} />}>
                <Route path="/tickets" element={<TicketsPage />} />
                <Route path="/tickets/:id" element={<TicketDetailPage />} />
              </Route>
              <Route element={<RoleRoute allowedPermissions={['tickets.apply']} />}>
                <Route path="/tickets/new" element={<NewTicketPage />} />
              </Route>
              <Route element={<RoleRoute allowedPermissions={['users.read', 'users.write']} />}>
                <Route path="/users" element={<UsersPage />} />
                <Route path="/users/groups" element={<UsersPage initialView="auth-groups" />} />
                <Route path="/users/resources" element={<UsersPage initialView="resources" />} />
              </Route>
              <Route element={<RoleRoute allowedPermissions={['sql_editor.query']} />}>
                <Route path="/sql-editor" element={<SQLEditorPage />} />
              </Route>
              <Route element={<RoleRoute allowedPermissions={['db_connections.read', 'db_connections.write']} />}>
                <Route path="/db-connections" element={<DBConnectionsPage />} />
              </Route>
              <Route element={<RoleRoute allowedPermissions={['db_metadata.read']} />}>
                <Route path="/db-metadata/inventory" element={<DBMetadataInventoryPage />} />
                <Route path="/db-metadata/objects" element={<DBMetadataObjectsPage />} />
              </Route>
              <Route element={<RoleRoute allowedPermissions={['masking_rules.read', 'masking_rules.write']} />}>
                <Route path="/masking-rules" element={<MaskingRulesPage />} />
                <Route path="/masking-rules/dsl-guide" element={<MaskingDSLGuidePage />} />
              </Route>
              <Route element={<RoleRoute allowedPermissions={['sql_review.read', 'sql_review.write']} />}>
                <Route path="/sql-review-rules" element={<Navigate to="/sql-review-rules/mysql" replace />} />
                <Route path="/sql-review-rules/:engine" element={<SQLReviewRulesPage />} />
              </Route>
              <Route element={<RoleRoute allowedPermissions={['audit_logs.read', 'audit_logs.write']} />}>
                <Route path="/audit-logs" element={<AuditLogsPage />} />
              </Route>
              <Route element={<RoleRoute allowedPermissions={['settings.read', 'settings.write']} />}>
                <Route path="/settings" element={<SettingsPage />} />
              </Route>
            </Route>
          </Route>
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </Suspense>
    </BrowserRouter>
  )
}
