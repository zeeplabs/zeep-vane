CREATE TABLE sessions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_agent   TEXT,
    ip           TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);

CREATE INDEX sessions_user_id_created_at_idx
    ON sessions (user_id, created_at DESC);

CREATE INDEX sessions_user_id_active_idx
    ON sessions (user_id)
    WHERE revoked_at IS NULL;
