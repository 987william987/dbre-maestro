ALTER TABLE tickets
  ADD COLUMN approved_at DATETIME(6) NULL AFTER review_comment,
  ADD COLUMN review_rejected_at DATETIME(6) NULL AFTER approved_at,
  ADD COLUMN pending_execution_at DATETIME(6) NULL AFTER review_rejected_at,
  ADD COLUMN execution_requested_at DATETIME(6) NULL AFTER pending_execution_at,
  ADD COLUMN execution_rejected_at DATETIME(6) NULL AFTER execution_requested_at,
  ADD COLUMN withdrawn_at DATETIME(6) NULL AFTER execution_rejected_at;
