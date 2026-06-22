DELETE agp
FROM auth_group_permissions agp
INNER JOIN auth_groups ag ON ag.id = agp.auth_group_id
WHERE ag.group_key = 'reviewer';

DELETE agdc
FROM auth_group_db_connections agdc
INNER JOIN auth_groups ag ON ag.id = agdc.auth_group_id
WHERE ag.group_key = 'reviewer';

DELETE uag
FROM user_auth_groups uag
INNER JOIN auth_groups ag ON ag.id = uag.auth_group_id
WHERE ag.group_key = 'reviewer';

DELETE FROM auth_group_memberships
WHERE auth_group = 'reviewer';

DELETE FROM auth_groups
WHERE group_key = 'reviewer';
