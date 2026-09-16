ALTER TABLE incident_updates
    DROP COLUMN is_ai_summary,
    DROP COLUMN author_id;

ALTER TABLE incidents
    DROP COLUMN severity;
