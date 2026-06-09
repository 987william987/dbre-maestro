CREATE TABLE IF NOT EXISTS tickets (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ticket_no       VARCHAR(16)     NOT NULL COMMENT 'T-001 format',
    title           VARCHAR(255)    NOT NULL,
    description     TEXT,
    sql_content     MEDIUMTEXT      NOT NULL,
    ticket_type     VARCHAR(16)     NOT NULL COMMENT 'ddl|dml',
    db_connection_id BIGINT UNSIGNED,
    status          VARCHAR(32)     NOT NULL DEFAULT 'pending_review'
                    COMMENT 'pending_review|approved|rejected|pending_execution|executing|completed|failed|stopped|interrupted',
    submitter_id    BIGINT UNSIGNED NOT NULL,
    reviewer_id     BIGINT UNSIGNED,
    executor_id     BIGINT UNSIGNED,
    review_comment  TEXT,
    rejection_reason TEXT,
    scheduled_at    DATETIME,
    started_at      DATETIME,
    completed_at    DATETIME,
    created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uq_ticket_no (ticket_no),
    KEY idx_tickets_submitter (submitter_id),
    KEY idx_tickets_status (status),
    KEY idx_tickets_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS ticket_executions (
    id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ticket_id    BIGINT UNSIGNED NOT NULL,
    seq          INT             NOT NULL COMMENT 'SQL statement sequence (1-based)',
    sql_stmt     MEDIUMTEXT      NOT NULL,
    status       VARCHAR(16)     NOT NULL COMMENT 'pending|running|completed|failed',
    rows_affected BIGINT,
    error_msg    TEXT,
    started_at   DATETIME,
    completed_at DATETIME,
    PRIMARY KEY (id),
    KEY idx_exec_ticket (ticket_id),
    KEY idx_exec_ticket_seq (ticket_id, seq)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
