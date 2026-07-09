ALTER TABLE ticket_workflow_snapshots
    DROP COLUMN execution_mode;

ALTER TABLE workflow_rules
    DROP COLUMN execution_mode;
