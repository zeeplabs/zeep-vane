-- email_providers holds one row per connected email provider (SendGrid,
-- Resend), each an instance owner's own account credentials - never Vane's.
-- provider is unique so a reconnect upserts the existing row instead of
-- creating a second one for the same provider (EMAIL-01/EMAIL-03).
CREATE TABLE email_providers (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider          TEXT NOT NULL UNIQUE CHECK (provider IN ('sendgrid', 'resend')),
    encrypted_api_key BYTEA NOT NULL,
    from_email        TEXT NOT NULL,
    from_name         TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'connected' CHECK (status IN ('connected', 'invalid')),
    last_checked_at   TIMESTAMPTZ,
    last_error        TEXT
);

-- email_settings is a singleton row (same shape as company_settings)
-- holding which connected provider, if any, is currently active
-- (EMAIL-04). active_provider references email_providers.provider (a
-- UNIQUE column) so it can never point at a provider that isn't actually
-- connected; ON DELETE SET NULL costs nothing today (disconnect is out of
-- scope for this feature) and keeps a future disconnect feature from
-- leaving a dangling reference.
CREATE TABLE email_settings (
    id              SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    active_provider TEXT REFERENCES email_providers (provider) ON DELETE SET NULL
);

INSERT INTO email_settings (id) VALUES (1) ON CONFLICT DO NOTHING;
