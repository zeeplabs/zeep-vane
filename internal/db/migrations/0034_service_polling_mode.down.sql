ALTER TABLE services DROP CONSTRAINT services_monitor_mode_fields_check;
ALTER TABLE services DROP CONSTRAINT services_poll_type_check;
ALTER TABLE services DROP CONSTRAINT services_monitor_mode_check;
ALTER TABLE services DROP COLUMN poll_interval_seconds;
ALTER TABLE services DROP COLUMN poll_target;
ALTER TABLE services DROP COLUMN poll_type;
ALTER TABLE services DROP COLUMN monitor_mode;
ALTER TABLE services ALTER COLUMN slo_id SET NOT NULL;
