ALTER TABLE users
    ADD COLUMN lark_recipient_type VARCHAR(32) NOT NULL DEFAULT 'open_id' AFTER lark_recipient,
    ADD COLUMN lark_union_id VARCHAR(255) NOT NULL DEFAULT '' AFTER lark_recipient_type,
    ADD KEY idx_users_lark_union_id (lark_union_id);
