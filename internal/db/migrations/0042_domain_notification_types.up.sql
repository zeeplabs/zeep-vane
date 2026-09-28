-- Domain health monitoring reuses the existing notification pipeline
-- (DHM-02, DHM-06): admit the two new types alongside the original three.
-- Postgres has no ALTER CHECK, so drop the constraint and re-add the
-- extended IN (...) list.
ALTER TABLE notification_preferences
    DROP CONSTRAINT notification_preferences_notification_type_check;
ALTER TABLE notification_preferences
    ADD CONSTRAINT notification_preferences_notification_type_check
    CHECK (notification_type IN ('incident_opened', 'incident_resolved', 'weekly_digest', 'domain_expiring', 'domain_ns_drift'));
