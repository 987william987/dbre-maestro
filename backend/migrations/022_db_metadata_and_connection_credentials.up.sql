CREATE TABLE IF NOT EXISTS db_connection_credentials (
    id                     BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    db_connection_id       BIGINT UNSIGNED NOT NULL,
    credential_role        VARCHAR(32)     NOT NULL COMMENT 'readonly|readwrite',
    username               VARCHAR(128)    NOT NULL,
    password_encrypted     VARBINARY(512)  NOT NULL COMMENT 'AES-256-GCM encrypted',
    encryption_key_version INT UNSIGNED    NOT NULL DEFAULT 1,
    created_at             DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at             DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uq_db_conn_credential_role (db_connection_id, credential_role),
    KEY idx_db_conn_credentials_conn (db_connection_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS cloud_db_inventory_snapshots (
    id                      BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    snapshot_at             DATETIME        NOT NULL,
    provider                VARCHAR(32)     NOT NULL DEFAULT 'aws',
    engine                  VARCHAR(64)     NOT NULL,
    region                  VARCHAR(64)     NOT NULL,
    az                      VARCHAR(64)     NULL,
    account_id              VARCHAR(64)     NULL,
    db_identifier           VARCHAR(255)    NOT NULL,
    cluster_identifier      VARCHAR(255)    NULL,
    instance_identifier     VARCHAR(255)    NULL,
    role                    VARCHAR(64)     NULL,
    engine_version          VARCHAR(128)    NULL,
    instance_class          VARCHAR(128)    NULL,
    storage_type            VARCHAR(128)    NULL,
    cluster_endpoint        VARCHAR(255)    NULL,
    cluster_reader_endpoint VARCHAR(255)    NULL,
    instance_endpoint       VARCHAR(255)    NULL,
    raw_payload_json        JSON            NULL,
    tags_json               JSON            NULL,
    created_at              DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_inventory_snapshot_time (snapshot_at),
    KEY idx_inventory_engine_region (engine, region),
    KEY idx_inventory_identifier (db_identifier)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS db_object_snapshots (
    id                       BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    snapshot_at              DATETIME        NOT NULL,
    db_connection_id         BIGINT UNSIGNED NOT NULL,
    connection_name_snapshot VARCHAR(128)    NOT NULL,
    engine                   VARCHAR(64)     NOT NULL,
    cluster_name             VARCHAR(255)    NULL,
    node_name                VARCHAR(255)    NULL,
    database_name            VARCHAR(255)    NOT NULL,
    schema_name              VARCHAR(255)    NOT NULL,
    table_name               VARCHAR(255)    NOT NULL,
    row_count                BIGINT          NOT NULL DEFAULT 0,
    data_size_bytes          BIGINT          NOT NULL DEFAULT 0,
    index_size_bytes         BIGINT          NOT NULL DEFAULT 0,
    created_at               DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_object_snapshot_time (snapshot_at),
    KEY idx_object_snapshot_conn (db_connection_id),
    KEY idx_object_snapshot_table (database_name, schema_name, table_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT IGNORE INTO permissions (permission_key, name, description, category)
VALUES
    ('db_metadata.read', 'Read DB Metadata', 'View database metadata inventory and object snapshots.', 'db_metadata');

INSERT IGNORE INTO auth_group_permissions (auth_group_id, permission_id)
SELECT ag.id, p.id
FROM auth_groups ag
JOIN permissions p ON (
    (ag.group_key = 'dba' AND p.permission_key IN ('db_metadata.read'))
    OR (ag.group_key = 'admin')
);
