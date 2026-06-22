CREATE TEMPORARY TABLE reviewer_removal_guard (
    blocker VARCHAR(128) NOT NULL
);

INSERT INTO reviewer_removal_guard (blocker)
SELECT NULL
FROM auth_group_memberships
WHERE auth_group = 'reviewer'
LIMIT 1;

INSERT INTO reviewer_removal_guard (blocker)
SELECT NULL
FROM user_auth_groups uag
INNER JOIN auth_groups ag ON ag.id = uag.auth_group_id
WHERE ag.group_key = 'reviewer'
LIMIT 1;

INSERT INTO reviewer_removal_guard (blocker)
SELECT NULL
FROM auth_group_db_connections agdc
INNER JOIN auth_groups ag ON ag.id = agdc.auth_group_id
WHERE ag.group_key = 'reviewer'
LIMIT 1;

INSERT INTO reviewer_removal_guard (blocker)
SELECT NULL
FROM workflow_rules
WHERE JSON_CONTAINS(approval_auth_groups, JSON_QUOTE('reviewer'))
   OR JSON_CONTAINS(executor_auth_groups, JSON_QUOTE('reviewer'))
LIMIT 1;

INSERT INTO reviewer_removal_guard (blocker)
SELECT NULL
FROM approval_policies
WHERE JSON_VALID(reviewer_auth_groups)
  AND JSON_CONTAINS(reviewer_auth_groups, JSON_QUOTE('reviewer'))
LIMIT 1;

DROP TEMPORARY TABLE reviewer_removal_guard;
