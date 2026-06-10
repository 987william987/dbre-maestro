-- Masking whitelist: users or auth groups exempt from data masking on specific columns.
-- At least one of user_id or auth_group must be non-null.
CREATE TABLE IF NOT EXISTS masking_whitelist (
    id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    db_connection_id BIGINT UNSIGNED NULL     COMMENT 'NULL = all connections',
    table_name       VARCHAR(128)    NOT NULL,
    column_name      VARCHAR(128)    NOT NULL,
    user_id          BIGINT UNSIGNED NULL     COMMENT 'grant individual user',
    auth_group       VARCHAR(32)     NULL     COMMENT 'grant whole auth group',
    created_by       BIGINT UNSIGNED NOT NULL,
    created_at       DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_whitelist_conn (db_connection_id, table_name, column_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
