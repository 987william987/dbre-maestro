ALTER TABLE db_connections
    DROP COLUMN readwrite_port,
    DROP COLUMN readwrite_host,
    DROP COLUMN readonly_port,
    DROP COLUMN readonly_host;
