INSERT IGNORE INTO permissions (permission_key, name, description, category)
VALUES
    ('sql_editor.export_review', 'Review SQL Editor Export', 'Review SQL export tickets created from SQL editor.', 'sql_editor');

INSERT IGNORE INTO auth_group_permissions (auth_group_id, permission_id)
SELECT ag.id, p.id
FROM auth_groups ag
JOIN permissions p ON (
    (ag.group_key = 'reviewer' AND p.permission_key IN ('sql_editor.export_review'))
    OR (ag.group_key = 'admin')
);
