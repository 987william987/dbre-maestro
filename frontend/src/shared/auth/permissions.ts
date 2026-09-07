export const TICKET_WORKSPACE_PERMISSIONS = [
  'tickets.read',
] as const

export function hasAnyPermission(userPermissions: string[], allowedPermissions: readonly string[]) {
  return userPermissions.some((permission) => allowedPermissions.includes(permission))
}

export function defaultRouteForPermissions(userPermissions: string[]) {
  if (hasAnyPermission(userPermissions, TICKET_WORKSPACE_PERMISSIONS)) {
    return '/tickets'
  }
  if (userPermissions.includes('sql_editor.read')) {
    return '/sql-editor'
  }
  if (userPermissions.includes('scheduled_sql_reports.read') || userPermissions.includes('scheduled_sql_reports.write')) {
    return '/scheduled-sql-reports'
  }
  if (userPermissions.includes('users.read') || userPermissions.includes('users.write')) {
    return '/users'
  }
  if (userPermissions.some((permission) => ['db_connections.read', 'db_connections.write', 'db_connections.overview', 'db_connections.databases', 'db_connections.accounts'].includes(permission))) {
    return '/db-connections'
  }
  if (userPermissions.includes('db_metadata.read')) {
    return '/db-metadata/inventory'
  }
  if (userPermissions.includes('masking_rules.read') || userPermissions.includes('masking_rules.write')) {
    return '/masking-rules'
  }
  if (userPermissions.includes('sql_review.read') || userPermissions.includes('sql_review.write')) {
    return '/sql-review-rules/mysql'
  }
  if (userPermissions.includes('audit_logs.read') || userPermissions.includes('audit_logs.write')) {
    return '/audit-logs'
  }
  if (userPermissions.includes('settings.read') || userPermissions.includes('settings.write')) {
    return '/settings'
  }
  return '/login'
}
