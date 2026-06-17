ALTER TABLE db_connections
    ADD COLUMN readonly_host VARCHAR(255) NOT NULL DEFAULT '' AFTER port,
    ADD COLUMN readonly_port INT UNSIGNED NOT NULL DEFAULT 0 AFTER readonly_host,
    ADD COLUMN readwrite_host VARCHAR(255) NOT NULL DEFAULT '' AFTER readonly_port,
    ADD COLUMN readwrite_port INT UNSIGNED NOT NULL DEFAULT 0 AFTER readwrite_host;

UPDATE db_connections
SET
    readonly_host = host,
    readonly_port = port,
    readwrite_host = host,
    readwrite_port = port
WHERE readonly_host = ''
   OR readonly_port = 0
   OR readwrite_host = ''
   OR readwrite_port = 0;
