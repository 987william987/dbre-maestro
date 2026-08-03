ALTER TABLE query_history
  ADD COLUMN row_count INT NULL AFTER sql_content;
