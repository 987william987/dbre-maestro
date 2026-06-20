ALTER TABLE platform_settings
    ADD COLUMN created_at DATETIME(6) NULL AFTER value;

UPDATE platform_settings
SET created_at = updated_at
WHERE created_at IS NULL;

ALTER TABLE platform_settings
    MODIFY COLUMN created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6);
