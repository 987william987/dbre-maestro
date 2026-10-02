CREATE TABLE IF NOT EXISTS lark_ticket_cards (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ticket_id BIGINT UNSIGNED NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    card_stage VARCHAR(16) NOT NULL COMMENT 'review|execution',
    message_id VARCHAR(128) NOT NULL,
    last_ticket_status VARCHAR(32) NOT NULL DEFAULT '',
    update_status VARCHAR(32) NOT NULL DEFAULT 'active' COMMENT 'active|synced|update_failed',
    update_attempts INT NOT NULL DEFAULT 0,
    last_error TEXT NULL,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_lark_ticket_cards_instance (ticket_id, user_id, card_stage, message_id),
    KEY idx_lark_ticket_cards_ticket_stage (ticket_id, card_stage),
    KEY idx_lark_ticket_cards_message (message_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
