-- Multi-tenancy core (AD-022, .specs/features/multi-tenancy-core).
--
-- Full schema recreation, per design.md's "Migração" section: there is no
-- external self-hosted installation holding real data (AD-022), so this
-- migration drops the single-tenant identity tables outright instead of
-- carrying them alongside the new ones. admins becomes users (a global
-- identity - one email, one account, no role: role moved to
-- tenant_memberships, scoped per tenant), admin_invites becomes
-- tenant_invites, and company_settings - the old "one row per
-- installation" singleton - is absorbed by tenants, which is the same row
-- shape once "installation" becomes "tenant".
--
-- Every domain table listed in spec.md AC1 gets tenant_id + a fail-closed
-- RLS policy: tenant_id defaults from the session's app.tenant_id setting
-- (set per-request by the tenant-context middleware) so existing INSERTs
-- that never mention tenant_id keep working inside a tenant-scoped
-- transaction; outside one the DEFAULT resolves to NULL and NOT NULL
-- rejects the insert rather than silently landing it on the wrong tenant.
-- NULLIF(..., '') turns an unset app.tenant_id (current_setting returns ''
-- with missing_ok=true) into NULL rather than an invalid-uuid-cast error,
-- and `col = NULL` is never true, so an unset session reads zero rows
-- (TENANT-03).

-- ---------------------------------------------------------------------
-- 1. Identity: users replaces admins
-- ---------------------------------------------------------------------

