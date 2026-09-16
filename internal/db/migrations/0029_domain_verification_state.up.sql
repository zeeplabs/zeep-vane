ALTER TABLE domains
    ADD COLUMN domain_type  TEXT NOT NULL DEFAULT 'custom'
        CHECK (domain_type IN ('custom')),
    ADD COLUMN status       TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'verified', 'error')),
    ADD COLUMN ssl_status   TEXT NOT NULL DEFAULT 'pending'
        CHECK (ssl_status IN ('pending', 'active', 'error')),
    ADD COLUMN verified_at  TIMESTAMPTZ NULL,
    ADD COLUMN last_error   TEXT NULL;
