ALTER TABLE masking_rules
    DROP COLUMN mask_config,
    DROP COLUMN match_type,
    MODIFY COLUMN mask_mode ENUM('full', 'partial', 'hash') NOT NULL DEFAULT 'full';
