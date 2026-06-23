SET @column_exists := (
    SELECT COUNT(*)
    FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'db_object_snapshots'
      AND COLUMN_NAME = 'row_count'
);

SET @ddl := IF(
    @column_exists > 0,
    'ALTER TABLE db_object_snapshots DROP COLUMN row_count',
    'SELECT 1'
);

PREPARE stmt FROM @ddl;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
