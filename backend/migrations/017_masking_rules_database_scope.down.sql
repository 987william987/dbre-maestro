ALTER TABLE masking_rules
    DROP INDEX idx_masking_scope_lookup,
    DROP INDEX uq_masking_rule_scope,
    ADD UNIQUE KEY uq_masking_rule (db_connection_id, table_name, column_name);

ALTER TABLE masking_rules
    DROP COLUMN schema_name,
    DROP COLUMN database_name;
