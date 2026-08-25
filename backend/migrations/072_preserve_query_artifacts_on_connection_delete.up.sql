ALTER TABLE query_history
    DROP FOREIGN KEY fk_query_history_connection;

ALTER TABLE saved_queries
    DROP FOREIGN KEY fk_saved_queries_connection;
