-- T6: db_connections with encryption_key_version
CREATE TABLE IF NOT EXISTS db_connections (
    id                    BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name                  VARCHAR(128)    NOT NULL,
    db_type               VARCHAR(16)     NOT NULL COMMENT 'mysql|postgresql|redis',
    host                  VARCHAR(255)    NOT NULL,
    port                  SMALLINT UNSIGNED NOT NULL,
    database_name         VARCHAR(128),
    username              VARCHAR(128)    NOT NULL,
    password_encrypted    VARBINARY(512)  NOT NULL COMMENT 'AES-256-GCM encrypted',
    encryption_key_version INT UNSIGNED   NOT NULL DEFAULT 1,
    ssl_mode              VARCHAR(16)     NOT NULL DEFAULT 'prefer',
    extra_params          JSON,
    created_by            BIGINT UNSIGNED NOT NULL,
    created_at            DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at            DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uq_conn_name (name),
    KEY idx_conn_type (db_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
