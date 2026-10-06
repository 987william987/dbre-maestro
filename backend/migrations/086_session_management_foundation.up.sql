ALTER TABLE db_connection_credentials
    MODIFY COLUMN credential_role VARCHAR(32) NOT NULL COMMENT 'readonly|readwrite|rollback|operations';

INSERT IGNORE INTO permissions (permission_key, name, description, category)
VALUES
    ('db_sessions.read', 'Read DB Sessions', 'View live database sessions for scoped database connections.', 'db_sessions'),
    ('db_sessions.kill', 'Kill DB Sessions', 'Cancel queries and terminate sessions for scoped database connections.', 'db_sessions'),
    ('db_sessions.loop_kill', 'Loop Kill DB Sessions', 'Create and stop bounded loop-kill jobs for scoped database connections.', 'db_sessions');

INSERT IGNORE INTO auth_group_permissions (auth_group_id, permission_id)
SELECT ag.id, p.id
FROM auth_groups ag
INNER JOIN permissions p ON p.permission_key IN ('db_sessions.read', 'db_sessions.kill', 'db_sessions.loop_kill')
WHERE ag.group_key = 'dba';
