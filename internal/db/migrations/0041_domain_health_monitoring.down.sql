ALTER TABLE domains
    DROP COLUMN rdap_last_error,
    DROP COLUMN last_rdap_check_at,
    DROP COLUMN ns_drift_detected,
    DROP COLUMN current_ns,
    DROP COLUMN expected_ns,
    DROP COLUMN registrar,
    DROP COLUMN expires_at;
