## Fixed

- **Domain health: expiration and NS-drift email alerts were silently never delivered.** `DomainHealthScheduler` called the notifier with a bare context that carried no tenant transaction. `tenant_memberships` enforces row-level security with a fail-closed policy (`user_id = app.user_id OR tenant_id = app.tenant_id`), which evaluates to false when neither session variable is set — so the recipient lookup silently returned zero rows and the notifier reported success, with no error and no log line. Every domain-expiring and NS-drift alert introduced in `v0.9.0` was affected: domains would cross their 30/15/7-day expiration thresholds, or drift NS records, without anyone being notified.

  Both notification call sites now run inside a tenant-scoped transaction (`BeginTenantTx`/`WithTenantTx`), the same pattern already used by the weekly digest scheduler and the incident poller. A new integration test (`internal/db/domain_health_notify_rls_test.go`) exercises the RLS behavior directly under a non-superuser role, proving both that an unscoped query fails closed and that a tenant-scoped query returns the real recipient.

**Upgrade note:** if you're running `v0.9.0`, upgrade to pick up domain expiration and NS-drift alerts that were silently not being sent — no configuration or migration changes are needed.
