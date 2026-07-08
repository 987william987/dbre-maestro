CREATE TABLE IF NOT EXISTS notification_deliveries (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    notification_type VARCHAR(64) NOT NULL,
    resource_type VARCHAR(32) NOT NULL,
    resource_id BIGINT UNSIGNED NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    channel VARCHAR(32) NOT NULL COMMENT 'in_app|lark',
    status VARCHAR(32) NOT NULL COMMENT 'sent|failed|skipped',
    attempts INT NOT NULL DEFAULT 0,
    error_message TEXT NULL,
    created_at DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    KEY idx_notification_deliveries_resource (resource_type, resource_id),
    KEY idx_notification_deliveries_user (user_id, created_at),
    KEY idx_notification_deliveries_status (channel, status, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
