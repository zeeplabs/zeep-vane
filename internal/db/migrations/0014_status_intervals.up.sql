CREATE TABLE status_intervals (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id             UUID NOT NULL REFERENCES services(id),
    status                 TEXT NOT NULL,
    error_budget_remaining DOUBLE PRECISION NOT NULL,
    starts_at              TIMESTAMPTZ NOT NULL,
    last_seen_at           TIMESTAMPTZ NOT NULL,
    ends_at                TIMESTAMPTZ
);

-- At most one open interval per service - the DB-enforced invariant that
-- makes the write path's race-loser case a constraint violation instead of
-- a silent duplicate.
CREATE UNIQUE INDEX idx_status_intervals_one_open_per_service
    ON status_intervals (service_id) WHERE ends_at IS NULL;

-- Overlap queries (ListOverlapping) and pruning (DeleteClosedBefore) both
-- filter/order by service_id + a timestamp column.
CREATE INDEX idx_status_intervals_service_id_starts_at ON status_intervals (service_id, starts_at);
CREATE INDEX idx_status_intervals_ends_at ON status_intervals (ends_at) WHERE ends_at IS NOT NULL;

DROP TABLE status_snapshots;
