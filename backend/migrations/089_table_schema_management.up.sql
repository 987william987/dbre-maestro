INSERT IGNORE INTO permissions (permission_key, name, description, category)
VALUES
    ('table_schemas.read', 'Read Table Schemas', 'Preview and export table schemas for scoped MySQL database connections.', 'table_schemas'),
    ('table_schemas.sync', 'Sync Table Schemas', 'Create table schemas on scoped MySQL database connections.', 'table_schemas');

INSERT IGNORE INTO auth_group_permissions (auth_group_id, permission_id)
SELECT ag.id, p.id
FROM auth_groups ag
INNER JOIN permissions p ON p.permission_key IN ('table_schemas.read', 'table_schemas.sync')
WHERE ag.group_key = 'dba';
