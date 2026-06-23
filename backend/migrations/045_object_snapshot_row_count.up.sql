SET @column_exists := (
    SELECT COUNT(*)
    FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'db_object_snapshots'
      AND COLUMN_NAME = 'row_count'
);

SET @ddl := IF(
    @column_exists = 0,
    'ALTER TABLE db_object_snapshots ADD COLUMN row_count BIGINT NOT NULL DEFAULT 0 AFTER table_name',
    'SELECT 1'
);

PREPARE stmt FROM @ddl;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
