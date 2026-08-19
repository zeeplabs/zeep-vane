CREATE TABLE admin_invites (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT NOT NULL,
    role          TEXT NOT NULL CHECK (role IN ('owner', 'operator', 'viewer')),
    token_hash    TEXT NOT NULL UNIQUE,
    invited_by_id UUID NOT NULL REFERENCES admins(id),
    expires_at    TIMESTAMPTZ NOT NULL,
    used_at       TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
