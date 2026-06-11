ALTER TABLE masking_whitelist
    DROP INDEX idx_masking_whitelist_lookup,
    ADD KEY idx_whitelist_conn (db_connection_id, table_name, column_name);

ALTER TABLE masking_whitelist
    DROP COLUMN database_name;
