CREATE TABLE incidents (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title        TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'investigating',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at  TIMESTAMPTZ
);

CREATE TABLE incident_services (
    incident_id UUID NOT NULL REFERENCES incidents(id),
    service_id  UUID NOT NULL REFERENCES services(id),
    PRIMARY KEY (incident_id, service_id)
);

CREATE TABLE incident_updates (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id UUID NOT NULL REFERENCES incidents(id),
    body        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_incident_updates_incident_id_created_at ON incident_updates (incident_id, created_at);
