ALTER TABLE incidents
    ADD COLUMN severity TEXT NOT NULL DEFAULT 'moderate'
        CHECK (severity IN ('minor', 'moderate', 'critical'));

-- ON DELETE SET NULL: an update's author being deleted must not destroy the
-- incident's audit trail - the entry survives, it just loses attribution
-- (same outcome as an AI-authored entry).
ALTER TABLE incident_updates
    ADD COLUMN author_id UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN is_ai_summary BOOLEAN NOT NULL DEFAULT false;
