DELETE up FROM user_permissions up
INNER JOIN permissions p ON p.id = up.permission_id
WHERE p.permission_key IN ('tickets.read', 'sql_editor.read');

DELETE agp FROM auth_group_permissions agp
INNER JOIN permissions p ON p.id = agp.permission_id
WHERE p.permission_key IN ('tickets.read', 'sql_editor.read');

DELETE FROM permissions
WHERE permission_key IN ('tickets.read', 'sql_editor.read');
