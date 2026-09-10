-- Reverses 0025, leaving 0024's tenant_isolation policies untouched.

DROP POLICY public_published_read ON domains;
DROP POLICY public_published_read ON status_pages;
