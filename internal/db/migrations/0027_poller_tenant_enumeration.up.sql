-- System-scoped read path for the poller's tenant enumeration (AD-024).
--
-- Bootstrap paradox, same shape as AD-023 but a different mechanism. The
-- poller has to enumerate every active tenant before it can iterate them
-- one at a time (T15, TENANT-04), but tenants is FORCE ROW LEVEL SECURITY
-- with the fail-closed tenant_isolation policy from 0024, whose USING
-- clause is "id = app.tenant_id" - reading the table requires already
-- knowing the very tenant the read is trying to discover. Under a real
-- non-superuser application role, TenantRepository.List returns zero rows
-- and a tenant-iterating poller polls nothing at all.
--
-- AD-023's condition cannot be reused here: tenants has no "published"
-- state, and gating on "app.tenant_id is unset" alone would expose the
-- full tenant list to the anonymous public-status-page path, which runs
-- in exactly that session shape. The poller is trusted internal server
-- code - in-process, ticker-driven, never reachable from external HTTP
-- input - so the correct condition is "this session is the system
-- itself", not a property of the row.
--
-- app.is_system is written in exactly one place in the codebase:
-- db.SystemTenantLister.List (internal/db/system_tenant_lister.go), with
-- a fixed literal, on its own short-lived transaction that runs only the
-- enumeration query and then rolls back. No HTTP handler, middleware,
-- cookie, header, or request parameter can reach it -
-- internal/db/system_flag_scope_test.go fails the build if that stops
-- being true. Per-tenant work afterwards runs on the ordinary
-- app.tenant_id-scoped transaction (Pool.BeginTenantTx) like every other
-- request.
--
-- PERMISSIVE (CREATE POLICY's default, same as 0024's tenant_isolation
-- and 0025's public_published_read) so PostgreSQL ORs it with the
-- existing policies: purely additive, nothing about tenant-scoped access
-- changes. FOR SELECT only: this opens a read, never a write. No
-- BYPASSRLS role and no SECURITY DEFINER function - RLS stays the single
-- enforcement path.

CREATE POLICY system_iteration_read ON tenants
    FOR SELECT
    USING (current_setting('app.is_system', true) = 'true');
