DROP TABLE IF EXISTS query_access_grants;
DROP TABLE IF EXISTS query_access_ticket_items;

ALTER TABLE tickets
    MODIFY COLUMN ticket_type VARCHAR(32) NOT NULL COMMENT 'ddl|dml|redis_command|sql_export|sensitive_query_access';
