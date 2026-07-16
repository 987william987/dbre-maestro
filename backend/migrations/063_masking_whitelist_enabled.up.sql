ALTER TABLE masking_whitelist
    ADD COLUMN enabled TINYINT(1) NOT NULL DEFAULT 1 AFTER column_name;
