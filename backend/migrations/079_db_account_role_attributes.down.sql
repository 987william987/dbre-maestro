ALTER TABLE db_account_snapshots
    DROP COLUMN can_bypass_rls,
    DROP COLUMN can_replicate,
    DROP COLUMN can_create_database,
    DROP COLUMN can_create_role,
    DROP COLUMN inherits_roles;
