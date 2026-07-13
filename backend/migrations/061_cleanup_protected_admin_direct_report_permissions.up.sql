DELETE up FROM user_permissions up
INNER JOIN users u ON u.id = up.user_id
INNER JOIN permissions p ON p.id = up.permission_id
WHERE u.is_protected = 1
  AND p.permission_key IN (
    'scheduled_sql_reports.read',
    'scheduled_sql_reports.write'
  );
