ALTER TABLE sql_review_rules
    ADD COLUMN category VARCHAR(32) NOT NULL DEFAULT 'statement' AFTER rule_name,
    ADD COLUMN severity VARCHAR(16) NOT NULL DEFAULT 'error' AFTER category;

UPDATE sql_review_rules SET category = 'table' WHERE rule_name = 'ddl_no_comment';
UPDATE sql_review_rules SET category = 'statement' WHERE rule_name IN ('dml_no_where', 'full_table_scan', 'high_row_count');
UPDATE sql_review_rules SET category = 'system' WHERE rule_name = 'require_utf8mb4';

INSERT INTO sql_review_rules (rule_name, category, severity, enabled, threshold, description) VALUES
('require_innodb', 'engine', 'error', 1, NULL, 'CREATE TABLE must use the InnoDB storage engine'),
('require_primary_key', 'table', 'error', 1, NULL, 'CREATE TABLE must include a primary key'),
('prohibit_foreign_key', 'table', 'warning', 1, NULL, 'CREATE TABLE must not define foreign key constraints');
