ALTER TABLE tickets
    ADD COLUMN execution_run_mode VARCHAR(32) NULL AFTER execution_requested_at,
    ADD KEY idx_tickets_execution_run_mode (execution_run_mode);

UPDATE tickets
SET execution_run_mode = 'batch'
WHERE status = 'executing'
  AND (execution_run_mode IS NULL OR execution_run_mode = '');
