DROP INDEX idx_masking_whitelist_auth_group_id ON masking_whitelist;

ALTER TABLE masking_whitelist
    DROP COLUMN auth_group_id;
