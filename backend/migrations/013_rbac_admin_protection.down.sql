ALTER TABLE resource_groups
    DROP COLUMN updated_at;

ALTER TABLE users
    DROP COLUMN is_protected;
