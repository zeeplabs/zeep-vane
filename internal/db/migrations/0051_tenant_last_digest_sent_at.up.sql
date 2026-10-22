-- Per-tenant marker for the weekly digest (DigestScheduler). The scheduler
-- moved from one global Monday 00:00 UTC fire to an hourly tick that sends
-- each tenant its digest at ~09:00 Monday in the tenant's own timezone; this
-- column is how a tick knows a tenant's digest already went out this week,
-- surviving restarts and shared across replicas (the leader lock serializes
-- the writers). Nullable with no default and no backfill: a metadata-only
-- ALTER, and NULL simply means "never sent" (the scheduler falls back to
-- the tenant's created_at so a tenant created mid-week isn't sent an
-- immediate catch-up digest).
ALTER TABLE tenants ADD COLUMN last_digest_sent_at TIMESTAMPTZ;
