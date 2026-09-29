-- Domain health monitoring (DHM-01/DHM-05): the latest RDAP/NS check result
-- per domain, stored in place on the domains row rather than a dedicated
-- history table (spec.md Out of Scope). Every column is nullable or
-- defaulted, so existing rows need no backfill.
ALTER TABLE domains
    ADD COLUMN expires_at         TIMESTAMPTZ NULL,
    ADD COLUMN registrar          TEXT NULL,
    ADD COLUMN expected_ns        TEXT[] NULL,
    ADD COLUMN current_ns         TEXT[] NULL,
    ADD COLUMN ns_drift_detected  BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN last_rdap_check_at TIMESTAMPTZ NULL,
    ADD COLUMN rdap_last_error    TEXT NULL;
