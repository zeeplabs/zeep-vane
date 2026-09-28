# v0.8.0

## Added

- **Tenant custom domains behind a shared reverse proxy** — on a SaaS deployment where an external reverse proxy already owns ports 80/443 (e.g. EasyPanel's Traefik), a new opt-in flag `VANE_TENANT_DOMAINS_ON_ADMIN_LISTENER` (default `false`) serves published status pages' custom domains through the existing admin HTTP listener instead of the dedicated CertMagic `:443` listener. A request whose `Host` resolves to a published status page is served by the same public mux; every other `Host`, including the admin domain, falls through to the admin API/SPA unchanged. Off by default — no existing deployment's behavior changes. See the README's [Tenant custom domains behind a shared reverse proxy](https://github.com/zeeplabs/zeep-vane#-tenant-custom-domains-behind-a-shared-reverse-proxy) runbook.

## Fixed

- **SLO degraded/outage enrichment silently discarded (`tx is closed`)** — the async LLM enrichment goroutines (degraded tooltip, outage description, closing-comment proposal) kept the poll cycle's own database transaction alive in their context via `context.WithoutCancel`, but that transaction was already committed by the time their LLM call (up to 30s) finished, so every generated analysis/description and the LLM provider's own bookkeeping write failed and was discarded. Each enrichment goroutine now opens its own tenant-scoped transaction.
- **Auto-detected outage incidents failing to create (`tenant_id` NOT NULL violation)** — `IncidentRepository.Create` always opened a fresh, un-scoped transaction instead of reusing the caller's tenant-scoped one, so every SLOAnalyzer-created outage incident hit a NOT NULL constraint on `tenant_id` and was silently dropped. Any Datadog-monitored service that entered degraded/outage state on a prior version was affected — both bugs repeated on every such transition, not isolated incidents.

## Upgrade instructions

```bash
docker pull ghcr.io/zeeplabs/zeep-vane:0.8.0
```

No new migration in this release — `vane migrate up` is a no-op if you're already on `0.7.0`.

Helm:

```bash
helm repo update zeeplabs
helm upgrade zeep-vane zeeplabs/zeep-vane --version 0.8.0
```
