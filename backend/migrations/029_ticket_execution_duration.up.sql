ALTER TABLE ticket_executions
    ADD COLUMN duration_ms BIGINT NULL AFTER completed_at;
