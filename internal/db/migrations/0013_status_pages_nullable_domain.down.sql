DROP INDEX status_pages_domain_subdomain_idx;

ALTER TABLE status_pages ALTER COLUMN subdomain SET NOT NULL;
ALTER TABLE status_pages ALTER COLUMN domain_id SET NOT NULL;
