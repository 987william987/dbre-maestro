ALTER TABLE db_account_snapshots
    ADD COLUMN inherits_roles TINYINT(1) NOT NULL DEFAULT 1 AFTER is_superuser,
    ADD COLUMN can_create_role TINYINT(1) NOT NULL DEFAULT 0 AFTER inherits_roles,
    ADD COLUMN can_create_database TINYINT(1) NOT NULL DEFAULT 0 AFTER can_create_role,
    ADD COLUMN can_replicate TINYINT(1) NOT NULL DEFAULT 0 AFTER can_create_database,
    ADD COLUMN can_bypass_rls TINYINT(1) NOT NULL DEFAULT 0 AFTER can_replicate;
