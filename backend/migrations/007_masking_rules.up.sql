CREATE TABLE masking_rules (
    id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    db_connection_id BIGINT UNSIGNED NULL COMMENT 'NULL = global rule applies to all connections',
    table_name     VARCHAR(128) NOT NULL,
    column_name    VARCHAR(128) NOT NULL,
    mask_mode      ENUM('full', 'partial', 'hash') NOT NULL DEFAULT 'full',
    created_by     BIGINT UNSIGNED NOT NULL,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uq_masking_rule (db_connection_id, table_name, column_name),
    KEY idx_masking_conn (db_connection_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
