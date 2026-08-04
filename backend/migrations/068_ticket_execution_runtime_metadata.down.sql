ALTER TABLE ticket_executions
  DROP COLUMN outcome_confidence,
  DROP COLUMN interruption_reason,
  DROP COLUMN db_process_id,
  DROP COLUMN db_process_type,
  DROP COLUMN sent_to_db_at;
