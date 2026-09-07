ALTER TABLE query_history
    ADD INDEX idx_query_history_created_at (created_at);
