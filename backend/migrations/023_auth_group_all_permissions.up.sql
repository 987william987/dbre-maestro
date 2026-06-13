ALTER TABLE auth_groups
    ADD COLUMN is_all_permissions TINYINT(1) NOT NULL DEFAULT 0;

UPDATE auth_groups SET is_all_permissions = 1 WHERE group_key = 'admin';
