DROP TABLE IF EXISTS sso_login_states;

ALTER TABLE sessions
    DROP COLUMN mfa_source,
    DROP COLUMN mfa_satisfied,
    DROP COLUMN auth_provider,
    DROP COLUMN auth_method;
