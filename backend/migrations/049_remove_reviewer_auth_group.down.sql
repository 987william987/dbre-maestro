INSERT IGNORE INTO auth_groups (group_key, name, description, is_system, is_protected, created_at, updated_at)
VALUES (
    'reviewer',
    'Reviewer (Deprecated)',
    'Legacy reviewer group. Use Data Owner for regular tickets or Security for export and sensitive data workflows.',
    1,
    0,
    UTC_TIMESTAMP(6),
    UTC_TIMESTAMP(6)
);

INSERT IGNORE INTO auth_group_permissions (auth_group_id, permission_id, created_at)
SELECT ag.id, p.id, UTC_TIMESTAMP(6)
FROM auth_groups ag
INNER JOIN permissions p ON p.permission_key IN (
    'tickets.review',
    'sql_editor.export_review',
    'sql_editor.sensitive_review'
)
WHERE ag.group_key = 'reviewer';
