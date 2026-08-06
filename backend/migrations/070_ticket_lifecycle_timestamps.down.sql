ALTER TABLE tickets
  DROP COLUMN withdrawn_at,
  DROP COLUMN execution_rejected_at,
  DROP COLUMN execution_requested_at,
  DROP COLUMN pending_execution_at,
  DROP COLUMN review_rejected_at,
  DROP COLUMN approved_at;
