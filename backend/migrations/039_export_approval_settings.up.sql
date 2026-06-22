ALTER TABLE tickets
    ADD COLUMN contains_sensitive TINYINT(1) NULL AFTER ticket_type,
    ADD KEY idx_tickets_export_sensitivity (ticket_type, contains_sensitive);

UPDATE tickets t
SET contains_sensitive = CASE
    WHEN EXISTS (
        SELECT 1
        FROM ticket_scopes ts
        WHERE ts.ticket_id = t.id
          AND ts.is_sensitive = 1
    ) THEN 1
    ELSE 0
END
WHERE t.ticket_type = 'sql_export'
  AND t.contains_sensitive IS NULL;
