ALTER TABLE services
    ADD COLUMN environment TEXT NOT NULL DEFAULT ''
    CHECK (environment IN ('', 'development', 'staging', 'production'));
