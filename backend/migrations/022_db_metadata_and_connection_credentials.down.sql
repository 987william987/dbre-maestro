DELETE agp
FROM auth_group_permissions agp
INNER JOIN permissions p ON p.id = agp.permission_id
WHERE p.permission_key IN ('db_metadata.read');

DELETE FROM permissions
WHERE permission_key IN ('db_metadata.read');

DROP TABLE IF EXISTS db_object_snapshots;
DROP TABLE IF EXISTS cloud_db_inventory_snapshots;
DROP TABLE IF EXISTS db_connection_credentials;
