ALTER TABLE domains
    DROP COLUMN last_error,
    DROP COLUMN verified_at,
    DROP COLUMN ssl_status,
    DROP COLUMN status,
    DROP COLUMN domain_type;
