DELETE FROM sql_review_rules
WHERE rule_name IN ('prohibit_trigger', 'prohibit_stored_function', 'prohibit_stored_procedure', 'prohibit_event', 'prohibit_reserved_column_name');
