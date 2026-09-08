ALTER TABLE db_account_grant_snapshots
    ADD COLUMN grant_statement TEXT NULL AFTER grant_kind;
