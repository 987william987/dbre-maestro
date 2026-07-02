CREATE TABLE IF NOT EXISTS redis_sensitive_key_prefixes (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    db_connection_id BIGINT UNSIGNED NOT NULL,
    redis_db_index INT NULL COMMENT 'NULL = applies to all Redis DB indexes for the connection',
    key_prefix VARCHAR(512) NOT NULL,
    reason VARCHAR(255) NULL,
    is_active TINYINT(1) NOT NULL DEFAULT 1,
    created_by BIGINT UNSIGNED NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_redis_sensitive_prefix_lookup (db_connection_id, redis_db_index, is_active),
    KEY idx_redis_sensitive_prefix_value (db_connection_id, key_prefix),
    CONSTRAINT fk_redis_sensitive_prefix_connection
        FOREIGN KEY (db_connection_id) REFERENCES db_connections(id)
        ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
