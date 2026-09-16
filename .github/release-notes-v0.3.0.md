# Release Notes for v0.3.0

This is a major release: Vane gains real multi-tenancy and a SaaS distribution model, two-factor authentication, per-device sessions, and a complete admin UI redesign — alongside a long list of smaller fixes and hardening. See `CHANGELOG.md` for the full list.

### Added

- **Multi-tenancy** (AD-022): Vane now supports two distribution models on the same codebase — self-hosted (single auto-provisioned tenant, no billing) and SaaS (real multi-tenant, public signup, free/paid plans). Data isolation is shared-schema + mandatory `tenant_id` on every domain table, enforced by Postgres RLS (fail-closed, not just an application filter).
- **SaaS public signup** with email verification, gated behind the new `VANE_DEPLOYMENT_MODE` env var (`self_hosted` | `saas`, default `self_hosted`) — self-hosted installs 404 every signup route outright, not just hide the UI link.
- **Two-factor authentication (TOTP)**: enroll/confirm/disable from Meu Perfil, recovery codes, a second-factor login step.
- **Per-device sessions**: Meu Perfil lists and can revoke any of the user's own sessions.
- **New admin UI**: a full visual redesign — collapsible Sidebar, Topbar, tenant/account popovers, and a light/dark design-token system. Every screen was rebuilt: Visão geral, Usuários, Domínios & Status Pages, Integrações, Serviços monitorados, Meu Perfil, and Configurações (now with full company profile, fiscal address, and self-service account deletion for SaaS owners).
- **Planos & Faturamento**: a decorative billing/plans showcase page — no real payment/licensing integration exists yet, every action is an honest "Em breve".
- **Manual polling mode**: monitor services via direct HTTP/TCP/Ping checks instead of requiring a Datadog SLO.
- **Notifications**: incident-opened/resolved emails, a weekly digest, per-admin preferences.
- **Incident severity** and update authorship in the timeline.
- **Real poller leadership reporting** on the Poller Status page.
- **Domain verification**: DNS-based verification is now actually checked and persisted.
- **Security hardening**: TLS/ACME private keys encrypted at rest; the IP rate limiter no longer fails open when Postgres is unavailable; the retention pruner is leader-gated to avoid duplicate runs across replicas.

### Fixed

- Self-hosted installs no longer show or accept account deletion — a self-hosted install is one tenant by design; deleting it would destroy the install, not close an account.
- The Overview page's upsell banner is disabled (no billing model exists behind it yet).
- A session token carrying an unexpected audience claim is now rejected.
- Numerous visual fixes across the redesigned screens to match the design handoff exactly.

### Upgrade notes

This release includes a substantial schema migration (`0024_multi_tenancy_core` through `0035_tenant_website_timezone_deleted_at`). Back up your database before upgrading and run migrations as usual (`vane migrate up` or the chart's built-in migration hook). Existing self-hosted installs are unaffected functionally — they continue to operate as a single tenant with no billing surface exposed.
