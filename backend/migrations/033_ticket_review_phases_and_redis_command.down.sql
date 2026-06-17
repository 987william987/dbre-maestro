ALTER TABLE ticket_review_results
    DROP COLUMN validation_method,
    DROP COLUMN object_type,
    DROP COLUMN statement_kind,
    DROP COLUMN validation_stage,
    DROP COLUMN phase;

ALTER TABLE tickets
    MODIFY COLUMN ticket_type VARCHAR(32) NOT NULL COMMENT 'ddl|dml|sql_export|sensitive_query_access';
