ALTER TABLE tickets
    MODIFY COLUMN ticket_type VARCHAR(32) NOT NULL COMMENT 'ddl|dml|redis_command|sql_export|sensitive_query_access';

ALTER TABLE ticket_review_results
    ADD COLUMN phase VARCHAR(32) NOT NULL DEFAULT 'validation' AFTER sql_stmt,
    ADD COLUMN validation_stage VARCHAR(32) NULL AFTER phase,
    ADD COLUMN statement_kind VARCHAR(32) NULL AFTER validation_stage,
    ADD COLUMN object_type VARCHAR(32) NULL AFTER statement_kind,
    ADD COLUMN validation_method VARCHAR(32) NULL AFTER object_type;

UPDATE ticket_review_results
SET phase = 'validation'
WHERE phase = '';
