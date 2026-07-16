ALTER TABLE masking_rules
    ADD COLUMN enabled TINYINT(1) NOT NULL DEFAULT 1 AFTER mask_config;
