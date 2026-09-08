INSERT IGNORE INTO permissions (permission_key, name, description, category)
VALUES ('sql_editor.admin', 'SQL Editor Admin Mode', 'Execute direct commands with the readwrite database credential.', 'sql_editor');

INSERT IGNORE INTO auth_group_permissions (auth_group_id, permission_id)
SELECT ag.id, p.id
FROM auth_groups ag
INNER JOIN permissions p ON p.permission_key = 'sql_editor.admin'
WHERE ag.group_key = 'admin';
