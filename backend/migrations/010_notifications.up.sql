CREATE TABLE IF NOT EXISTS notifications (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id       BIGINT UNSIGNED NOT NULL,
    type          VARCHAR(64)     NOT NULL COMMENT 'ticket_approved|ticket_rejected|ticket_executed|export_approved|export_rejected',
    title         VARCHAR(255)    NOT NULL,
    body          TEXT            NOT NULL,
    resource_type VARCHAR(32)     NULL,
    resource_id   BIGINT UNSIGNED NULL,
    is_read       TINYINT(1)      NOT NULL DEFAULT 0,
    created_at    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_user_unread (user_id, is_read, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
