CREATE TABLE status_page_service_groups (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    status_page_id UUID NOT NULL REFERENCES status_pages(id),
    name           TEXT NOT NULL,
    position       INT NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE status_page_services
    ADD COLUMN group_id UUID REFERENCES status_page_service_groups(id) ON DELETE SET NULL,
    ADD COLUMN parent_service_id UUID REFERENCES services(id) ON DELETE SET NULL,
    ADD COLUMN position INT NOT NULL DEFAULT 0;
