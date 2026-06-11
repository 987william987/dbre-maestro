ALTER TABLE masking_rules
    ADD COLUMN database_name VARCHAR(128) NOT NULL DEFAULT '' AFTER db_connection_id,
    ADD COLUMN schema_name VARCHAR(128) NOT NULL DEFAULT '' AFTER database_name;

ALTER TABLE masking_rules
    DROP INDEX uq_masking_rule,
    ADD UNIQUE KEY uq_masking_rule_scope (db_connection_id, database_name, schema_name, table_name, column_name);

ALTER TABLE masking_rules
    ADD KEY idx_masking_scope_lookup (db_connection_id, database_name, schema_name, table_name, column_name);
