CREATE TABLE services (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                TEXT NOT NULL,
    slo_id              TEXT NOT NULL,
    current_status      TEXT NOT NULL DEFAULT 'not_configured',
    last_status_change_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
