UPDATE auth_groups
SET
    name = 'Reviewer',
    description = 'Can review change requests and sensitive data workflows.',
    updated_at = UTC_TIMESTAMP(6)
WHERE group_key = 'reviewer';
