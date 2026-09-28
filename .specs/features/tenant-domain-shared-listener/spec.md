# Tenant Custom Domain Behind a Shared Reverse Proxy Specification

## Problem Statement

Today a tenant's custom status-page domain only ever resolves through `newHTTPSServer` (`internal/cli/serve.go:212`), the dedicated `:443` listener where `vane serve` itself terminates TLS via CertMagic (on-demand HTTP-01/TLS-ALPN-01, gated by `tls.HostPolicy`). That listener requires binding real host ports `80`/`443` directly to the `vane` process.

This breaks down on the SaaS EasyPanel deployment (confirmed this session, no code change needed to reproduce): EasyPanel's own Traefik instance already owns ports `80`/`443` on the host to route every app it manages, including `vane`'s own admin domain. There is no way to also bind those ports directly to the `vane` container without either (a) a second dedicated public IP just for `vane`'s raw listener, or (b) hand-written custom Traefik dynamic config (`/etc/easypanel/traefik/config/custom.yaml`) doing raw TCP/TLS passthrough by SNI for every future tenant domain sight-unseen — both real infra burdens with no code-side alternative today. Self-hosted single-tenant installs are unaffected (there `vane` is usually the only thing on the box and can own `80`/`443` outright); this is specifically a SaaS-with-shared-reverse-proxy problem.

Separately, `router.HostRouter` (`internal/router/host_router.go:92`) — the function that resolves a request's `Host` header to a published `StatusPage` and scopes the request's RLS transaction to that tenant — is hard-coded to answer `404` for any hostname it does not resolve to a published status page (`host_router.go:97-104`). It has no "fall through to something else" behavior; it was built assuming its caller (`newHTTPSServer`) never receives admin traffic in the first place, since the admin API/SPA lives on a completely separate listener (`buildAdminRouter`, `internal/cli/routes.go:48`, bound in `RunE` at `serve.go:123`).

## Goals

- [ ] A tenant's custom status-page domain resolves correctly when TLS for that domain is terminated by an external reverse proxy (EasyPanel/Traefik, or any other) in front of `vane`'s existing admin HTTP listener (`PORT`), instead of requiring `vane`'s own CertMagic listener to own ports `80`/`443` on the host.
- [ ] The existing admin domain (SPA + API) keeps working unchanged on that same listener, on the same port, with no behavior change for self-hosted or for a SaaS deploy that has no tenant custom domains yet.
- [ ] `vane`'s own CertMagic `:443` listener (`newHTTPSServer`) keeps working exactly as today for any deployment that can dedicate ports `80`/`443` to it directly (self-hosted default, or SaaS on infra with a spare IP) — this feature adds a second way to serve tenant domains, it does not remove the first.
- [ ] The operational runbook for registering a new tenant domain with the external reverse proxy (e.g. EasyPanel's own Domains feature/API) so it forwards to `vane`'s admin port is documented, even if performed manually rather than automated by this feature (see Out of Scope).

## Out of Scope

| Feature | Reason |
| --- | --- |
| Automating tenant-domain registration against the EasyPanel API (or any specific reverse proxy's API) when `POST /api/domains/{id}/attach` succeeds | Real, separate integration (new credential, new connector package, comparable in size to `internal/connectors/notificationservice`) — decided this session to defer until deploy volume justifies it. This spec only makes the shared-listener *routing* possible; registering the domain with the external proxy stays a manual operator step for now. |
| DNS-01 ACME challenge, or any other certificate-issuance method change | `vane`'s own CertMagic path (HTTP-01/TLS-ALPN-01) is unaffected by this feature — when TLS is terminated externally (this feature's whole point), certificate issuance for that domain is the external proxy's job entirely, not `vane`'s. `tls.HostPolicy`/`tls.NewManager` are untouched. |
| Removing or replacing `newHTTPSServer`/the CertMagic `:443` listener | Still the right answer for a deployment that can dedicate `80`/`443` to `vane` directly; this feature is additive. |
| Manual/uploaded tenant-provided TLS certificates | Discussed and rejected this session as a separate alternative — no existing admin UI/endpoint for it, worse renewal UX, unrelated to this feature's actual gap (routing, not certificate sourcing). |
| Changing `tls.HostPolicy`'s abuse-prevention gate (registered + non-draft status pages only) | This feature reuses the same `statusPageHostLookup` resolution `HostRouter` already does; the gate itself is correct today and not implicated by where TLS terminates. |
| Detecting at boot whether an external proxy is actually in front of `PORT` (vs. exposed directly) | Purely an operator config choice (see Assumptions) — `vane` cannot reliably introspect its own network topology, matching the existing `PUBLIC_DNS_TARGET`/`VANE_ADMIN_BASE_URL` precedent of trusting operator-supplied config over self-discovery. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| How the admin listener decides "this Host header is the admin domain, not a tenant domain" | `HostRouter` gains a `fallback http.Handler` parameter: an unresolved/unpublished hostname now calls `fallback` instead of hard-coding `404`. `buildAdminRouter`'s existing router becomes that fallback when `HostRouter` is mounted on the primary listener. | Matches how the codebase already treats "any other hostname" in prose (`host_router.go:70-71`'s own comment already claims this behavior, but the implementation never actually took a fallback param - the comment was aspirational/stale, not implemented). Smallest change that preserves every existing `HostRouter` test's contract for the *matched* case, only changing the unmatched case. | n — technical default, revisit in Design if a cleaner shape (e.g. two `chi` routes instead of a fallback param) fits the existing router composition better |
| Whether this is opt-in or always-on | New config flag (name TBD in Design, e.g. `VANE_TENANT_DOMAINS_ON_ADMIN_LISTENER`), default `false` | Self-hosted and any SaaS deploy without this problem should see zero behavior change by default - mounting `HostRouter` in front of the admin router on every deploy, unconditionally, adds a Postgres round-trip (`GetByHostname`) to every single admin API/SPA request's hot path for no benefit where the dedicated `:443` listener already works fine. | n — technical default, revisit in Design |
| `VANE_HTTPS_ENABLED=false` interaction | When the new flag above is `true`, `VANE_HTTPS_ENABLED` continues to independently control whether `newHTTPSServer` also starts - the two are not mutually exclusive, an operator could run both listeners if some tenant domains are on dedicated infra and others are behind the shared proxy | No stated requirement to make these mutually exclusive; keeping them independent avoids over-constraining a future mixed deployment. | n — revisit in Design if it turns out to be confusing/error-prone to allow both |
| Does the shared-listener path need its own TLS/HSTS handling | No - unlike `newHTTPSServer` (`api.SecurityHeaders(true)`, `hsts=true`, since it terminates TLS itself), the admin listener is plain HTTP behind the external proxy exactly as it is today; HSTS/TLS is the external proxy's responsibility for whichever Host header it forwards | The admin listener already runs this way in every current SaaS deployment (EasyPanel terminates TLS, forwards HTTP to `PORT`) - this feature doesn't change that model, it only adds a second `Host`-header branch to the same already-HTTP listener. | y |
| RBAC / auth impact | None - the shared-listener path only ever reaches unauthenticated, read-only routes (public status JSON, logo file, static SPA), exactly like `newHTTPSServer` today; it never exposes an authenticated admin route under a tenant's custom hostname | `HostRouter`'s existing contract (SP-15, TENANT-01/02/03) already establishes this boundary for the dedicated-listener case; reusing the same function on the shared listener inherits it unchanged. | y |

