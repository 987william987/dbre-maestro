CREATE TABLE IF NOT EXISTS auth_groups (
    id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    group_key    VARCHAR(64)     NOT NULL,
    name         VARCHAR(128)    NOT NULL,
    description  VARCHAR(255)    NOT NULL DEFAULT '',
    is_system    TINYINT(1)      NOT NULL DEFAULT 0,
    is_protected TINYINT(1)      NOT NULL DEFAULT 0,
    created_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uq_auth_groups_key (group_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS permissions (
    id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    permission_key VARCHAR(128)    NOT NULL,
    name           VARCHAR(128)    NOT NULL,
    description    VARCHAR(255)    NOT NULL DEFAULT '',
    category       VARCHAR(64)     NOT NULL DEFAULT '',
    created_at     DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uq_permissions_key (permission_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS auth_group_permissions (
    auth_group_id BIGINT UNSIGNED NOT NULL,
    permission_id BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (auth_group_id, permission_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS user_permissions (
    user_id       BIGINT UNSIGNED NOT NULL,
    permission_id BIGINT UNSIGNED NOT NULL,
    granted_by    BIGINT UNSIGNED NULL,
    created_at    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, permission_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS user_auth_groups (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id       BIGINT UNSIGNED NOT NULL,
    auth_group_id BIGINT UNSIGNED NOT NULL,
    granted_by    BIGINT UNSIGNED NULL,
    expires_at    DATETIME NULL,
    created_at    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uq_user_auth_group (user_id, auth_group_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS user_db_connections (
    user_id          BIGINT UNSIGNED NOT NULL,
    db_connection_id BIGINT UNSIGNED NOT NULL,
    granted_by       BIGINT UNSIGNED NULL,
    created_at       DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, db_connection_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS auth_group_db_connections (
    auth_group_id    BIGINT UNSIGNED NOT NULL,
    db_connection_id BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (auth_group_id, db_connection_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT IGNORE INTO auth_groups (group_key, name, description, is_system, is_protected)
VALUES
    ('developer', 'Developer', 'Can submit tickets and use SQL editor within granted database scope.', 1, 0),
    ('reviewer', 'Reviewer', 'Can review change requests and sensitive data workflows.', 1, 0),
    ('dba', 'DBA', 'Can manage database assets, governance rules, and execute database changes.', 1, 0),
    ('admin', 'Admin', 'Full platform administrator.', 1, 1);

INSERT IGNORE INTO permissions (permission_key, name, description, category)
VALUES
    ('users.read', 'Read Users', 'View the Users RBAC page.', 'users'),
    ('users.write', 'Write Users', 'Manage users, auth groups, permissions, and database bindings.', 'users'),
    ('audit_logs.read', 'Read Audit Logs', 'View audit log records.', 'audit_logs'),
    ('audit_logs.write', 'Write Audit Logs', 'Export audit log reports.', 'audit_logs'),
    ('db_connections.read', 'Read DB Connections', 'View database connection assets.', 'db_connections'),
    ('db_connections.write', 'Write DB Connections', 'Create and update database connection assets.', 'db_connections'),
    ('masking_rules.read', 'Read Masking Rules', 'View masking rules.', 'masking_rules'),
    ('masking_rules.write', 'Write Masking Rules', 'Manage masking rules.', 'masking_rules'),
    ('sql_review.read', 'Read SQL Review', 'View SQL review rules.', 'sql_review'),
    ('sql_review.write', 'Write SQL Review', 'Manage SQL review rules.', 'sql_review'),
    ('tickets.apply', 'Apply Tickets', 'Submit DDL and DML tickets.', 'tickets'),
    ('tickets.review', 'Review Tickets', 'Review DDL and DML tickets.', 'tickets'),
    ('tickets.execute', 'Execute Tickets', 'Execute approved DDL and DML tickets.', 'tickets'),
    ('sql_editor.query', 'Query SQL Editor', 'Run SQL queries in the SQL editor.', 'sql_editor'),
    ('sql_editor.export', 'Export SQL Editor', 'Export SQL editor query results.', 'sql_editor'),
    ('sql_editor.sensitive_apply', 'Apply Sensitive SQL Editor', 'Request temporary sensitive data access in SQL editor.', 'sql_editor'),
    ('sql_editor.sensitive_review', 'Review Sensitive SQL Editor', 'Review sensitive data access requests in SQL editor.', 'sql_editor'),
    ('sql_editor.sensitive_execute', 'Execute Sensitive SQL Editor', 'Execute approved sensitive data access requests in SQL editor.', 'sql_editor'),
    ('global.sensitive', 'Global Sensitive Override', 'Bypass masking rules and permanently view sensitive data.', 'global');

INSERT IGNORE INTO auth_group_permissions (auth_group_id, permission_id)
SELECT ag.id, p.id
FROM auth_groups ag
JOIN permissions p ON (
    (ag.group_key = 'developer' AND p.permission_key IN ('tickets.apply', 'sql_editor.query', 'sql_editor.export'))
    OR (ag.group_key = 'reviewer' AND p.permission_key IN ('tickets.review', 'sql_editor.sensitive_review'))
    OR (ag.group_key = 'dba' AND p.permission_key IN (
        'db_connections.read',
        'db_connections.write',
        'masking_rules.read',
        'masking_rules.write',
        'sql_review.read',
        'sql_review.write',
        'tickets.execute',
        'sql_editor.query',
        'sql_editor.export',
        'sql_editor.sensitive_execute',
        'audit_logs.read'
    ))
    OR (ag.group_key = 'admin')
);

INSERT IGNORE INTO user_auth_groups (user_id, auth_group_id, granted_by, expires_at, created_at)
SELECT agm.user_id, ag.id, agm.granted_by, agm.expires_at, agm.created_at
FROM auth_group_memberships agm
INNER JOIN auth_groups ag ON ag.group_key = agm.auth_group;
