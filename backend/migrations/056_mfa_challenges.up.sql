CREATE TABLE IF NOT EXISTS mfa_challenges (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    token_id VARCHAR(128) NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    setup TINYINT(1) NOT NULL DEFAULT 0,
    expires_at DATETIME(6) NOT NULL,
    attempt_count INT NOT NULL DEFAULT 0,
    used_at DATETIME(6) NULL,
    revoked_at DATETIME(6) NULL,
    created_ip VARCHAR(64) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_mfa_challenges_token_id (token_id),
    KEY idx_mfa_challenges_user (user_id, created_at),
    KEY idx_mfa_challenges_expires (expires_at),
    KEY idx_mfa_challenges_active (user_id, used_at, revoked_at, expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
