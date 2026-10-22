ALTER TABLE services
  DROP COLUMN last_burn_rate_short,
  DROP COLUMN last_burn_rate_long,
  DROP COLUMN last_window_errors,
  DROP COLUMN last_window_requests,
  DROP COLUMN last_reading_at,
  DROP COLUMN last_error_at;
