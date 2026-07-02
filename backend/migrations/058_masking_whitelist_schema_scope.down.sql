ALTER TABLE masking_whitelist
    DROP INDEX idx_masking_whitelist_lookup,
    ADD KEY idx_masking_whitelist_lookup (db_connection_id, database_name, table_name, column_name);

ALTER TABLE masking_whitelist
    DROP COLUMN schema_name;
