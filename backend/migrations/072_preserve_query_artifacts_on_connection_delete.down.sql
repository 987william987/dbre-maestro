DELETE qh
FROM query_history AS qh
LEFT JOIN db_connections AS dc ON dc.id = qh.db_connection_id
WHERE dc.id IS NULL;

DELETE sq
FROM saved_queries AS sq
LEFT JOIN db_connections AS dc ON dc.id = sq.db_connection_id
WHERE dc.id IS NULL;

ALTER TABLE query_history
    ADD CONSTRAINT fk_query_history_connection FOREIGN KEY (db_connection_id) REFERENCES db_connections(id) ON DELETE CASCADE;

ALTER TABLE saved_queries
    ADD CONSTRAINT fk_saved_queries_connection FOREIGN KEY (db_connection_id) REFERENCES db_connections(id) ON DELETE CASCADE;
