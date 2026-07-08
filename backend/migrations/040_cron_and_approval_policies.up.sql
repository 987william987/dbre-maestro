CREATE TABLE IF NOT EXISTS db_metadata_job_runs (
    job_name VARCHAR(64) NOT NULL,
    last_scheduled_at DATETIME(6) NULL,
    last_started_at DATETIME(6) NULL,
    last_finished_at DATETIME(6) NULL,
    last_success_at DATETIME(6) NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'idle' COMMENT 'idle|running|success|failed',
    error_message TEXT NULL,
    updated_at DATETIME(6) NOT NULL,
    PRIMARY KEY (job_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS approval_policies (
    workflow_type VARCHAR(32) NOT NULL,
    reviewer_user_ids TEXT NOT NULL,
    reviewer_auth_groups TEXT NOT NULL,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    PRIMARY KEY (workflow_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
