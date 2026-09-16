-- Anonymous read path for published status pages (AD-023).
--
-- Bootstrap paradox: resolving which tenant an anonymous public-status-page
-- request belongs to means reading status_pages/domains by hostname
-- (StatusPageRepository.GetByHostname, called by router.HostRouter before
-- any transaction has app.tenant_id set). Both tables are FORCE ROW LEVEL
-- SECURITY with the fail-closed tenant_isolation policy from 0024, so under
-- a real non-superuser application role that lookup returns zero rows for a
-- page that exists and is published - every public status page would 404.
--
-- Fix: a second PERMISSIVE policy on each table. PostgreSQL ORs all
-- PERMISSIVE policies for the same command together, and CREATE POLICY
-- defaults to PERMISSIVE (0024's tenant_isolation policies carry no AS
-- clause, so they are PERMISSIVE too) - these are purely additive and
-- change nothing about tenant-scoped access. No BYPASSRLS role and no
-- SECURITY DEFINER resolver: RLS stays the single enforcement path.
--
-- Both policies are gated on app.tenant_id being UNSET, not just on the
-- page being published. Without that clause a tenant A session would also
-- see tenant B's published pages and their domains in any query without an
-- explicit "WHERE tenant_id = ?" - which is exactly the shape of
-- StatusPageRepository.ListPaginated and DomainRepository's list queries,
-- so it would be a real cross-tenant leak in the admin listings
-- (TENANT-01/02). With it, no session that has an active tenant changes
-- behaviour at all.
--
-- FOR SELECT only: this opens reads of already-public data, never writes.

CREATE POLICY public_published_read ON status_pages
    FOR SELECT
    USING (
        NULLIF(current_setting('app.tenant_id', true), '') IS NULL
        AND state = 'published'
    );

-- domains has no state of its own; a domain is publicly readable only while
-- some published status page points at it (status_pages.domain_id is the FK
-- direction). The EXISTS subquery is itself subject to status_pages' RLS -
-- policy expressions run as the invoking role - which resolves via the
-- policy above in exactly the anonymous sessions this policy applies to. A
-- domain whose only status page is draft/pending_tls/tls_failed stays
-- invisible.
CREATE POLICY public_published_read ON domains
    FOR SELECT
    USING (
        NULLIF(current_setting('app.tenant_id', true), '') IS NULL
        AND EXISTS (
            SELECT 1 FROM status_pages sp
            WHERE sp.domain_id = domains.id AND sp.state = 'published'
        )
    );
