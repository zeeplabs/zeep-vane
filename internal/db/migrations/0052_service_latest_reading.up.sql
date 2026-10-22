-- Latest burn-rate reading per service (poller-burn-rate-classification
-- BURN-13/BURN-17): the poller writes these together with current_status in
-- one UPDATE, so the admin detail drawer can show the last reading and the
-- grace timer survives a restart or a leader handover. All nullable, no
-- default and no backfill (metadata-only ALTER): NULL means "no reading yet"
-- and renders the empty state; a NULL last_error_at falls back to
-- last_status_change_at for the grace timer. The existing tenant RLS policy
-- on services covers these columns, so no new policy is needed.
ALTER TABLE services
  ADD COLUMN last_burn_rate_short double precision,
  ADD COLUMN last_burn_rate_long  double precision,
  ADD COLUMN last_window_errors   bigint,
  ADD COLUMN last_window_requests bigint,
  ADD COLUMN last_reading_at      timestamptz,
  ADD COLUMN last_error_at        timestamptz;
