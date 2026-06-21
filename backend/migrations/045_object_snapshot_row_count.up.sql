ALTER TABLE db_object_snapshots
    ADD COLUMN row_count BIGINT NOT NULL DEFAULT 0 AFTER table_name;
