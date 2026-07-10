DROP TABLE IF EXISTS lark_login_states;

ALTER TABLE users
    DROP KEY idx_users_lark_login_union_id,
    DROP KEY idx_users_lark_login_open_id,
    DROP KEY idx_users_external_identity,
    DROP COLUMN lark_binding_status,
    DROP COLUMN lark_bound_at,
    DROP COLUMN lark_avatar_url,
    DROP COLUMN lark_display_name,
    DROP COLUMN lark_login_union_id,
    DROP COLUMN lark_login_open_id,
    DROP COLUMN password_login_disabled,
    DROP COLUMN external_identity_id,
    DROP COLUMN external_identity_source;
