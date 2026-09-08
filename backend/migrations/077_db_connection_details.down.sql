DELETE agp FROM auth_group_permissions agp
INNER JOIN permissions p ON p.id = agp.permission_id
WHERE p.permission_key IN ('db_connections.overview', 'db_connections.databases', 'db_connections.accounts');

DELETE up FROM user_permissions up
INNER JOIN permissions p ON p.id = up.permission_id
WHERE p.permission_key IN ('db_connections.overview', 'db_connections.databases', 'db_connections.accounts');

DELETE FROM permissions
WHERE permission_key IN ('db_connections.overview', 'db_connections.databases', 'db_connections.accounts');

DROP TABLE IF EXISTS db_account_snapshot_statuses;
DROP TABLE IF EXISTS db_account_grant_snapshots;
DROP TABLE IF EXISTS db_account_snapshots;
DROP TABLE IF EXISTS db_database_snapshots;
