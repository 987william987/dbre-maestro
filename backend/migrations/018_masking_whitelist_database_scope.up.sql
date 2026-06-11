ALTER TABLE masking_whitelist
    ADD COLUMN database_name VARCHAR(128) NOT NULL DEFAULT '' AFTER db_connection_id;

ALTER TABLE masking_whitelist
    DROP INDEX idx_whitelist_conn,
    ADD KEY idx_masking_whitelist_lookup (db_connection_id, database_name, table_name, column_name);
