CREATE TABLE integrations (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider          TEXT NOT NULL UNIQUE,
    encrypted_api_key BYTEA NOT NULL,
    encrypted_app_key BYTEA NOT NULL,
    status            TEXT NOT NULL DEFAULT 'active',
    last_checked_at   TIMESTAMPTZ,
    last_error        TEXT
);
