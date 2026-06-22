ALTER TABLE users
    ADD COLUMN mfa_enabled TINYINT(1) NOT NULL DEFAULT 0 AFTER is_active,
    ADD COLUMN mfa_secret_encrypted VARBINARY(512) NULL AFTER mfa_enabled,
    ADD COLUMN mfa_enabled_at DATETIME NULL AFTER mfa_secret_encrypted;
