-- Allow the app user to create temporary shadow databases for MySQL DDL validation.
-- This runs under MIGRATION_DSN (root/admin) during startup and deployment.
GRANT CREATE, ALTER, DROP ON `shadow\_%`.* TO 'maestro_app'@'%';
