ALTER TABLE workflow_rules
    ADD COLUMN execution_mode VARCHAR(32) NOT NULL DEFAULT 'manual'
        COMMENT 'manual|auto_after_approval'
        AFTER priority;

ALTER TABLE ticket_workflow_snapshots
    ADD COLUMN execution_mode VARCHAR(32) NOT NULL DEFAULT 'manual'
        COMMENT 'manual|auto_after_approval'
        AFTER approval_enabled;
