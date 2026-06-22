DROP TABLE IF EXISTS workflow_rules;

DELETE agp FROM auth_group_permissions agp
INNER JOIN auth_groups ag ON ag.id = agp.auth_group_id
WHERE ag.group_key IN ('security', 'data_owner');

DELETE FROM auth_groups WHERE group_key IN ('security', 'data_owner');

ALTER TABLE tickets
    MODIFY COLUMN status VARCHAR(32) NOT NULL DEFAULT 'pending_review'
        COMMENT 'pending_review|approved|rejected|pending_execution|executing|completed|failed|stopped|interrupted';
