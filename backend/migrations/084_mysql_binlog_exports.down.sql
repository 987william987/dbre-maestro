DELETE agp FROM auth_group_permissions agp
INNER JOIN permissions p ON p.id = agp.permission_id
WHERE p.permission_key IN ('binlog_exports.read', 'binlog_exports.execute');

DELETE up FROM user_permissions up
INNER JOIN permissions p ON p.id = up.permission_id
WHERE p.permission_key IN ('binlog_exports.read', 'binlog_exports.execute');

DELETE FROM permissions
WHERE permission_key IN ('binlog_exports.read', 'binlog_exports.execute');

DROP TABLE IF EXISTS mysql_binlog_export_artifacts;
DROP TABLE IF EXISTS mysql_binlog_export_jobs;
