CREATE TABLE company_settings (
    id            SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    name          TEXT NOT NULL DEFAULT '',
    contact_email TEXT NOT NULL DEFAULT '',
    logo_url      TEXT
);

INSERT INTO company_settings (id) VALUES (1) ON CONFLICT DO NOTHING;
