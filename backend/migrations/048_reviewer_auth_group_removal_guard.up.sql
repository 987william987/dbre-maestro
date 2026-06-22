INSERT IGNORE INTO user_auth_groups (user_id, auth_group_id, granted_by, expires_at, created_at)
SELECT uag.user_id, target.id, uag.granted_by, uag.expires_at, uag.created_at
FROM user_auth_groups uag
INNER JOIN auth_groups reviewer ON reviewer.id = uag.auth_group_id AND reviewer.group_key = 'reviewer'
INNER JOIN auth_groups target ON target.group_key IN ('data_owner', 'security');

INSERT IGNORE INTO auth_group_memberships (user_id, auth_group, granted_by, expires_at, created_at)
SELECT user_id, 'data_owner', granted_by, expires_at, created_at
FROM auth_group_memberships
WHERE auth_group = 'reviewer';

INSERT IGNORE INTO auth_group_memberships (user_id, auth_group, granted_by, expires_at, created_at)
SELECT user_id, 'security', granted_by, expires_at, created_at
FROM auth_group_memberships
WHERE auth_group = 'reviewer';

INSERT IGNORE INTO auth_group_db_connections (auth_group_id, db_connection_id, created_at)
SELECT target.id, agdc.db_connection_id, COALESCE(agdc.created_at, UTC_TIMESTAMP(6))
FROM auth_group_db_connections agdc
INNER JOIN auth_groups reviewer ON reviewer.id = agdc.auth_group_id AND reviewer.group_key = 'reviewer'
INNER JOIN auth_groups target ON target.group_key = 'data_owner';

UPDATE workflow_rules
SET approval_auth_groups = JSON_ARRAY('security'), updated_at = UTC_TIMESTAMP(6)
WHERE ticket_type IN ('sql_export', 'sensitive_query_access')
  AND JSON_CONTAINS(approval_auth_groups, JSON_QUOTE('reviewer'));

UPDATE workflow_rules
SET approval_auth_groups = JSON_ARRAY('data_owner'), updated_at = UTC_TIMESTAMP(6)
WHERE ticket_type IN ('ddl', 'dml', 'redis_command', 'query_access')
  AND JSON_CONTAINS(approval_auth_groups, JSON_QUOTE('reviewer'));

UPDATE workflow_rules
SET executor_auth_groups = JSON_ARRAY('dba'), updated_at = UTC_TIMESTAMP(6)
WHERE ticket_type IN ('ddl', 'dml', 'redis_command')
  AND JSON_CONTAINS(executor_auth_groups, JSON_QUOTE('reviewer'));

UPDATE workflow_rules
SET executor_auth_groups = JSON_ARRAY(), updated_at = UTC_TIMESTAMP(6)
WHERE ticket_type NOT IN ('ddl', 'dml', 'redis_command')
  AND JSON_CONTAINS(executor_auth_groups, JSON_QUOTE('reviewer'));

UPDATE approval_policies
SET reviewer_auth_groups = JSON_ARRAY('security'), updated_at = UTC_TIMESTAMP(6)
WHERE workflow_type IN ('sql_export_normal', 'sql_export_sensitive', 'sensitive_query_access')
  AND JSON_VALID(reviewer_auth_groups)
  AND JSON_CONTAINS(reviewer_auth_groups, JSON_QUOTE('reviewer'));

UPDATE approval_policies
SET reviewer_auth_groups = JSON_ARRAY('data_owner'), updated_at = UTC_TIMESTAMP(6)
WHERE workflow_type IN ('ddl', 'dml', 'redis_command', 'query_access')
  AND JSON_VALID(reviewer_auth_groups)
  AND JSON_CONTAINS(reviewer_auth_groups, JSON_QUOTE('reviewer'));
