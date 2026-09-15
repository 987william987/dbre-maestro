INSERT INTO sql_review_rules (rule_name, category, severity, enabled, threshold, description)
VALUES ('prohibit_view', 'system', 'error', 1, NULL, 'Creating MySQL views is prohibited');
