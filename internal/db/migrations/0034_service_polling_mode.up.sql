ALTER TABLE services ALTER COLUMN slo_id DROP NOT NULL;
ALTER TABLE services ADD COLUMN monitor_mode TEXT NOT NULL DEFAULT 'slo';
ALTER TABLE services ADD COLUMN poll_type TEXT;
ALTER TABLE services ADD COLUMN poll_target TEXT;
ALTER TABLE services ADD COLUMN poll_interval_seconds INT;
ALTER TABLE services ADD CONSTRAINT services_monitor_mode_check
  CHECK (monitor_mode IN ('slo', 'polling'));
ALTER TABLE services ADD CONSTRAINT services_poll_type_check
  CHECK (poll_type IS NULL OR poll_type IN ('http', 'tcp', 'ping'));
ALTER TABLE services ADD CONSTRAINT services_monitor_mode_fields_check
  CHECK (
    (monitor_mode = 'slo' AND slo_id IS NOT NULL
      AND poll_type IS NULL AND poll_target IS NULL AND poll_interval_seconds IS NULL)
    OR
    (monitor_mode = 'polling' AND slo_id IS NULL
      AND poll_type IS NOT NULL AND poll_target IS NOT NULL AND poll_interval_seconds IS NOT NULL)
  );
