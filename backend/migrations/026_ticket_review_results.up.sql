ALTER TABLE tickets
    ADD COLUMN database_name VARCHAR(255) NULL AFTER db_connection_id;

CREATE TABLE IF NOT EXISTS ticket_review_results (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ticket_id  BIGINT UNSIGNED NOT NULL,
    seq        INT             NOT NULL COMMENT 'SQL statement sequence (1-based)',
    sql_stmt   MEDIUMTEXT      NOT NULL,
    scan_rows  BIGINT          NOT NULL DEFAULT 0,
    status     VARCHAR(16)     NOT NULL COMMENT 'pass|warn|error',
    message    TEXT,
    created_at DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_ticket_review_results_ticket (ticket_id),
    KEY idx_ticket_review_results_ticket_seq (ticket_id, seq)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
