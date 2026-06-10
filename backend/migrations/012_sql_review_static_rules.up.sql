-- Add static SQL review rules (string-heuristic based, no parser required).
INSERT IGNORE INTO sql_review_rules (rule_name, enabled, threshold, description) VALUES
('dml_no_where',    1, NULL, 'UPDATE/DELETE 必須有 WHERE 條件'),
('ddl_no_comment',  1, NULL, 'CREATE TABLE 必須有表備注 (COMMENT)'),
('require_utf8mb4', 1, NULL, 'CREATE/ALTER TABLE 必須使用 utf8mb4 字符集');
