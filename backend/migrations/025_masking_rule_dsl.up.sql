ALTER TABLE masking_rules
    MODIFY COLUMN mask_mode VARCHAR(32) NOT NULL DEFAULT 'full',
    ADD COLUMN match_type VARCHAR(16) NOT NULL DEFAULT 'exact' AFTER column_name,
    ADD COLUMN mask_config JSON NULL AFTER mask_mode;
