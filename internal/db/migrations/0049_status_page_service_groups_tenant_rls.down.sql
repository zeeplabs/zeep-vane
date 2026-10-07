DROP POLICY tenant_isolation ON status_page_service_groups;
ALTER TABLE status_page_service_groups DISABLE ROW LEVEL SECURITY;
ALTER TABLE status_page_service_groups DROP COLUMN tenant_id;
