INSERT IGNORE INTO user_permissions (user_id, permission_id, granted_by, created_at)
SELECT u.id, p.id, NULL, UTC_TIMESTAMP(6)
FROM users u
INNER JOIN permissions p ON p.permission_key IN (
  'scheduled_sql_reports.read',
  'scheduled_sql_reports.write'
)
WHERE u.is_protected = 1;
