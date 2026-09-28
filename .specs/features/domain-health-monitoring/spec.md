# Domain Health Monitoring Specification

## Problem Statement

`vane` today only observes the *service* behind a tenant's status page (Datadog SLO polling, `internal/poller`). It has no visibility into the *domain* itself — expiration, registrar, or nameserver configuration. Domain verification (`internal/api/domain_verifier.go`) only checks DNS resolution and TLS handshake, on demand, when an operator clicks "verify" in the admin UI (`internal/api/domains_handler.go`).

This blind spot caused a real production incident (Starbem, `eks-starbem-dev`, 2026-09-28): the root domain (`starbem.app`, registered at GoDaddy, DNS zone hosted on Route 53) went inoperative because its renewal payment failed. After renewal, the domain stayed broken for roughly 3 more hours because of DNS/nameserver cache propagation delay. Nothing in `vane` would have surfaced this risk in advance — no expiration warning, no way to see that the registrar (GoDaddy) and DNS host (Route 53) had drifted apart, no alert when the domain became unreachable at the registration level rather than the service level.

`vane` already has a `domains` table (`internal/db/domain_repository.go`) where operators register the domains they care about — today used for the CNAME/TLS attach flow to a status page. There is no periodic background job for domains at all; the only scheduled tenant-facing job today is the weekly digest (`internal/cli/digest_scheduler.go`).

## Goals

- [ ] For every domain registered in `vane` (via the existing "Domínios" admin screen), periodically fetch registration data (expiration date, registrar) via RDAP and surface it in the domain's detail view.
- [ ] Periodically read the domain's current nameservers (NS records) and detect when they drift from a baseline the system learned on the domain's first check — a real signal of registrar/DNS-host misconfiguration or an unexpected change.
- [ ] Alert tenants ahead of domain expiration at three thresholds (30 days, 15 days, 7 days) through the existing notification pipeline (the same one used for incident/SLO notifications), so a renewal-payment failure like the one that caused this feature to exist is caught with weeks of lead time instead of discovered as an outage.
- [ ] Alert tenants when nameserver drift is detected, since that is exactly the shape of problem (registrar vs. DNS host disagreement) that made this incident's recovery slow (3h propagation).
- [ ] This monitoring applies to **any domain already registered in `vane`**, including a bare root domain that has no status page or CNAME attached to it — `vane` does not need to serve traffic on a domain to monitor its health.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Serving traffic on a tenant's root/apex domain (routing `vane` itself onto e.g. `starbem.app` instead of a subdomain) | Explicitly rejected this session — the root domain today points at infrastructure `vane` doesn't control (Route 53 in the incident example), and making it a `vane`-served domain would require registrar-level changes and an admin choice with real operational weight. Out of scope entirely, not deferred. |
| WHOIS text-format fallback when RDAP is unavailable for a TLD | Explicitly rejected this session in favor of simplicity — RDAP only, no fallback parser. A TLD without RDAP support simply won't get expiration/registrar data; NS drift detection (plain DNS lookup) is unaffected either way. |
| Slack (or any new) notification channel for these alerts | Deferred — a Slack integration is planned for a future "integrations" section of `vane`, unrelated to this feature. This feature reuses whatever channel the existing notification pipeline already sends through (email today). |
| Historical audit trail of NS/expiration changes over time (a `domain_health_checks` table) | Considered as Approach B during design and rejected for now — no stated requirement for a change history/timeline, only current state + alerting. The chosen design (extend the `domains` row in place) can be migrated to add history later without breaking this feature's contract. |
| Admin-declared "expected NS" at domain creation time | Considered and rejected — the system learns the baseline automatically from the first successful check instead of requiring operator input. |
| Any change to the existing on-demand DNS/TLS `domain_verifier.go` flow | Unrelated and unaffected — this feature is a separate periodic background check, not a replacement for the existing manual "verify" button. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| RDAP client implementation | Direct HTTP client against `https://rdap.org/domain/{hostname}` (IANA-bootstrapped redirect to the correct registry RDAP server) — no third-party Go RDAP library dependency unless one proves necessary in Design | Keeps the dependency surface minimal, matches the existing codebase's preference for stdlib-first (`net.LookupNS`/`net.Resolver` used elsewhere). Confirmed shape only, library choice can be revisited in Design if `rdap.org` proves unreliable. | y (scope), n (exact client library — Design decides) |
| Polling frequency | NS/DNS check: same cadence as an existing periodic cycle (piggybacked or its own daily tick, Design decides). RDAP/expiration check: once per day. | RDAP registries commonly rate-limit; expiration state does not change meaningfully more often than daily. Confirmed explicitly by the user this session. | y |
| NS baseline source | Learned automatically on first successful check per domain, not operator-declared | Zero-config; user explicitly chose this over a manual "expected NS" field, accepting the trade-off that a domain already misconfigured at registration time won't be flagged until it changes again. | y |
| Expiration alert thresholds | 30 days (aviso), 15 days (aviso), 7 days (urgente) — alert fires once per threshold crossing, not on every daily check | User-specified explicitly. "Once per crossing" avoids alert spam on a domain sitting at, say, 6 days for a week straight. | y |
| Notification channel | Reuses the existing notification pipeline/`notification_preferences` (same one incident/SLO alerts use today) | User explicitly chose to reuse rather than build new infrastructure; Slack is future work, unrelated. | y |
| Scope of monitored domains | Every domain in the existing `domains` table, regardless of whether it has a status page/CNAME attached — including a bare root domain registered with no status page | User confirmed explicitly: the root-domain case (the one that caused the real incident) has no CNAME and must still be covered. | y |
| Scheduler infrastructure | New dedicated scheduler modeled on `internal/cli/digest_scheduler.go` (own ticker, own Postgres advisory lock in the `727200000–727299999` reserved block), not an extension of `internal/poller` (which is coupled to Datadog SLO/uptime semantics) | Confirmed via codebase investigation this session — no existing generic periodic-job infrastructure fits better; the digest scheduler is the closest existing pattern for "once-daily, per-tenant, background". | y |