**Open questions:** none blocking Design - the two "n" rows above are technical shape decisions, not product decisions, and don't need another round of user confirmation before Design proceeds.

---

## User Stories

### P1: Tenant custom domain resolves through the shared admin listener ⭐ MVP

**User Story**: As a SaaS operator running `vane` behind an external reverse proxy that already owns ports 80/443 (EasyPanel/Traefik), I want a tenant's custom status-page domain to resolve correctly when forwarded to `vane`'s existing admin port, so I don't need a second dedicated IP or hand-written TCP-passthrough Traefik config just to support one tenant custom domain.

**Why P1**: This is the entire blocking gap - without it, tenant custom domains are simply not supported on a shared-reverse-proxy SaaS deployment at all.

**Acceptance Criteria**:

1. WHILE the new opt-in flag is enabled, WHEN a request arrives on the admin listener (`PORT`) with a `Host` header matching a published `StatusPage`'s hostname THEN the system SHALL serve the same public status response (JSON/logo/SPA) that `newHTTPSServer` serves today for that hostname, with the same tenant-scoped RLS transaction (`BeginTenantTx` against that `StatusPage.TenantID`) and the same `StatusPageIDFromContext`/`TenantIDFromContext` wiring. <!-- state-driven -->
2. WHILE the new opt-in flag is enabled, WHEN a request arrives on the admin listener with a `Host` header that does NOT match any published `StatusPage` THEN the system SHALL fall through to the existing admin router (`buildAdminRouter`) unchanged - same admin API/SPA behavior as today, no regression. <!-- state-driven -->
3. WHILE the new opt-in flag is disabled (default), the admin listener's behavior SHALL be byte-for-byte identical to today - no `GetByHostname` lookup added to its request path at all. <!-- state-driven -->
4. The system SHALL NOT require any change to `newHTTPSServer`, `tls.HostPolicy`, or `tls.NewManager` - a deployment already using the dedicated `:443` listener SHALL be unaffected by this feature existing. <!-- ubiquitous -->

**Independent Test**: With the flag enabled and a published status page's domain pointed (via the external proxy) at the admin listener's port, request that hostname and confirm the public status JSON/SPA renders; request the admin domain's own hostname on the same listener and confirm the admin SPA/API still renders unchanged.

### P2: Operational runbook for registering a tenant domain with the external proxy

**User Story**: As a SaaS operator, I want a documented manual procedure for pointing a newly attached tenant domain at `vane` through EasyPanel (or an equivalent proxy), so I can support a customer's custom domain today without waiting on the automated-registration feature (Out of Scope).

**Why P2**: Necessary to actually operate this in practice, but it's a documentation/runbook deliverable, not new application code - lower priority than the routing mechanism itself.

**Acceptance Criteria**:

1. The system's documentation (README or a deploy runbook) SHALL describe, step by step, how an operator adds a tenant's attached domain to the external proxy so it forwards to `vane`'s admin port, using EasyPanel's Domains feature as the concrete worked example. <!-- ubiquitous -->
2. The documentation SHALL state explicitly that this step is manual per tenant domain today, and point at the deferred automation (Out of Scope) as the future improvement. <!-- ubiquitous -->

**Independent Test**: Follow the documented steps for a fresh tenant domain attach end-to-end (attach in `vane`'s UI → register in EasyPanel → DNS propagates → status page loads over HTTPS) with no undocumented step required.
