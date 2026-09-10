ALTER TABLE incidents
    ADD COLUMN severity TEXT NOT NULL DEFAULT 'moderate'
        CHECK (severity IN ('minor', 'moderate', 'critical'));

ALTER TABLE incident_updates
    ADD COLUMN author_id UUID NULL REFERENCES users(id),
    ADD COLUMN is_ai_summary BOOLEAN NOT NULL DEFAULT false;
