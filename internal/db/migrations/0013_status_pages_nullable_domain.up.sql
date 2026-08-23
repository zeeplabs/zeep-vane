ALTER TABLE status_pages ALTER COLUMN domain_id DROP NOT NULL;
ALTER TABLE status_pages ALTER COLUMN subdomain DROP NOT NULL;

CREATE UNIQUE INDEX status_pages_domain_subdomain_idx
    ON status_pages (domain_id, subdomain) WHERE domain_id IS NOT NULL;
