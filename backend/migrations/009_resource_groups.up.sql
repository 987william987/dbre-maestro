CREATE TABLE IF NOT EXISTS resource_groups (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name        VARCHAR(64)     NOT NULL,
    description VARCHAR(255)    NOT NULL DEFAULT '',
    created_by  BIGINT UNSIGNED NOT NULL,
    created_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uq_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Which DB connections belong to a resource group
CREATE TABLE IF NOT EXISTS resource_group_connections (
    resource_group_id BIGINT UNSIGNED NOT NULL,
    db_connection_id  BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (resource_group_id, db_connection_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Individual user assignments (with optional expiry)
CREATE TABLE IF NOT EXISTS resource_group_users (
    resource_group_id BIGINT UNSIGNED NOT NULL,
    user_id           BIGINT UNSIGNED NOT NULL,
    expires_at        DATETIME        NULL,
    granted_by        BIGINT UNSIGNED NOT NULL,
    granted_at        DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (resource_group_id, user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Auth group assignments (all members of an auth group get access)
CREATE TABLE IF NOT EXISTS resource_group_auth_groups (
    resource_group_id BIGINT UNSIGNED NOT NULL,
    auth_group        VARCHAR(32)     NOT NULL,
    PRIMARY KEY (resource_group_id, auth_group)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
