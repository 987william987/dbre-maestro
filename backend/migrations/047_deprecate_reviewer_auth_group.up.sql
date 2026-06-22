UPDATE auth_groups
SET
    name = 'Reviewer (Deprecated)',
    description = 'Legacy reviewer group. Use Data Owner for regular tickets or Security for export and sensitive data workflows.',
    updated_at = UTC_TIMESTAMP(6)
WHERE group_key = 'reviewer';
