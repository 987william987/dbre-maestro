DELETE agp FROM auth_group_permissions agp
INNER JOIN permissions p ON p.id = agp.permission_id
WHERE p.permission_key = 'sql_editor.admin';

DELETE up FROM user_permissions up
INNER JOIN permissions p ON p.id = up.permission_id
WHERE p.permission_key = 'sql_editor.admin';

DELETE FROM permissions WHERE permission_key = 'sql_editor.admin';
