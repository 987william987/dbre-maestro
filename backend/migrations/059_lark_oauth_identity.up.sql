ALTER TABLE users
    ADD COLUMN external_identity_source VARCHAR(32) NOT NULL DEFAULT '' AFTER password,
    ADD COLUMN external_identity_id VARCHAR(255) NOT NULL DEFAULT '' AFTER external_identity_source,
    ADD COLUMN password_login_disabled TINYINT(1) NOT NULL DEFAULT 0 AFTER external_identity_id,
    ADD COLUMN lark_login_open_id VARCHAR(255) NOT NULL DEFAULT '' AFTER password_login_disabled,
    ADD COLUMN lark_login_union_id VARCHAR(255) NOT NULL DEFAULT '' AFTER lark_login_open_id,
    ADD COLUMN lark_display_name VARCHAR(255) NOT NULL DEFAULT '' AFTER lark_login_union_id,
    ADD COLUMN lark_avatar_url VARCHAR(512) NOT NULL DEFAULT '' AFTER lark_display_name,
    ADD COLUMN lark_bound_at DATETIME NULL AFTER lark_avatar_url,
    ADD COLUMN lark_binding_status VARCHAR(32) NOT NULL DEFAULT 'unbound' AFTER lark_bound_at,
    ADD KEY idx_users_external_identity (external_identity_source, external_identity_id),
    ADD KEY idx_users_lark_login_open_id (lark_login_open_id),
    ADD KEY idx_users_lark_login_union_id (lark_login_union_id);

CREATE TABLE IF NOT EXISTS lark_login_states (
    id                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    state             VARCHAR(128)    NOT NULL,
    return_to         VARCHAR(512)    NOT NULL DEFAULT '/',
    user_id           BIGINT UNSIGNED NULL,
    ticket            VARCHAR(128)    NOT NULL DEFAULT '',
    error             VARCHAR(128)    NOT NULL DEFAULT '',
    expires_at        DATETIME        NOT NULL,
    used_at           DATETIME NULL,
    ticket_expires_at DATETIME NULL,
    ticket_used_at    DATETIME NULL,
    created_at        DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uq_lark_login_states_state (state),
    KEY idx_lark_login_states_ticket (ticket),
    KEY idx_lark_login_states_expires_at (expires_at),
    KEY idx_lark_login_states_ticket_expires_at (ticket_expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
