-- llm_providers holds one row per connected LLM provider (OpenAI, ...),
-- each an instance owner's own account credentials - never Vane's.
-- provider is unique so a reconnect upserts the existing row instead of
-- creating a second one for the same provider (mirrors email_providers,
-- 0016).
CREATE TABLE llm_providers (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider          TEXT NOT NULL UNIQUE CHECK (provider IN ('openai')),
    encrypted_api_key BYTEA NOT NULL,
    model             TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'connected' CHECK (status IN ('connected', 'invalid')),
    last_checked_at   TIMESTAMPTZ,
    last_error        TEXT
);

-- llm_settings is a singleton row (same shape as email_settings) holding
-- which connected provider, if any, is currently active.
CREATE TABLE llm_settings (
    id              SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    active_provider TEXT REFERENCES llm_providers (provider) ON DELETE SET NULL
);

INSERT INTO llm_settings (id) VALUES (1) ON CONFLICT DO NOTHING;
