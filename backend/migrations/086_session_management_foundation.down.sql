DELETE agp FROM auth_group_permissions agp
INNER JOIN permissions p ON p.id = agp.permission_id
WHERE p.permission_key IN ('db_sessions.read', 'db_sessions.kill', 'db_sessions.loop_kill');

DELETE up FROM user_permissions up
INNER JOIN permissions p ON p.id = up.permission_id
WHERE p.permission_key IN ('db_sessions.read', 'db_sessions.kill', 'db_sessions.loop_kill');

DELETE FROM permissions
WHERE permission_key IN ('db_sessions.read', 'db_sessions.kill', 'db_sessions.loop_kill');

ALTER TABLE db_connection_credentials
    MODIFY COLUMN credential_role VARCHAR(32) NOT NULL COMMENT 'readonly|readwrite|rollback';
