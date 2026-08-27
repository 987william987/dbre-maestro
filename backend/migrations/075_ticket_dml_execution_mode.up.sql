ALTER TABLE tickets
  ADD COLUMN dml_execution_mode VARCHAR(32) NULL AFTER execution_run_mode;
