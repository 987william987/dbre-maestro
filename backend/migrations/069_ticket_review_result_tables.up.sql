ALTER TABLE ticket_review_results
    ADD COLUMN tables_json JSON NULL AFTER object_type;
