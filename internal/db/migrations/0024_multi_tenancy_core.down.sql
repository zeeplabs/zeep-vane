-- Reverses 0024, restoring the pre-multi-tenancy single-tenant schema.
-- Identity data is not carried back: users/tenant_memberships hold the
-- role-per-tenant shape that admins.role cannot represent, so the down
-- path recreates the old tables empty rather than inventing a mapping.

DROP POLICY tenant_isolation ON llm_settings;
DROP POLICY tenant_isolation ON llm_providers;
DROP POLICY tenant_isolation ON email_settings;
DROP POLICY tenant_isolation ON email_providers;
DROP POLICY tenant_isolation ON admin_audit_log;
DROP POLICY tenant_isolation ON domains;
DROP POLICY tenant_isolation ON status_pages;
DROP POLICY tenant_isolation ON incidents;
DROP POLICY tenant_isolation ON services;
DROP POLICY tenant_isolation ON tenant_invites;
DROP POLICY tenant_isolation ON tenant_memberships;
DROP POLICY tenant_isolation ON tenants;

ALTER TABLE llm_settings DISABLE ROW LEVEL SECURITY;
ALTER TABLE llm_providers DISABLE ROW LEVEL SECURITY;
ALTER TABLE email_settings DISABLE ROW LEVEL SECURITY;
ALTER TABLE email_providers DISABLE ROW LEVEL SECURITY;
ALTER TABLE admin_audit_log DISABLE ROW LEVEL SECURITY;
ALTER TABLE domains DISABLE ROW LEVEL SECURITY;
ALTER TABLE status_pages DISABLE ROW LEVEL SECURITY;
ALTER TABLE incidents DISABLE ROW LEVEL SECURITY;
ALTER TABLE services DISABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_invites DISABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_memberships DISABLE ROW LEVEL SECURITY;
ALTER TABLE tenants DISABLE ROW LEVEL SECURITY;

-- Settings singletons go back to the id = 1 shape (0016 / 0021).
DROP TABLE llm_settings;
DROP TABLE email_settings;

ALTER TABLE llm_providers DROP CONSTRAINT llm_providers_tenant_id_provider_key;
ALTER TABLE llm_providers DROP COLUMN tenant_id;
DELETE FROM llm_providers a USING llm_providers b
    WHERE a.ctid > b.ctid AND a.provider = b.provider;
ALTER TABLE llm_providers ADD CONSTRAINT llm_providers_provider_key UNIQUE (provider);

ALTER TABLE email_providers DROP CONSTRAINT email_providers_tenant_id_provider_key;
ALTER TABLE email_providers DROP COLUMN tenant_id;
DELETE FROM email_providers a USING email_providers b
    WHERE a.ctid > b.ctid AND a.provider = b.provider;
ALTER TABLE email_providers ADD CONSTRAINT email_providers_provider_key UNIQUE (provider);

CREATE TABLE email_settings (
    id              SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    active_provider TEXT REFERENCES email_providers (provider) ON DELETE SET NULL
);
INSERT INTO email_settings (id) VALUES (1) ON CONFLICT DO NOTHING;

CREATE TABLE llm_settings (
    id              SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    active_provider TEXT REFERENCES llm_providers (provider) ON DELETE SET NULL
);
INSERT INTO llm_settings (id) VALUES (1) ON CONFLICT DO NOTHING;

ALTER TABLE admin_audit_log DROP COLUMN tenant_id;
ALTER TABLE domains DROP COLUMN tenant_id;
ALTER TABLE status_pages DROP COLUMN tenant_id;
ALTER TABLE incidents DROP COLUMN tenant_id;
ALTER TABLE services DROP COLUMN tenant_id;

DROP TABLE tenant_invites;
DROP TABLE tenant_memberships;
DROP TABLE tenants;

CREATE TABLE company_settings (
    id                SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    name              TEXT NOT NULL DEFAULT '',
    contact_email     TEXT NOT NULL DEFAULT '',
    logo_data         BYTEA,
    logo_content_type TEXT
);
INSERT INTO company_settings (id) VALUES (1) ON CONFLICT DO NOTHING;

CREATE TABLE admins (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email               TEXT NOT NULL UNIQUE,
    password_hash       TEXT NOT NULL,
    role                TEXT NOT NULL DEFAULT 'owner'
                            CHECK (role IN ('owner', 'operator', 'viewer')),
    sessions_revoked_at TIMESTAMPTZ,
    name                TEXT NOT NULL DEFAULT '',
    phone               TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE admin_invites (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT NOT NULL,
    role          TEXT NOT NULL CHECK (role IN ('owner', 'operator', 'viewer')),
    token_hash    TEXT NOT NULL UNIQUE,
    invited_by_id UUID NOT NULL REFERENCES admins(id),
    expires_at    TIMESTAMPTZ NOT NULL,
    used_at       TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    name          TEXT NOT NULL DEFAULT '',
    phone         TEXT
);

DELETE FROM password_reset_tokens;
ALTER TABLE password_reset_tokens DROP CONSTRAINT password_reset_tokens_user_id_fkey;
ALTER TABLE password_reset_tokens RENAME COLUMN user_id TO admin_id;
ALTER TABLE password_reset_tokens
    ADD CONSTRAINT password_reset_tokens_admin_id_fkey
    FOREIGN KEY (admin_id) REFERENCES admins(id);

DROP TABLE users;
