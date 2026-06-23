SET @column_exists := (
    SELECT COUNT(*)
    FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'cloud_db_inventory_snapshots'
      AND COLUMN_NAME = 'tags_json'
);

SET @ddl := IF(
    @column_exists > 0,
    'ALTER TABLE cloud_db_inventory_snapshots DROP COLUMN tags_json',
    'SELECT 1'
);

PREPARE stmt FROM @ddl;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
