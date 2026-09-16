CREATE TABLE notification_preferences (
    user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    notification_type TEXT NOT NULL CHECK (notification_type IN ('incident_opened', 'incident_resolved', 'weekly_digest')),
    enabled           BOOLEAN NOT NULL,
    PRIMARY KEY (user_id, notification_type)
);
