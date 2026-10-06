ALTER TABLE status_pages
    ADD COLUMN environment TEXT NOT NULL DEFAULT ''
    CHECK (environment IN ('', 'staging', 'development'));
