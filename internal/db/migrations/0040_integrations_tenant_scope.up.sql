-- integrations was never touched by 0024_multi_tenancy_core: it stayed a
-- single global row per provider ("datadog"), so a second tenant connecting
-- Datadog silently overwrites the first tenant's credentials (same
-- ON CONFLICT (provider) upsert races email_providers/llm_providers had
-- before 0024), and every tenant's poller reads/writes the same row
-- regardless of which tenant is active. Same fix, same shape, one
-- migration later.
ALTER TABLE integrations ADD COLUMN tenant_id UUID REFERENCES tenants(id);
UPDATE integrations SET tenant_id = '00000000-0000-0000-0000-000000000000' WHERE tenant_id IS NULL;
ALTER TABLE integrations ALTER COLUMN tenant_id SET DEFAULT NULLIF(current_setting('app.tenant_id', true), '')::uuid;
ALTER TABLE integrations ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE integrations DROP CONSTRAINT integrations_provider_key;
ALTER TABLE integrations ADD CONSTRAINT integrations_tenant_id_provider_key UNIQUE (tenant_id, provider);

ALTER TABLE integrations ENABLE ROW LEVEL SECURITY;
ALTER TABLE integrations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON integrations
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
