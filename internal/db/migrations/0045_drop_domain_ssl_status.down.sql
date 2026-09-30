ALTER TABLE domains
    ADD COLUMN ssl_status TEXT NOT NULL DEFAULT 'pending'
        CHECK (ssl_status IN ('pending', 'active', 'error'));
