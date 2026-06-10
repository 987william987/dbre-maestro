ALTER TABLE masking_whitelist
    ADD COLUMN auth_group_id BIGINT UNSIGNED NULL AFTER user_id;

UPDATE masking_whitelist mw
INNER JOIN auth_groups ag ON ag.group_key = mw.auth_group
SET mw.auth_group_id = ag.id
WHERE mw.auth_group IS NOT NULL
  AND mw.auth_group_id IS NULL;

CREATE INDEX idx_masking_whitelist_auth_group_id ON masking_whitelist(auth_group_id);
