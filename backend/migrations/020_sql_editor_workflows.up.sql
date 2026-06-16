ALTER TABLE tickets
    MODIFY COLUMN ticket_type VARCHAR(32) NOT NULL COMMENT 'ddl|dml|sql_export|sensitive_query_access',
    ADD COLUMN approved_duration_minutes INT NULL AFTER completed_at,
    ADD COLUMN approved_until DATETIME NULL AFTER approved_duration_minutes,
    ADD COLUMN revoked_at DATETIME NULL AFTER approved_until,
    ADD COLUMN revoked_by BIGINT UNSIGNED NULL AFTER revoked_at;

CREATE TABLE IF NOT EXISTS ticket_scopes (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ticket_id BIGINT UNSIGNED NOT NULL,
    connection_id BIGINT UNSIGNED NOT NULL,
    database_name VARCHAR(255) NULL,
    schema_name VARCHAR(255) NULL,
    table_name VARCHAR(255) NULL,
    column_name VARCHAR(255) NOT NULL,
    is_sensitive TINYINT(1) NOT NULL DEFAULT 0,
    source_kind VARCHAR(32) NOT NULL DEFAULT 'query_column',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_ticket_scopes_ticket (ticket_id),
    KEY idx_ticket_scopes_ticket_column (ticket_id, column_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT IGNORE INTO permissions (permission_key, name, description, category)
VALUES
    ('settings.read', 'Read Settings', 'View platform governance settings.', 'settings'),
    ('settings.write', 'Write Settings', 'Manage platform governance settings.', 'settings');

INSERT IGNORE INTO auth_group_permissions (auth_group_id, permission_id)
SELECT ag.id, p.id
FROM auth_groups ag
JOIN permissions p ON (
    (ag.group_key = 'dba' AND p.permission_key IN ('settings.read'))
    OR (ag.group_key = 'admin' AND p.permission_key IN ('settings.read', 'settings.write'))
);
