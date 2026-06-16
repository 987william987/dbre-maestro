DELETE agp
FROM auth_group_permissions agp
INNER JOIN permissions p ON p.id = agp.permission_id
WHERE p.permission_key IN ('sql_editor.export_review');

DELETE FROM permissions
WHERE permission_key IN ('sql_editor.export_review');
