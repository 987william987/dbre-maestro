ALTER TABLE db_connections
    ADD COLUMN last_test_status VARCHAR(16) NULL AFTER extra_params,
    ADD COLUMN last_test_error TEXT NULL AFTER last_test_status,
    ADD COLUMN last_tested_at DATETIME NULL AFTER last_test_error;
