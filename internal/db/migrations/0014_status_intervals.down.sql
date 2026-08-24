DROP TABLE status_intervals;

CREATE TABLE status_snapshots (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id             UUID NOT NULL REFERENCES services(id),
    status                 TEXT NOT NULL,
    error_budget_remaining DOUBLE PRECISION NOT NULL,
    fetched_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_status_snapshots_service_id_fetched_at ON status_snapshots (service_id, fetched_at);
