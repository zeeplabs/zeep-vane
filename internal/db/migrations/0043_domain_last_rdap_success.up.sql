-- Domain health monitoring bugfix: last_rdap_check_at stamps every cycle
-- (success or failure), so a threshold crossing missed on a day RDAP failed
-- could never be detected on the next successful check - the "previous
-- band" derivation used LastRDAPCheckAt/ExpiresAt as if that pair only ever
-- reflected a successful lookup. last_rdap_success_at stamps only the cycles
-- that got real RDAP data, giving the crossing calculation an accurate
-- "state as of the last successful check" to compare against.
ALTER TABLE domains
    ADD COLUMN last_rdap_success_at TIMESTAMPTZ NULL;
