-- E1: append-only audit log
CREATE TABLE IF NOT EXISTS audit_logs (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    actor_id    BIGINT UNSIGNED COMMENT 'NULL = system',
    actor_name  VARCHAR(64),
    action_type VARCHAR(64)  NOT NULL
                COMMENT 'login|logout|ticket_submit|ticket_approve|ticket_reject|ticket_execute|ticket_stop|query_execute|export_create|export_download|setting_change|notification_failure',
    resource_type VARCHAR(32) COMMENT 'ticket|user|db_connection|export',
    resource_id   BIGINT UNSIGNED,
    details     JSON,
    ip_address  VARCHAR(45),
    created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_audit_actor (actor_id),
    KEY idx_audit_action (action_type),
    KEY idx_audit_created (created_at),
    KEY idx_audit_resource (resource_type, resource_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
