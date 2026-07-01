UPDATE sql_review_rules
SET description = 'CREATE/ALTER TABLE 必須使用 utf8mb4 字符集'
WHERE rule_name = 'require_utf8mb4';
