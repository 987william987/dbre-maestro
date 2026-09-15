DELETE FROM sql_review_rules
WHERE rule_name IN ('require_innodb', 'require_primary_key', 'prohibit_foreign_key', 'prohibit_select_star');

ALTER TABLE sql_review_rules
    DROP COLUMN severity,
    DROP COLUMN category;
