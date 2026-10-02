SET @column_exists := (
    SELECT COUNT(*)
    FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'mysql_binlog_export_jobs'
      AND COLUMN_NAME = 'artifact_expires_at'
);

SET @ddl := IF(
    @column_exists = 0,
    'ALTER TABLE mysql_binlog_export_jobs ADD COLUMN artifact_expires_at DATETIME(6) NULL AFTER total_duration_ms',
    'SELECT 1'
);

PREPARE stmt FROM @ddl;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
