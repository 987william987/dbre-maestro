-- TE1: sessions table for revocable refresh tokens
CREATE TABLE IF NOT EXISTS sessions (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id         BIGINT UNSIGNED NOT NULL,
    token_hash      VARCHAR(64)     NOT NULL COMMENT 'SHA-256 hex of refresh token',
    user_agent      VARCHAR(512),
    ip_address      VARCHAR(45),
    expires_at      DATETIME        NOT NULL,
    revoked_at      DATETIME,
    created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uq_token_hash (token_hash),
    KEY idx_sessions_user (user_id),
    KEY idx_sessions_expiry (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
