DROP TABLE IF EXISTS scheduled_sql_report_runs;
DROP TABLE IF EXISTS scheduled_sql_reports;

DELETE agp FROM auth_group_permissions agp
INNER JOIN permissions p ON p.id = agp.permission_id
WHERE p.permission_key IN ('scheduled_sql_reports.read', 'scheduled_sql_reports.write');

DELETE up FROM user_permissions up
INNER JOIN permissions p ON p.id = up.permission_id
WHERE p.permission_key IN ('scheduled_sql_reports.read', 'scheduled_sql_reports.write');

DELETE FROM permissions
WHERE permission_key IN ('scheduled_sql_reports.read', 'scheduled_sql_reports.write');
