ALTER TABLE sessions
    ADD COLUMN auth_method VARCHAR(32) NOT NULL DEFAULT 'password' AFTER ip_address,
    ADD COLUMN auth_provider VARCHAR(64) NOT NULL DEFAULT '' AFTER auth_method,
    ADD COLUMN mfa_satisfied TINYINT(1) NOT NULL DEFAULT 0 AFTER auth_provider,
    ADD COLUMN mfa_source VARCHAR(64) NOT NULL DEFAULT '' AFTER mfa_satisfied;

CREATE TABLE IF NOT EXISTS sso_login_states (
    id                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    state             VARCHAR(128)    NOT NULL,
    return_to         VARCHAR(512)    NOT NULL DEFAULT '/',
    user_id           BIGINT UNSIGNED NULL,
    ticket            VARCHAR(128)    NOT NULL DEFAULT '',
    error             VARCHAR(128)    NOT NULL DEFAULT '',
    identity_json     JSON            NULL,
    expires_at        DATETIME        NOT NULL,
    used_at           DATETIME NULL,
    ticket_expires_at DATETIME NULL,
    ticket_used_at    DATETIME NULL,
    created_at        DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uq_sso_login_states_state (state),
    KEY idx_sso_login_states_ticket (ticket),
    KEY idx_sso_login_states_expires_at (expires_at),
    KEY idx_sso_login_states_ticket_expires_at (ticket_expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
