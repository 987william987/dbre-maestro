ALTER TABLE users
    DROP KEY idx_users_lark_union_id,
    DROP COLUMN lark_union_id,
    DROP COLUMN lark_recipient_type;
