CREATE TABLE IF NOT EXISTS scheduled_sql_reports (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name VARCHAR(128) NOT NULL,
    description TEXT NULL,
    db_connection_id BIGINT UNSIGNED NOT NULL,
    database_name VARCHAR(128) NULL,
    schema_name VARCHAR(128) NULL,
    sql_content MEDIUMTEXT NOT NULL,
    cron_expression VARCHAR(64) NOT NULL,
    timezone VARCHAR(64) NOT NULL DEFAULT 'UTC',
    recipient_user_ids JSON NOT NULL,
    is_active TINYINT(1) NOT NULL DEFAULT 1,
    next_run_at DATETIME(6) NULL,
    last_run_at DATETIME(6) NULL,
    last_status VARCHAR(32) NULL,
    last_error TEXT NULL,
    created_by BIGINT UNSIGNED NOT NULL,
    updated_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_scheduled_sql_reports_due (is_active, next_run_at),
    KEY idx_scheduled_sql_reports_connection (db_connection_id),
    KEY idx_scheduled_sql_reports_created_by (created_by)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS scheduled_sql_report_runs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    report_id BIGINT UNSIGNED NOT NULL,
    status VARCHAR(32) NOT NULL,
    row_count INT NOT NULL DEFAULT 0,
    file_name VARCHAR(255) NULL,
    error_message TEXT NULL,
    started_at DATETIME(6) NOT NULL,
    finished_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_scheduled_sql_report_runs_report (report_id, started_at),
    KEY idx_scheduled_sql_report_runs_status (status, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT IGNORE INTO permissions (permission_key, name, description, category)
VALUES
    ('scheduled_sql_reports.read', 'Read Scheduled SQL Reports', 'Enter Scheduled SQL Reports and view report definitions and run history.', 'scheduled_sql_reports'),
    ('scheduled_sql_reports.write', 'Write Scheduled SQL Reports', 'Create, update, enable, disable, and delete scheduled SQL reports.', 'scheduled_sql_reports');

INSERT IGNORE INTO auth_group_permissions (auth_group_id, permission_id)
SELECT ag.id, p.id
FROM auth_groups ag
INNER JOIN permissions p ON p.permission_key IN ('scheduled_sql_reports.read', 'scheduled_sql_reports.write')
WHERE ag.group_key = 'admin';

INSERT IGNORE INTO user_permissions (user_id, permission_id, granted_by, created_at)
SELECT u.id, p.id, NULL, UTC_TIMESTAMP(6)
FROM users u
INNER JOIN permissions p ON p.permission_key IN ('scheduled_sql_reports.read', 'scheduled_sql_reports.write')
WHERE u.username = 'admin' OR u.is_protected = 1;
