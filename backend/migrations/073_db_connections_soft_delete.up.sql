ALTER TABLE db_connections
    ADD COLUMN deleted_at DATETIME(6) NULL AFTER updated_at,
    ADD COLUMN deleted_by BIGINT UNSIGNED NULL AFTER deleted_at,
    ADD KEY idx_db_connections_deleted_at (deleted_at);
