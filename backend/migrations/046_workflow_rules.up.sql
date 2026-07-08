ALTER TABLE tickets
    MODIFY COLUMN status VARCHAR(32) NOT NULL DEFAULT 'pending_review'
        COMMENT 'pending_review|approved|rejected|withdrawn|pending_execution|executing|completed|failed|stopped|interrupted|needs_admin_attention';

INSERT IGNORE INTO auth_groups (group_key, name, description, is_system, is_protected, created_at, updated_at)
VALUES
    ('security', 'Security', 'Reviews sensitive data workflows and data export requests.', 1, 0, UTC_TIMESTAMP(6), UTC_TIMESTAMP(6)),
    ('data_owner', 'Data Owner', 'Reviews regular database change and access tickets for owned data scope.', 1, 0, UTC_TIMESTAMP(6), UTC_TIMESTAMP(6));

INSERT IGNORE INTO auth_group_permissions (auth_group_id, permission_id, created_at)
SELECT ag.id, p.id, UTC_TIMESTAMP(6)
FROM auth_groups ag
JOIN permissions p ON (
    (ag.group_key = 'data_owner' AND p.permission_key = 'tickets.review')
    OR (ag.group_key = 'security' AND p.permission_key IN ('sql_editor.export_review', 'sql_editor.sensitive_review'))
);

CREATE TABLE IF NOT EXISTS workflow_rules (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    rule_name VARCHAR(128) NOT NULL,
    ticket_type VARCHAR(32) NOT NULL,
    db_connection_id BIGINT UNSIGNED NULL COMMENT 'NULL means All connections',
    export_sensitivity VARCHAR(16) NULL COMMENT 'normal|sensitive, only for sql_export',
    approval_enabled TINYINT(1) NOT NULL DEFAULT 1,
    approval_auth_groups JSON NOT NULL,
    executor_auth_groups JSON NOT NULL,
    priority INT NOT NULL DEFAULT 100,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    KEY idx_workflow_rules_match (ticket_type, db_connection_id, export_sensitivity, enabled, priority)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT INTO workflow_rules
    (rule_name, ticket_type, db_connection_id, export_sensitivity, approval_enabled, approval_auth_groups, executor_auth_groups, priority, enabled, created_at, updated_at)
SELECT 'Global DDL', 'ddl', NULL, NULL, 1, JSON_ARRAY('data_owner'), JSON_ARRAY('dba'), 100, 1, UTC_TIMESTAMP(6), UTC_TIMESTAMP(6)
WHERE NOT EXISTS (SELECT 1 FROM workflow_rules WHERE ticket_type = 'ddl' AND db_connection_id IS NULL AND export_sensitivity IS NULL);

INSERT INTO workflow_rules
    (rule_name, ticket_type, db_connection_id, export_sensitivity, approval_enabled, approval_auth_groups, executor_auth_groups, priority, enabled, created_at, updated_at)
SELECT 'Global DML', 'dml', NULL, NULL, 1, JSON_ARRAY('data_owner'), JSON_ARRAY('dba'), 100, 1, UTC_TIMESTAMP(6), UTC_TIMESTAMP(6)
WHERE NOT EXISTS (SELECT 1 FROM workflow_rules WHERE ticket_type = 'dml' AND db_connection_id IS NULL AND export_sensitivity IS NULL);

INSERT INTO workflow_rules
    (rule_name, ticket_type, db_connection_id, export_sensitivity, approval_enabled, approval_auth_groups, executor_auth_groups, priority, enabled, created_at, updated_at)
SELECT 'Global Redis Command', 'redis_command', NULL, NULL, 1, JSON_ARRAY('data_owner'), JSON_ARRAY('dba'), 100, 1, UTC_TIMESTAMP(6), UTC_TIMESTAMP(6)
WHERE NOT EXISTS (SELECT 1 FROM workflow_rules WHERE ticket_type = 'redis_command' AND db_connection_id IS NULL AND export_sensitivity IS NULL);

INSERT INTO workflow_rules
    (rule_name, ticket_type, db_connection_id, export_sensitivity, approval_enabled, approval_auth_groups, executor_auth_groups, priority, enabled, created_at, updated_at)
SELECT 'Global Query Access', 'query_access', NULL, NULL, 1, JSON_ARRAY('data_owner'), JSON_ARRAY(), 100, 1, UTC_TIMESTAMP(6), UTC_TIMESTAMP(6)
WHERE NOT EXISTS (SELECT 1 FROM workflow_rules WHERE ticket_type = 'query_access' AND db_connection_id IS NULL AND export_sensitivity IS NULL);

INSERT INTO workflow_rules
    (rule_name, ticket_type, db_connection_id, export_sensitivity, approval_enabled, approval_auth_groups, executor_auth_groups, priority, enabled, created_at, updated_at)
SELECT 'Global Normal SQL Export', 'sql_export', NULL, 'normal',
       CASE COALESCE((SELECT JSON_UNQUOTE(value) FROM platform_settings WHERE key_name = 'require_non_sensitive_export_review'), 'true')
           WHEN 'false' THEN 0
           ELSE 1
       END,
       JSON_ARRAY('security'), JSON_ARRAY(), 100, 1, UTC_TIMESTAMP(6), UTC_TIMESTAMP(6)
WHERE NOT EXISTS (SELECT 1 FROM workflow_rules WHERE ticket_type = 'sql_export' AND db_connection_id IS NULL AND export_sensitivity = 'normal');

INSERT INTO workflow_rules
    (rule_name, ticket_type, db_connection_id, export_sensitivity, approval_enabled, approval_auth_groups, executor_auth_groups, priority, enabled, created_at, updated_at)
SELECT 'Global Sensitive SQL Export', 'sql_export', NULL, 'sensitive', 1, JSON_ARRAY('security'), JSON_ARRAY(), 100, 1, UTC_TIMESTAMP(6), UTC_TIMESTAMP(6)
WHERE NOT EXISTS (SELECT 1 FROM workflow_rules WHERE ticket_type = 'sql_export' AND db_connection_id IS NULL AND export_sensitivity = 'sensitive');

INSERT INTO workflow_rules
    (rule_name, ticket_type, db_connection_id, export_sensitivity, approval_enabled, approval_auth_groups, executor_auth_groups, priority, enabled, created_at, updated_at)
SELECT 'Global Sensitive Query Access', 'sensitive_query_access', NULL, NULL, 1, JSON_ARRAY('security'), JSON_ARRAY(), 100, 1, UTC_TIMESTAMP(6), UTC_TIMESTAMP(6)
WHERE NOT EXISTS (SELECT 1 FROM workflow_rules WHERE ticket_type = 'sensitive_query_access' AND db_connection_id IS NULL AND export_sensitivity IS NULL);
