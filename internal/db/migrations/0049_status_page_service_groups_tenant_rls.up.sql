-- status_page_service_groups (migration 0046) was never given a tenant_id
-- column or RLS policy, unlike every other tenant-scoped table (AD-022).
-- UpdateGroup/DeleteGroup filtered only by "WHERE id = $1", so an
-- authenticated operator of one tenant could PATCH/DELETE another tenant's
-- group by guessing or reading its id off that tenant's public status page
-- (group ids are exposed in the public status page response). Same fix,
-- same shape as 0040's integrations fix, except tenant_id is backfilled by
-- joining to the owning status_pages row rather than a fixed placeholder -
-- a service group's tenant is always its parent status page's tenant.
ALTER TABLE status_page_service_groups ADD COLUMN tenant_id UUID REFERENCES tenants(id);
UPDATE status_page_service_groups g
    SET tenant_id = sp.tenant_id
    FROM status_pages sp
    WHERE sp.id = g.status_page_id;
ALTER TABLE status_page_service_groups ALTER COLUMN tenant_id SET DEFAULT NULLIF(current_setting('app.tenant_id', true), '')::uuid;
ALTER TABLE status_page_service_groups ALTER COLUMN tenant_id SET NOT NULL;

ALTER TABLE status_page_service_groups ENABLE ROW LEVEL SECURITY;
ALTER TABLE status_page_service_groups FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON status_page_service_groups
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
