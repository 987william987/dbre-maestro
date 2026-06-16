DROP TABLE IF EXISTS ticket_review_results;

ALTER TABLE tickets
    DROP COLUMN database_name;
