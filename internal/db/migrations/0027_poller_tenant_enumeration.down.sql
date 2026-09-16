-- Reverses 0027, leaving 0024's tenant_isolation and 0025's
-- public_published_read policies untouched.

DROP POLICY system_iteration_read ON tenants;
