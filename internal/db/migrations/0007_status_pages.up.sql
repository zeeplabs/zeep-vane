CREATE TABLE status_pages (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT NOT NULL,
    subdomain      TEXT NOT NULL,
    domain_id      UUID NOT NULL REFERENCES domains(id),
    state          TEXT NOT NULL DEFAULT 'draft',
    tls_last_error TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE status_page_services (
    status_page_id UUID NOT NULL REFERENCES status_pages(id),
    service_id     UUID NOT NULL REFERENCES services(id),
    PRIMARY KEY (status_page_id, service_id)
);
