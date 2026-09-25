# v0.7.0

## Added

- **SaaS transactional email via `zeep-notification-service`** — in `saas` deployment mode, all outbound transactional email (invites, verification, 2FA, password reset, incident notifications, weekly digest) now goes through Zeep's own platform notification service instead of a tenant-connected SendGrid/Resend integration. Self-hosted deployments are unaffected. Requires two new env vars in `saas` mode: `NOTIFICATION_SERVICE_URL`, `NOTIFICATION_SERVICE_API_KEY`.
- CI/CD migrated to CircleCI (`go-gate`, `go-integration`, `full-build`).

## Fixed

- Integrations page no longer shows email-provider cards in `saas` mode (tenants there can't connect their own provider).

## Security

- **Cross-tenant credential isolation fix for the Datadog integration.** The `integrations` table never got `tenant_id`/RLS in the original multi-tenancy rewrite. One tenant connecting Datadog could overwrite another tenant's stored key, and the poller used a single shared Datadog client across all tenants. Fixed via migration `0040` (adds `tenant_id` + forced RLS) plus a poller/analyzer refactor to resolve a Datadog client per tenant. See AD-037.
  - **Upgrade note**: if you run a multi-tenant (`saas`) deployment with more than one tenant that has connected Datadog, verify after upgrading that each tenant's stored Datadog key is still the one they expect.

## Upgrade instructions

```bash
docker pull ghcr.io/zeeplabs/zeep-vane:0.7.0
```

Run `vane migrate up` against your database before starting the new version — this release includes migration `0040_integrations_tenant_scope`.

Helm:

```bash
helm repo update zeeplabs
helm upgrade zeep-vane zeeplabs/zeep-vane --version 0.7.0
```
