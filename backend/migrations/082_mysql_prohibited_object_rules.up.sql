INSERT INTO sql_review_rules (rule_name, category, severity, enabled, threshold, description) VALUES
('prohibit_trigger', 'system', 'error', 1, NULL, 'MySQL triggers are prohibited'),
('prohibit_stored_function', 'system', 'error', 1, NULL, 'MySQL stored functions are prohibited'),
('prohibit_stored_procedure', 'system', 'error', 1, NULL, 'MySQL stored procedures are prohibited'),
('prohibit_event', 'system', 'error', 1, NULL, 'MySQL events are prohibited'),
('prohibit_reserved_column_name', 'naming', 'error', 1, NULL, 'Column names must not use MySQL reserved words');
