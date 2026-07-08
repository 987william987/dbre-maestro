-- TE6: export_requests with secure download token
CREATE TABLE IF NOT EXISTS export_requests (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ticket_id       BIGINT UNSIGNED,
    requester_id    BIGINT UNSIGNED NOT NULL,
    approver_id     BIGINT UNSIGNED,
    download_token  CHAR(64)        NOT NULL COMMENT 'hex(crypto/rand 32 bytes)',
    sql_content     MEDIUMTEXT      NOT NULL,
    db_connection_id BIGINT UNSIGNED NOT NULL,
    row_count       INT UNSIGNED,
    file_path       VARCHAR(512),
    status          VARCHAR(16)     NOT NULL DEFAULT 'pending'
                    COMMENT 'pending|approved|rejected|ready|expired',
    expires_at      DATETIME        NOT NULL,
    downloaded_at   DATETIME,
    created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uq_download_token (download_token),
    KEY idx_export_requester (requester_id),
    KEY idx_export_expiry (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
