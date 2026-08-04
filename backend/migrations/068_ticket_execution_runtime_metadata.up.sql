ALTER TABLE ticket_executions
  ADD COLUMN sent_to_db_at DATETIME(3) NULL AFTER duration_ms,
  ADD COLUMN db_process_type VARCHAR(32) NULL AFTER sent_to_db_at,
  ADD COLUMN db_process_id BIGINT UNSIGNED NULL AFTER db_process_type,
  ADD COLUMN interruption_reason VARCHAR(64) NULL AFTER db_process_id,
  ADD COLUMN outcome_confidence VARCHAR(32) NULL AFTER interruption_reason;
