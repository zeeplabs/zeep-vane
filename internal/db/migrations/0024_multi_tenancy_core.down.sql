DROP POLICY tenant_isolation ON services;
ALTER TABLE services DISABLE ROW LEVEL SECURITY;
ALTER TABLE services DROP COLUMN tenant_id;

DROP TABLE tenant_invites;
DROP TABLE tenant_memberships;
DROP TABLE tenants;
