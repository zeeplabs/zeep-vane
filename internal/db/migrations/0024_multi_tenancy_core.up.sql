-- Multi-tenancy core (AD-022, .specs/features/multi-tenancy-core).
--
-- SPEC_DEVIATION (batch T1-T8, phases 1-3 only): design.md describes a full
-- schema recreation - admins renamed to users (role moved off it entirely
-- onto tenant_memberships), admin_invites renamed to tenant_invites, and
-- tenant_id/RLS added to every domain table (incidents, status_pages,
-- domains, llm_providers/llm_settings, email_providers/email_settings,
-- admin_audit_log) in addition to services. Doing all of that in one
-- migration would immediately break every existing integration test in
-- internal/db and internal/api that authenticates via the admins table or
-- reads/writes those other domain tables (~40 files) - work that belongs to
-- later phases (T13 tenant-scopes admin_invites, T16 merges
-- company_settings, and new follow-up tasks this plan does not yet name for
-- the remaining domain tables). This migration is scoped to what T1-T8
-- actually build and test: the new tenant tables, plus tenant_id/RLS on
-- services (needed for T2's fail-closed/cross-tenant isolation proof, the
-- P1 MVP acceptance test). tenant_memberships.user_id therefore still
-- references admins(id), not a new users(id) - see tenant_membership_repository.go.
-- company_settings, admin_invites, and the remaining domain tables are left
-- untouched. Flagged for the orchestrator/user: the full spec AC1 table
-- list is not yet RLS-protected after this batch.

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

-- tenants' own id IS its tenant identity - the policy checks id itself,
-- not a separate tenant_id column. NULLIF(...,'') turns an unset
-- app.tenant_id (current_setting returns '' with missing_ok=true) into
-- NULL rather than an invalid-uuid-cast error; id = NULL is never true,
-- so an unset session fails closed (zero rows), matching TENANT-03.
ALTER TABLE tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenants FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tenants
    USING (id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- tenant_memberships must be readable by two different session shapes:
-- (a) the owning user, by app.user_id, before any tenant is active yet
-- (login resolving which tenant(s) to offer - chicken-and-egg, since the
-- membership row is what determines the tenant in the first place), and
-- (b) anyone acting inside a tenant context, by app.tenant_id (future
-- tenant-scoped member management, T13). Either match is sufficient; both
-- unset still fails closed (NULL OR NULL = NULL, never true).
CREATE TABLE tenant_memberships (
    user_id    UUID NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('owner', 'operator', 'viewer')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, tenant_id)
);
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

-- tenant_invites: schema groundwork for T13/T14 (Phase 5, out of this
-- batch) - not yet wired into any handler. Mirrors admin_invites'  shape
-- plus tenant_id; admin_invites itself is untouched in this batch (see
-- deviation note above).
CREATE TABLE tenant_invites (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email         TEXT NOT NULL,
    role          TEXT NOT NULL CHECK (role IN ('owner', 'operator', 'viewer')),
    name          TEXT NOT NULL DEFAULT '',
    phone         TEXT,
    token_hash    TEXT NOT NULL UNIQUE,
    invited_by_id UUID NOT NULL REFERENCES admins(id),
    expires_at    TIMESTAMPTZ NOT NULL,
    used_at       TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE tenant_invites ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_invites FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tenant_invites
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- services: tenant_id defaults from the session's app.tenant_id setting
-- (set per-request by the tenant-context middleware, T3) so existing
-- INSERT statements that never mention tenant_id keep working unchanged
-- as long as they run inside a tenant-scoped transaction; outside one,
-- the DEFAULT resolves to NULL and the NOT NULL constraint rejects the
-- insert rather than silently landing on the wrong tenant.
ALTER TABLE services ADD COLUMN tenant_id UUID REFERENCES tenants(id);
ALTER TABLE services ALTER COLUMN tenant_id SET DEFAULT NULLIF(current_setting('app.tenant_id', true), '')::uuid;
ALTER TABLE services ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE services ENABLE ROW LEVEL SECURITY;
ALTER TABLE services FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON services
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
