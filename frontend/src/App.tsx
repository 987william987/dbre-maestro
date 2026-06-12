import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { AppShell } from '@/app/layout/AppShell'
import { ProtectedRoute } from '@/app/router/ProtectedRoute'
import { RoleRoute } from '@/app/router/RoleRoute'
import { AuditLogsPage } from '@/modules/audit/pages/AuditLogsPage'
import { LoginPage } from '@/modules/auth/pages/LoginPage'
import { DBConnectionsPage } from '@/modules/db-connections/pages/DBConnectionsPage'
import { DBMetadataInventoryPage } from '@/modules/db-metadata/pages/DBMetadataInventoryPage'
import { DBMetadataObjectsPage } from '@/modules/db-metadata/pages/DBMetadataObjectsPage'
import { MaskingRulesPage } from '@/modules/masking-rules/pages/MaskingRulesPage'
import { SettingsPage } from '@/modules/settings/pages/SettingsPage'
import { SQLEditorPage } from '@/modules/sql-editor/pages/SQLEditorPage'
import { SQLReviewRulesPage } from '@/modules/sql-review-rules/pages/SQLReviewRulesPage'
import { TicketDetailPage } from '@/modules/tickets/pages/TicketDetailPage'
import { NewTicketPage } from '@/modules/tickets/pages/NewTicketPage'
import { TicketsPage } from '@/modules/tickets/pages/TicketsPage'
import { UsersPage } from '@/modules/users/pages/UsersPage'
import { SetupWizard } from '@/pages/setup/SetupWizard'

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/setup" element={<SetupWizard />} />
        <Route element={<ProtectedRoute />}>
          <Route element={<AppShell />}>
            <Route path="/" element={<Navigate to="/tickets" replace />} />
            <Route path="/tickets" element={<TicketsPage />} />
            <Route element={<RoleRoute allowedPermissions={['tickets.apply']} />}>
              <Route path="/tickets/new" element={<NewTicketPage />} />
            </Route>
            <Route path="/tickets/:id" element={<TicketDetailPage />} />
            <Route element={<RoleRoute allowedPermissions={['users.read', 'users.write']} />}>
              <Route path="/users" element={<UsersPage />} />
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
            </Route>
            <Route element={<RoleRoute allowedPermissions={['sql_review.read', 'sql_review.write']} />}>
              <Route path="/sql-review-rules" element={<SQLReviewRulesPage />} />
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
    </BrowserRouter>
  )
}
