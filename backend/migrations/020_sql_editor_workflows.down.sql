DELETE agp
FROM auth_group_permissions agp
JOIN auth_groups ag ON ag.id = agp.auth_group_id
JOIN permissions p ON p.id = agp.permission_id
WHERE p.permission_key IN ('settings.read', 'settings.write')
  AND ag.group_key IN ('dba', 'admin');

DELETE FROM permissions
WHERE permission_key IN ('settings.read', 'settings.write');

DROP TABLE IF EXISTS ticket_scopes;

ALTER TABLE tickets
    DROP COLUMN revoked_by,
    DROP COLUMN revoked_at,
    DROP COLUMN approved_until,
    DROP COLUMN approved_duration_minutes,
    MODIFY COLUMN ticket_type VARCHAR(16) NOT NULL COMMENT 'ddl|dml';
