-- Domain apex TXT verification (DATV-08): a domains row is always a root
-- domain that never serves vane traffic and never gets its own TLS
-- certificate (only the attached subdomain.hostname does, tracked entirely
-- by the unrelated StatusPage state machine), so a per-domain ssl_status
-- has no correct value to hold. Dropped after every Go read path stopped
-- selecting it (expand/contract - 0044 added the token first).
ALTER TABLE domains
    DROP COLUMN ssl_status;
