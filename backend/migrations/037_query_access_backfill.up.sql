ALTER TABLE tickets
    MODIFY COLUMN ticket_type VARCHAR(32) NOT NULL COMMENT 'ddl|dml|redis_command|sql_export|sensitive_query_access|query_access';

CREATE TABLE IF NOT EXISTS query_access_ticket_items (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ticket_id BIGINT UNSIGNED NOT NULL,
    connection_id BIGINT UNSIGNED NOT NULL,
    scope_mode VARCHAR(16) NOT NULL COMMENT 'database|table',
    database_name VARCHAR(255) NOT NULL,
    table_name VARCHAR(255) NULL,
    created_at DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    KEY idx_query_access_ticket_items_ticket (ticket_id),
    KEY idx_query_access_ticket_items_connection (connection_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS query_access_grants (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    subject_type VARCHAR(16) NOT NULL,
    subject_id BIGINT UNSIGNED NOT NULL,
    connection_id BIGINT UNSIGNED NOT NULL,
    database_name VARCHAR(255) NULL,
    table_name VARCHAR(255) NULL,
    granted_via VARCHAR(32) NOT NULL DEFAULT 'ticket',
    source_ticket_id BIGINT UNSIGNED NULL,
    expires_at DATETIME(6) NULL,
    revoked_at DATETIME(6) NULL,
    revoked_by BIGINT UNSIGNED NULL,
    created_by BIGINT UNSIGNED NULL,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    KEY idx_query_access_grants_subject (subject_type, subject_id),
    KEY idx_query_access_grants_connection (connection_id),
    KEY idx_query_access_grants_ticket (source_ticket_id),
    KEY idx_query_access_grants_expiry (expires_at),
    KEY idx_query_access_grants_active (revoked_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
