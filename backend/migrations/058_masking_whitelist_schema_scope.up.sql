ALTER TABLE masking_whitelist
    ADD COLUMN schema_name VARCHAR(128) NOT NULL DEFAULT '' AFTER database_name;

ALTER TABLE masking_whitelist
    DROP INDEX idx_masking_whitelist_lookup,
    ADD KEY idx_masking_whitelist_lookup (db_connection_id, database_name, schema_name, table_name, column_name);
