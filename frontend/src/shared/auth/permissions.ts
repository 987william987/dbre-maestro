export const TICKET_WORKSPACE_PERMISSIONS = [
  'tickets.apply',
  'tickets.review',
  'tickets.execute',
  'sql_editor.export',
  'sql_editor.export_review',
  'sql_editor.sensitive_apply',
  'sql_editor.sensitive_review',
] as const

export function hasAnyPermission(userPermissions: string[], allowedPermissions: readonly string[]) {
  return userPermissions.some((permission) => allowedPermissions.includes(permission))
}
