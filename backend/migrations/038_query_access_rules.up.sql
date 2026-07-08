CREATE TABLE IF NOT EXISTS query_access_ticket_rules (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ticket_id BIGINT UNSIGNED NOT NULL,
    effect VARCHAR(16) NOT NULL DEFAULT 'allow' COMMENT 'allow|deny',
    connection_id BIGINT UNSIGNED NOT NULL,
    database_pattern VARCHAR(255) NOT NULL DEFAULT '*',
    table_pattern VARCHAR(255) NOT NULL DEFAULT '*',
    created_at DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    KEY idx_query_access_ticket_rules_ticket (ticket_id),
    KEY idx_query_access_ticket_rules_connection (connection_id),
    KEY idx_query_access_ticket_rules_effect (effect)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS query_access_rules (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    subject_type VARCHAR(16) NOT NULL COMMENT 'user|auth_group',
    subject_id BIGINT UNSIGNED NOT NULL,
    effect VARCHAR(16) NOT NULL DEFAULT 'allow' COMMENT 'allow|deny',
    connection_id BIGINT UNSIGNED NOT NULL,
    database_pattern VARCHAR(255) NOT NULL DEFAULT '*',
    table_pattern VARCHAR(255) NOT NULL DEFAULT '*',
    granted_via VARCHAR(32) NOT NULL DEFAULT 'ticket',
    source_ticket_id BIGINT UNSIGNED NULL,
    expires_at DATETIME(6) NULL,
    revoked_at DATETIME(6) NULL,
    revoked_by BIGINT UNSIGNED NULL,
    created_by BIGINT UNSIGNED NULL,
    updated_by BIGINT UNSIGNED NULL,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    KEY idx_query_access_rules_subject (subject_type, subject_id),
    KEY idx_query_access_rules_connection (connection_id),
    KEY idx_query_access_rules_ticket (source_ticket_id),
    KEY idx_query_access_rules_expiry (expires_at),
    KEY idx_query_access_rules_active (revoked_at),
    KEY idx_query_access_rules_effect (effect)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT INTO query_access_ticket_rules
    (ticket_id, effect, connection_id, database_pattern, table_pattern, created_at)
SELECT
    ticket_id,
    'allow',
    connection_id,
    database_name,
    COALESCE(NULLIF(table_name, ''), '*'),
    created_at
FROM query_access_ticket_items old_items
WHERE NOT EXISTS (
    SELECT 1
    FROM query_access_ticket_rules rules
    WHERE rules.ticket_id = old_items.ticket_id
);

INSERT INTO query_access_rules
    (subject_type, subject_id, effect, connection_id, database_pattern, table_pattern, granted_via, source_ticket_id, expires_at, revoked_at, revoked_by, created_by, updated_by, created_at, updated_at)
SELECT
    subject_type,
    subject_id,
    'allow',
    connection_id,
    COALESCE(NULLIF(database_name, ''), '*'),
    COALESCE(NULLIF(table_name, ''), '*'),
    granted_via,
    source_ticket_id,
    expires_at,
    revoked_at,
    revoked_by,
    created_by,
    created_by,
    created_at,
    updated_at
FROM query_access_grants old_grants
WHERE NOT EXISTS (
    SELECT 1
    FROM query_access_rules rules
    WHERE rules.subject_type = old_grants.subject_type
      AND rules.subject_id = old_grants.subject_id
      AND rules.connection_id = old_grants.connection_id
      AND rules.database_pattern = COALESCE(NULLIF(old_grants.database_name, ''), '*')
      AND rules.table_pattern = COALESCE(NULLIF(old_grants.table_name, ''), '*')
      AND (
        (rules.source_ticket_id IS NULL AND old_grants.source_ticket_id IS NULL)
        OR rules.source_ticket_id = old_grants.source_ticket_id
      )
);
