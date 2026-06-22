ALTER TABLE users
    DROP COLUMN mfa_enabled_at,
    DROP COLUMN mfa_secret_encrypted,
    DROP COLUMN mfa_enabled;
