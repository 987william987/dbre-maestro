ALTER TABLE auth_group_permissions
    ADD COLUMN created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6);

INSERT IGNORE INTO permissions (permission_key, name, description, category)
VALUES
    ('tickets.read', 'Read Tickets', 'Enter the Tickets workspace and view tickets within the allowed visibility scope.', 'tickets'),
    ('sql_editor.read', 'Read SQL Editor', 'Enter the SQL Editor workspace.', 'sql_editor');

INSERT IGNORE INTO auth_group_permissions (auth_group_id, permission_id, created_at)
SELECT DISTINCT agp.auth_group_id, read_perm.id, UTC_TIMESTAMP(6)
FROM auth_group_permissions agp
INNER JOIN permissions existing_perm ON existing_perm.id = agp.permission_id
INNER JOIN permissions read_perm ON read_perm.permission_key = 'tickets.read'
WHERE existing_perm.permission_key IN (
    'tickets.apply',
    'tickets.review',
    'tickets.execute',
    'sql_editor.export',
    'sql_editor.export_review',
    'sql_editor.sensitive_apply',
    'sql_editor.sensitive_review'
);

INSERT IGNORE INTO auth_group_permissions (auth_group_id, permission_id, created_at)
SELECT DISTINCT agp.auth_group_id, read_perm.id, UTC_TIMESTAMP(6)
FROM auth_group_permissions agp
INNER JOIN permissions existing_perm ON existing_perm.id = agp.permission_id
INNER JOIN permissions read_perm ON read_perm.permission_key = 'sql_editor.read'
WHERE existing_perm.permission_key IN (
    'sql_editor.query',
    'sql_editor.export',
    'sql_editor.export_review',
    'sql_editor.sensitive_apply',
    'sql_editor.sensitive_review'
);

INSERT IGNORE INTO user_permissions (user_id, permission_id, granted_by, created_at)
SELECT DISTINCT up.user_id, read_perm.id, up.granted_by, UTC_TIMESTAMP(6)
FROM user_permissions up
INNER JOIN permissions existing_perm ON existing_perm.id = up.permission_id
INNER JOIN permissions read_perm ON read_perm.permission_key = 'tickets.read'
WHERE existing_perm.permission_key IN (
    'tickets.apply',
    'tickets.review',
    'tickets.execute',
    'sql_editor.export',
    'sql_editor.export_review',
    'sql_editor.sensitive_apply',
    'sql_editor.sensitive_review'
);

INSERT IGNORE INTO user_permissions (user_id, permission_id, granted_by, created_at)
SELECT DISTINCT up.user_id, read_perm.id, up.granted_by, UTC_TIMESTAMP(6)
FROM user_permissions up
INNER JOIN permissions existing_perm ON existing_perm.id = up.permission_id
INNER JOIN permissions read_perm ON read_perm.permission_key = 'sql_editor.read'
WHERE existing_perm.permission_key IN (
    'sql_editor.query',
    'sql_editor.export',
    'sql_editor.export_review',
    'sql_editor.sensitive_apply',
    'sql_editor.sensitive_review'
);
