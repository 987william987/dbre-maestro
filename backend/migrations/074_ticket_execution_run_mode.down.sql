ALTER TABLE tickets
    DROP KEY idx_tickets_execution_run_mode,
    DROP COLUMN execution_run_mode;
