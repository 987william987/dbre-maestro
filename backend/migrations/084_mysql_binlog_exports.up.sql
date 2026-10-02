CREATE TABLE IF NOT EXISTS mysql_binlog_export_jobs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    requested_by BIGINT UNSIGNED NOT NULL,
    source_connection_id BIGINT UNSIGNED NOT NULL,
    range_mode VARCHAR(16) NOT NULL COMMENT 'time|position',
    timezone VARCHAR(64) NOT NULL DEFAULT 'UTC',
    requested_start_time DATETIME(6) NULL,
    requested_end_time DATETIME(6) NULL,
    requested_start_file VARCHAR(255) NULL,
    requested_start_pos BIGINT UNSIGNED NULL,
    requested_end_file VARCHAR(255) NULL,
    requested_end_pos BIGINT UNSIGNED NULL,
    actual_start_file VARCHAR(255) NULL,
    actual_start_pos BIGINT UNSIGNED NULL,
    actual_end_file VARCHAR(255) NULL,
    actual_end_pos BIGINT UNSIGNED NULL,
    source_database_name VARCHAR(255) NULL,
    source_tables JSON NOT NULL,
    dml_types JSON NOT NULL,
    acknowledged_unfiltered TINYINT(1) NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'queued' COMMENT 'queued|running|cancel_requested|succeeded|failed|cancelled|interrupted',
    phase VARCHAR(32) NOT NULL DEFAULT 'queued',
    progress_message VARCHAR(255) NULL,
    generator VARCHAR(64) NOT NULL DEFAULT 'my2sql',
    generator_version VARCHAR(128) NOT NULL DEFAULT '',
    error_code VARCHAR(64) NULL,
    error_message TEXT NULL,
    cancel_requested_by BIGINT UNSIGNED NULL,
    retry_of_job_id BIGINT UNSIGNED NULL,
    binlog_file_count INT UNSIGNED NULL,
    queue_wait_ms BIGINT UNSIGNED NULL,
    range_resolve_ms BIGINT UNSIGNED NULL,
    forward_generation_ms BIGINT UNSIGNED NULL,
    rollback_generation_ms BIGINT UNSIGNED NULL,
    artifact_persist_ms BIGINT UNSIGNED NULL,
    total_duration_ms BIGINT UNSIGNED NULL,
	artifact_expires_at DATETIME(6) NULL,
    started_at DATETIME(6) NULL,
    completed_at DATETIME(6) NULL,
    interrupted_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    KEY idx_mysql_binlog_export_jobs_status_created (status, created_at),
    KEY idx_mysql_binlog_export_jobs_connection_created (source_connection_id, created_at),
    KEY idx_mysql_binlog_export_jobs_requester_created (requested_by, created_at),
    KEY idx_mysql_binlog_export_jobs_retry (retry_of_job_id),
    CONSTRAINT fk_mysql_binlog_export_jobs_requester FOREIGN KEY (requested_by) REFERENCES users(id),
    CONSTRAINT fk_mysql_binlog_export_jobs_connection FOREIGN KEY (source_connection_id) REFERENCES db_connections(id),
    CONSTRAINT fk_mysql_binlog_export_jobs_cancel_user FOREIGN KEY (cancel_requested_by) REFERENCES users(id),
    CONSTRAINT fk_mysql_binlog_export_jobs_retry FOREIGN KEY (retry_of_job_id) REFERENCES mysql_binlog_export_jobs(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS mysql_binlog_export_artifacts (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    job_id BIGINT UNSIGNED NOT NULL,
    artifact_kind VARCHAR(32) NOT NULL COMMENT 'forward_sql|rollback_sql',
    compression VARCHAR(16) NOT NULL DEFAULT 'gzip',
    sql_encrypted LONGBLOB NULL,
    plaintext_sha256 CHAR(64) NOT NULL,
    plaintext_bytes BIGINT UNSIGNED NOT NULL,
    compressed_bytes BIGINT UNSIGNED NOT NULL,
    statement_count INT UNSIGNED NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    purged_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_mysql_binlog_export_artifacts_job_kind (job_id, artifact_kind),
    KEY idx_mysql_binlog_export_artifacts_expiry (expires_at, purged_at),
    CONSTRAINT fk_mysql_binlog_export_artifacts_job FOREIGN KEY (job_id) REFERENCES mysql_binlog_export_jobs(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO permissions (permission_key, name, description, category)
VALUES
    ('binlog_exports.read', 'Read Binlog Exports', 'View MySQL binlog files, export jobs, previews, and artifacts for scoped database connections.', 'binlog_exports'),
    ('binlog_exports.execute', 'Execute Binlog Exports', 'Create, cancel, and retry MySQL binlog export jobs for scoped database connections.', 'binlog_exports');

INSERT IGNORE INTO auth_group_permissions (auth_group_id, permission_id)
SELECT ag.id, p.id
FROM auth_groups ag
INNER JOIN permissions p ON p.permission_key IN ('binlog_exports.read', 'binlog_exports.execute')
WHERE ag.group_key = 'dba';
