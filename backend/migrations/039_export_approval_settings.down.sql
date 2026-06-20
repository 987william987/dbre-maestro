ALTER TABLE tickets
    DROP KEY idx_tickets_export_sensitivity,
    DROP COLUMN contains_sensitive;
