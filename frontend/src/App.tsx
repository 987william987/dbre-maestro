import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { AppShell } from '@/app/layout/AppShell'
import { ProtectedRoute } from '@/app/router/ProtectedRoute'
import { RoleRoute } from '@/app/router/RoleRoute'
import { AuditLogsPage } from '@/modules/audit/pages/AuditLogsPage'
import { LoginPage } from '@/modules/auth/pages/LoginPage'
import { DBConnectionsPage } from '@/modules/db-connections/pages/DBConnectionsPage'
import { TicketDetailPage } from '@/modules/tickets/pages/TicketDetailPage'
import { NewTicketPage } from '@/modules/tickets/pages/NewTicketPage'
import { TicketsPage } from '@/modules/tickets/pages/TicketsPage'
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
            <Route path="/tickets/new" element={<NewTicketPage />} />
            <Route path="/tickets/:id" element={<TicketDetailPage />} />
            <Route element={<RoleRoute allowedGroups={['dba', 'admin']} />}>
              <Route path="/db-connections" element={<DBConnectionsPage />} />
              <Route path="/audit-logs" element={<AuditLogsPage />} />
            </Route>
          </Route>
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  )
}
