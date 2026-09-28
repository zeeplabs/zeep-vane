-- Drop the two domain-health preference rows before narrowing the CHECK back
-- to the original three - otherwise the re-added constraint would reject the
-- existing rows.
DELETE FROM notification_preferences WHERE notification_type IN ('domain_expiring', 'domain_ns_drift');
ALTER TABLE notification_preferences
    DROP CONSTRAINT notification_preferences_notification_type_check;
ALTER TABLE notification_preferences
    ADD CONSTRAINT notification_preferences_notification_type_check
    CHECK (notification_type IN ('incident_opened', 'incident_resolved', 'weekly_digest'));
