CREATE TABLE IF NOT EXISTS ticket_workflow_snapshots (
    ticket_id BIGINT UNSIGNED NOT NULL,
    workflow_rule_id BIGINT UNSIGNED NULL,
    workflow_rule_name VARCHAR(128) NOT NULL DEFAULT '',
    approval_enabled TINYINT(1) NOT NULL DEFAULT 1,
    approval_user_ids JSON NOT NULL,
    executor_user_ids JSON NOT NULL,
    admin_user_ids JSON NOT NULL,
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    error_message VARCHAR(255) NOT NULL DEFAULT '',
    resolution_trace JSON NOT NULL,
    resolved_at DATETIME(6) NOT NULL,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    PRIMARY KEY (ticket_id),
    KEY idx_ticket_workflow_snapshots_rule (workflow_rule_id),
    CONSTRAINT fk_ticket_workflow_snapshots_ticket
        FOREIGN KEY (ticket_id) REFERENCES tickets(id)
        ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
