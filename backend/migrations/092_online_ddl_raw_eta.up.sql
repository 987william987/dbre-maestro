ALTER TABLE ticket_online_ddl_runs
    ADD COLUMN eta_display VARCHAR(32) NULL AFTER eta_seconds;
