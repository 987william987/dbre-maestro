CREATE TABLE IF NOT EXISTS db_database_snapshots (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    snapshot_at DATETIME(6) NOT NULL,
    db_connection_id BIGINT UNSIGNED NOT NULL,
    engine VARCHAR(32) NOT NULL,
    database_name VARCHAR(255) NOT NULL,
    character_set_name VARCHAR(128) NULL,
    collation_name VARCHAR(255) NULL,
    table_count BIGINT NOT NULL DEFAULT 0,
    data_size_bytes BIGINT NOT NULL DEFAULT 0,
    index_size_bytes BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    UNIQUE KEY uq_db_database_snapshot_connection_name (db_connection_id, database_name),
    KEY idx_db_database_snapshot_time (snapshot_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS db_account_snapshots (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    snapshot_at DATETIME(6) NOT NULL,
    db_connection_id BIGINT UNSIGNED NOT NULL,
    engine VARCHAR(32) NOT NULL,
    principal_key VARCHAR(512) NOT NULL,
    principal_name VARCHAR(255) NOT NULL,
    principal_host VARCHAR(255) NULL,
    principal_type VARCHAR(32) NOT NULL COMMENT 'user|role',
    can_login TINYINT(1) NOT NULL DEFAULT 0,
    is_superuser TINYINT(1) NOT NULL DEFAULT 0,
    is_locked TINYINT(1) NOT NULL DEFAULT 0,
    valid_until DATETIME(6) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_db_account_snapshot_principal (db_connection_id, principal_key),
    KEY idx_db_account_snapshot_time (snapshot_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS db_account_grant_snapshots (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    snapshot_at DATETIME(6) NOT NULL,
    db_connection_id BIGINT UNSIGNED NOT NULL,
    principal_key VARCHAR(512) NOT NULL,
    grant_kind VARCHAR(32) NOT NULL COMMENT 'privilege|role_membership',
    granted_role VARCHAR(255) NULL,
    scope_type VARCHAR(32) NULL COMMENT 'global|database|schema|table|column|routine|sequence',
    database_name VARCHAR(255) NULL,
    schema_name VARCHAR(255) NULL,
    object_name VARCHAR(255) NULL,
    privilege_type VARCHAR(128) NULL,
    is_grantable TINYINT(1) NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    KEY idx_db_account_grant_connection_principal (db_connection_id, principal_key),
    KEY idx_db_account_grant_time (snapshot_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS db_account_snapshot_statuses (
    db_connection_id BIGINT UNSIGNED NOT NULL,
    last_attempt_at DATETIME(6) NOT NULL,
    last_success_at DATETIME(6) NULL,
    status VARCHAR(32) NOT NULL,
    error_message TEXT NULL,
    PRIMARY KEY (db_connection_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO permissions (permission_key, name, description, category)
VALUES
    ('db_connections.overview', 'DB Connection Overview', 'View database connection details and test status.', 'db_connections'),
    ('db_connections.databases', 'DB Connection Databases', 'View database snapshots for a database connection.', 'db_connections'),
    ('db_connections.accounts', 'DB Connection Accounts', 'View database-native accounts, roles, and grants.', 'db_connections');

INSERT IGNORE INTO auth_group_permissions (auth_group_id, permission_id)
SELECT DISTINCT existing.auth_group_id, added.id
FROM auth_group_permissions existing
INNER JOIN permissions old_permission ON old_permission.id = existing.permission_id
INNER JOIN permissions added ON added.permission_key IN ('db_connections.overview', 'db_connections.databases')
WHERE old_permission.permission_key IN ('db_connections.read', 'db_connections.write');

INSERT IGNORE INTO user_permissions (user_id, permission_id, granted_by, created_at)
SELECT DISTINCT existing.user_id, added.id, existing.granted_by, UTC_TIMESTAMP(6)
FROM user_permissions existing
INNER JOIN permissions old_permission ON old_permission.id = existing.permission_id
INNER JOIN permissions added ON added.permission_key IN ('db_connections.overview', 'db_connections.databases')
WHERE old_permission.permission_key IN ('db_connections.read', 'db_connections.write');

INSERT IGNORE INTO auth_group_permissions (auth_group_id, permission_id)
SELECT ag.id, p.id
FROM auth_groups ag
INNER JOIN permissions p ON p.permission_key = 'db_connections.accounts'
WHERE ag.group_key IN ('admin', 'dba');

INSERT IGNORE INTO user_permissions (user_id, permission_id, granted_by, created_at)
SELECT u.id, p.id, NULL, UTC_TIMESTAMP(6)
FROM users u
INNER JOIN permissions p ON p.permission_key = 'db_connections.accounts'
WHERE u.is_protected = 1;