**Open questions:** none blocking Design.

---

## User Stories

### P1: Domain expiration visibility and advance alerting ⭐ MVP

**User Story**: As an operator, I want `vane` to tell me when a domain I've registered is approaching its expiration date, with enough lead time to act, so a renewal-payment failure doesn't turn into an unplanned outage like the one that motivated this feature.

**Why P1**: This is the actual incident trigger — expiration/renewal failure, not NS misconfiguration, is what took the domain down in the first place.

**Acceptance Criteria**:

1. WHEN a domain is registered in `vane`'s `domains` table THEN the system SHALL periodically (at least once per day) query RDAP for that domain's expiration date and registrar, and persist the result. <!-- state-driven -->
2. WHEN a domain's RDAP-reported expiration date is within 30, 15, or 7 days of the current date AND that threshold was not already alerted for the domain's current expiration date THEN the system SHALL send a notification through the existing notification pipeline. <!-- event-driven -->
3. WHEN an RDAP query fails for a domain (timeout, unsupported TLD, rate limit) THEN the system SHALL record the failure without alerting spuriously and SHALL retry on the next scheduled cycle, without interrupting checks for other domains in the same cycle. <!-- event-driven -->
4. WHERE a domain's expiration data has been successfully fetched, the domain's detail view in the admin UI SHALL display the expiration date, days remaining, and registrar. <!-- state-driven -->

**Independent Test**: Register a domain whose RDAP-reported expiration is 6 days out; run the scheduler once; confirm a "7 days" notification fires exactly once and the domain detail view shows the expiration date and registrar.

### P2: Nameserver drift detection and alerting

**User Story**: As an operator, I want `vane` to tell me when a domain's nameservers change unexpectedly, so a registrar/DNS-host mismatch (like GoDaddy vs. Route 53 in the real incident) is visible before it causes a slow-propagating outage.

**Why P2**: Real contributor to the incident's *recovery time* (3h DNS cache propagation), though not the initial trigger — expiration is the higher-priority signal.

**Acceptance Criteria**:

1. WHEN a domain is checked for the first time (no stored baseline) THEN the system SHALL record its current nameservers as the baseline without alerting. <!-- event-driven -->
2. WHEN a domain with an existing NS baseline is checked and its current nameservers differ from the baseline THEN the system SHALL mark the domain as having nameserver drift and send a notification through the existing notification pipeline. <!-- event-driven -->
3. WHEN a domain's DNS lookup fails entirely (no NS records resolvable) THEN the system SHALL record the failure and SHALL NOT overwrite or clear the existing baseline. <!-- event-driven -->
4. WHERE a domain has nameserver drift detected, the domain's detail view AND the domains list SHALL visibly indicate this state. <!-- state-driven -->

**Independent Test**: Register a domain, run the scheduler once (baseline learned, no alert); change the domain's NS records (or point the test's DNS lookup at a different NS set); run the scheduler again; confirm a drift notification fires and the UI shows the divergence between baseline and current NS.

### P3: Domain health visible for domains with no status page attached

**User Story**: As an operator, I want to register and monitor a bare root domain (e.g. `starbem.app`) that has no CNAME or status page pointed at it, so the exact scenario that caused this feature's motivating incident is covered.

**Why P3**: Without this, the feature would only cover subdomains already used for status pages — missing the actual domain that broke in the real incident.

**Acceptance Criteria**:

1. The system SHALL include every domain in the `domains` table in the periodic health check cycle, regardless of whether it has an attached status page. <!-- ubiquitous -->
2. The domain creation drawer's hostname placeholder SHALL read as a root-domain example (`suaempresa.com`), not a subdomain example (`status.suaempresa.com`), since the subdomain used for a status page is created separately at attach time. <!-- ubiquitous -->

**Independent Test**: Register a root domain with no status page attached; confirm it appears in RDAP/NS check results and can show expiration/drift state in its detail view exactly like a domain that does have a status page.

---

## Requirement Traceability

| ID | Acceptance Criterion (User Story) | Status |
| --- | --- | --- |
| DHM-01 | P1-AC1: daily RDAP query per domain, expiration/registrar persisted | implemented |
| DHM-02 | P1-AC2: notification at 30/15/7-day thresholds, once per crossing | implemented |
| DHM-03 | P1-AC3: RDAP failure recorded, no spurious alert, cycle continues for other domains, retried next cycle | implemented |
| DHM-04 | P1-AC4: domain detail view shows expiration date, days remaining, registrar | implemented |
| DHM-05 | P2-AC1: first check with no baseline records current NS as baseline, no alert | implemented |
| DHM-06 | P2-AC2: NS differing from baseline marks drift and notifies | implemented |
| DHM-07 | P2-AC3: DNS lookup failure recorded, existing baseline never overwritten/cleared | implemented |
| DHM-08 | P2-AC4: domain detail view + domains list visibly indicate NS drift | implemented |
| DHM-09 | P3-AC1: every domain in `domains` table included in the cycle, with or without an attached status page | implemented |
| DHM-10 | P3-AC2: `AddDomainDrawer` hostname placeholder reads as a root-domain example, not a subdomain | implemented |
