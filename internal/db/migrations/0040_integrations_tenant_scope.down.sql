DROP POLICY tenant_isolation ON integrations;
ALTER TABLE integrations DISABLE ROW LEVEL SECURITY;

ALTER TABLE integrations DROP CONSTRAINT integrations_tenant_id_provider_key;
ALTER TABLE integrations DROP COLUMN tenant_id;
DELETE FROM integrations a USING integrations b
    WHERE a.ctid > b.ctid AND a.provider = b.provider;
ALTER TABLE integrations ADD CONSTRAINT integrations_provider_key UNIQUE (provider);
