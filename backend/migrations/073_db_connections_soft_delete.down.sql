ALTER TABLE db_connections
    DROP KEY idx_db_connections_deleted_at,
    DROP COLUMN deleted_by,
    DROP COLUMN deleted_at;