CREATE TABLE users (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email               TEXT NOT NULL UNIQUE,
    password_hash       TEXT NOT NULL,
    name                TEXT NOT NULL DEFAULT '',
    phone               TEXT,
    sessions_revoked_at TIMESTAMPTZ,
    -- NULL until the SaaS signup flow's verification link is followed
    -- (T10). Self-hosted bootstrap creates its owner already verified,
    -- so email verification never gates a self-hosted login.
    email_verified_at   TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- users carries no tenant_id and no RLS: identity is global (one email =
-- one account regardless of how many tenants it belongs to), and login
-- has to resolve a user before any tenant is known.

-- password_reset_tokens.admin_id pointed at the table being dropped.
-- Existing rows are single-use, short-lived credentials for accounts that
-- no longer exist after this migration, so they are discarded rather than
-- remapped.
DELETE FROM password_reset_tokens;
ALTER TABLE password_reset_tokens DROP CONSTRAINT password_reset_tokens_admin_id_fkey;
ALTER TABLE password_reset_tokens RENAME COLUMN admin_id TO user_id;
ALTER TABLE password_reset_tokens
    ADD CONSTRAINT password_reset_tokens_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

DROP TABLE admin_invites;
DROP TABLE admins;
DROP TABLE company_settings;

-- ---------------------------------------------------------------------
-- 2. Tenants
-- ---------------------------------------------------------------------

CREATE TABLE tenants (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name              TEXT NOT NULL,
    slug              TEXT UNIQUE,
    plan              TEXT NOT NULL DEFAULT 'free',
    status            TEXT NOT NULL DEFAULT 'active',
    contact_email     TEXT NOT NULL DEFAULT '',
    logo_data         BYTEA,
    logo_content_type TEXT,
    legal_name        TEXT,
    tax_id            TEXT,
    tax_id_type       TEXT CHECK (tax_id_type IN ('cpf', 'cnpj')),
    billing_address   JSONB,
    locale            TEXT NOT NULL DEFAULT 'pt-BR',
    primary_color     TEXT,
    secondary_color   TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Backfill safety net: this migration assumes fresh/empty domain tables
-- (AD-022 - no real self-hosted customer data exists to migrate), but a
-- test database replaying this migration (down then up - golang-migrate's
-- Steps(-1) in the *_migration_test.go files, or a disposable container
-- reused across a whole `go test` run) can carry leftover rows from
-- earlier, unrelated tests. Assign any such orphaned row to a dedicated
-- placeholder tenant rather than leaving tenant_id NULL, so the SET NOT
-- NULL statements below never fail on rows this migration has no real
-- tenant identity for in the first place. Inserted before tenants gets
-- its RLS policy, since the policy keys off app.tenant_id, which is not
-- set while migrations run.
INSERT INTO tenants (id, name)
VALUES ('00000000-0000-0000-0000-000000000000', 'unassigned-legacy-data')
ON CONFLICT (id) DO NOTHING;

CREATE TABLE tenant_memberships (
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('owner', 'operator', 'viewer')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, tenant_id)
);

-- tenant_invites replaces admin_invites entirely: same token-hash + TTL
-- mechanism (0010), plus the tenant the invite grants membership of.
CREATE TABLE tenant_invites (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL DEFAULT NULLIF(current_setting('app.tenant_id', true), '')::uuid
                       REFERENCES tenants(id) ON DELETE CASCADE,
    email         TEXT NOT NULL,
    role          TEXT NOT NULL CHECK (role IN ('owner', 'operator', 'viewer')),
    name          TEXT NOT NULL DEFAULT '',
    phone         TEXT,
    token_hash    TEXT NOT NULL UNIQUE,
    invited_by_id UUID NOT NULL REFERENCES users(id),
    expires_at    TIMESTAMPTZ NOT NULL,
    used_at       TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------
-- 3. tenant_id + RLS on every domain table (spec.md AC1)
-- ---------------------------------------------------------------------

ALTER TABLE services ADD COLUMN tenant_id UUID REFERENCES tenants(id);
UPDATE services SET tenant_id = '00000000-0000-0000-0000-000000000000' WHERE tenant_id IS NULL;
ALTER TABLE services ALTER COLUMN tenant_id SET DEFAULT NULLIF(current_setting('app.tenant_id', true), '')::uuid;
ALTER TABLE services ALTER COLUMN tenant_id SET NOT NULL;

ALTER TABLE incidents ADD COLUMN tenant_id UUID REFERENCES tenants(id);
UPDATE incidents SET tenant_id = '00000000-0000-0000-0000-000000000000' WHERE tenant_id IS NULL;
ALTER TABLE incidents ALTER COLUMN tenant_id SET DEFAULT NULLIF(current_setting('app.tenant_id', true), '')::uuid;
ALTER TABLE incidents ALTER COLUMN tenant_id SET NOT NULL;

ALTER TABLE status_pages ADD COLUMN tenant_id UUID REFERENCES tenants(id);
UPDATE status_pages SET tenant_id = '00000000-0000-0000-0000-000000000000' WHERE tenant_id IS NULL;
ALTER TABLE status_pages ALTER COLUMN tenant_id SET DEFAULT NULLIF(current_setting('app.tenant_id', true), '')::uuid;
ALTER TABLE status_pages ALTER COLUMN tenant_id SET NOT NULL;

ALTER TABLE domains ADD COLUMN tenant_id UUID REFERENCES tenants(id);
UPDATE domains SET tenant_id = '00000000-0000-0000-0000-000000000000' WHERE tenant_id IS NULL;
ALTER TABLE domains ALTER COLUMN tenant_id SET DEFAULT NULLIF(current_setting('app.tenant_id', true), '')::uuid;
ALTER TABLE domains ALTER COLUMN tenant_id SET NOT NULL;

ALTER TABLE admin_audit_log ADD COLUMN tenant_id UUID REFERENCES tenants(id);
UPDATE admin_audit_log SET tenant_id = '00000000-0000-0000-0000-000000000000' WHERE tenant_id IS NULL;
ALTER TABLE admin_audit_log ALTER COLUMN tenant_id SET DEFAULT NULLIF(current_setting('app.tenant_id', true), '')::uuid;
ALTER TABLE admin_audit_log ALTER COLUMN tenant_id SET NOT NULL;

-- email_providers/llm_providers: the provider name was globally unique
-- ("one row per connected provider per installation"); it becomes unique
-- per tenant. email_settings/llm_settings were id = 1 singletons - the
-- same "one row per installation" shape - so their primary key becomes
-- the tenant itself. They are recreated rather than altered in place
-- because the seeded id = 1 row has no tenant to belong to.
ALTER TABLE email_settings DROP CONSTRAINT email_settings_active_provider_fkey;
ALTER TABLE llm_settings DROP CONSTRAINT llm_settings_active_provider_fkey;

ALTER TABLE email_providers ADD COLUMN tenant_id UUID REFERENCES tenants(id);
UPDATE email_providers SET tenant_id = '00000000-0000-0000-0000-000000000000' WHERE tenant_id IS NULL;
ALTER TABLE email_providers ALTER COLUMN tenant_id SET DEFAULT NULLIF(current_setting('app.tenant_id', true), '')::uuid;
ALTER TABLE email_providers ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE email_providers DROP CONSTRAINT email_providers_provider_key;
ALTER TABLE email_providers ADD CONSTRAINT email_providers_tenant_id_provider_key UNIQUE (tenant_id, provider);

ALTER TABLE llm_providers ADD COLUMN tenant_id UUID REFERENCES tenants(id);
UPDATE llm_providers SET tenant_id = '00000000-0000-0000-0000-000000000000' WHERE tenant_id IS NULL;
ALTER TABLE llm_providers ALTER COLUMN tenant_id SET DEFAULT NULLIF(current_setting('app.tenant_id', true), '')::uuid;
ALTER TABLE llm_providers ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE llm_providers DROP CONSTRAINT llm_providers_provider_key;
ALTER TABLE llm_providers ADD CONSTRAINT llm_providers_tenant_id_provider_key UNIQUE (tenant_id, provider);

DROP TABLE email_settings;
CREATE TABLE email_settings (
    tenant_id       UUID PRIMARY KEY DEFAULT NULLIF(current_setting('app.tenant_id', true), '')::uuid
                         REFERENCES tenants(id) ON DELETE CASCADE,
    active_provider TEXT,
    -- Composite FK so the active provider can only ever name a provider
    -- connected by this same tenant. ON DELETE SET NULL names the column
    -- explicitly (PostgreSQL 15+) so disconnecting a provider clears
    -- active_provider without touching tenant_id, which is the key.
    FOREIGN KEY (tenant_id, active_provider)
        REFERENCES email_providers (tenant_id, provider) ON DELETE SET NULL (active_provider)
);

DROP TABLE llm_settings;
CREATE TABLE llm_settings (
    tenant_id       UUID PRIMARY KEY DEFAULT NULLIF(current_setting('app.tenant_id', true), '')::uuid
                         REFERENCES tenants(id) ON DELETE CASCADE,
    active_provider TEXT,
    FOREIGN KEY (tenant_id, active_provider)
        REFERENCES llm_providers (tenant_id, provider) ON DELETE SET NULL (active_provider)
);

-- ---------------------------------------------------------------------
-- 4. RLS policies
-- ---------------------------------------------------------------------

-- tenants' own id IS its tenant identity - the policy checks id itself,
-- not a separate tenant_id column.
ALTER TABLE tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenants FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tenants
    USING (id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- tenant_memberships must be readable by two different session shapes:
-- (a) the owning user, by app.user_id, before any tenant is active yet
-- (login resolving which tenant(s) to offer - chicken-and-egg, since the
-- membership row is what determines the tenant in the first place), and
-- (b) anyone acting inside a tenant context, by app.tenant_id. Either
-- match is sufficient; both unset still fails closed (NULL OR NULL =
-- NULL, never true).
ALTER TABLE tenant_memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_memberships FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tenant_memberships
    USING (
        user_id = NULLIF(current_setting('app.user_id', true), '')::uuid
        OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    )
    WITH CHECK (
        user_id = NULLIF(current_setting('app.user_id', true), '')::uuid
        OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    );

ALTER TABLE tenant_invites ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_invites FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tenant_invites
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE services ENABLE ROW LEVEL SECURITY;
ALTER TABLE services FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON services
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE incidents ENABLE ROW LEVEL SECURITY;
ALTER TABLE incidents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON incidents
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE status_pages ENABLE ROW LEVEL SECURITY;
ALTER TABLE status_pages FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON status_pages
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE domains ENABLE ROW LEVEL SECURITY;
ALTER TABLE domains FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON domains
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE admin_audit_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE admin_audit_log FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON admin_audit_log
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE email_providers ENABLE ROW LEVEL SECURITY;
ALTER TABLE email_providers FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON email_providers
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE email_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE email_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON email_settings
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE llm_providers ENABLE ROW LEVEL SECURITY;
ALTER TABLE llm_providers FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON llm_providers
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE llm_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE llm_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON llm_settings
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
