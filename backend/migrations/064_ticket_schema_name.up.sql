ALTER TABLE tickets
    ADD COLUMN schema_name VARCHAR(255) NULL AFTER database_name;
