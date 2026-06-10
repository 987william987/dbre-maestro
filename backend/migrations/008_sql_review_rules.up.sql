CREATE TABLE sql_review_rules (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    rule_name   VARCHAR(64)     NOT NULL,
    enabled     TINYINT(1)      NOT NULL DEFAULT 1,
    threshold   BIGINT          NULL COMMENT 'rule-specific numeric threshold (e.g. row count for high_row_count)',
    description VARCHAR(255)    NOT NULL DEFAULT '',
    updated_by  BIGINT UNSIGNED NULL,
    updated_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uq_rule_name (rule_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Seed: rules that correspond to implemented checks in sqlreview package
INSERT INTO sql_review_rules (rule_name, enabled, threshold, description) VALUES
('full_table_scan', 1, NULL,  'Reject queries that trigger a full table scan (EXPLAIN type=ALL)'),
('high_row_count',  1, 10000, 'Reject queries where estimated row count exceeds threshold');
