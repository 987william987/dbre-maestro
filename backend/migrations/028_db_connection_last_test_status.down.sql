ALTER TABLE db_connections
    DROP COLUMN last_tested_at,
    DROP COLUMN last_test_error,
    DROP COLUMN last_test_status;
