# Domain Verification State Specification

## Problem Statement

The redesigned Domínios & Status Pages screen (`handoff-new-layout/Dominios e Status Pages.dc.html`) lists each registered domain with a status badge (verificado/pendente/erro), an SSL badge, a "verificado em" timestamp, the CNAME target to configure, and an error message when verification fails. Today `Domain` (`internal/db/domain_repository.go`) is just `{ID, Hostname, CreatedAt}` — none of that state exists on it. The actual DNS/TLS checking logic already exists (`internal/api/domain_verifier.go`, a real DNS lookup + TLS handshake), but it is wired only to `StatusPagesHandler.VerifyDomain`, keyed on a status page's `TLSLastError`/`State` — not to `Domain` itself, and produces a live, unpersisted result on every call rather than a stored status a list screen can render cheaply.

## Goals

- [ ] Every `Domain` carries a persisted verification state: `status` (`pending`/`verified`/`error`), `ssl_status` (`pending`/`active`/`error`), `verified_at`, `last_error` — renderable directly by the list screen without a live network check per row.
- [ ] An owner/operator can trigger a real re-check for one domain, updating its persisted state from the actual DNS/TLS result (reusing the existing `domainVerifier`).
- [ ] The screen shows the correct CNAME target for a newly added domain to configure.
- [ ] A newly created domain starts in a sensible default state (`pending`) without requiring an immediate check.

## Out of Scope

| Feature | Reason |
| --- | --- |
| `type: 'subdomain'` (Vane-hosted, zero-CNAME domains like `acme.vane.app`) | Decision confirmed with the user: this requires a shared base domain and its own DNS/TLS strategy that doesn't exist today (no `PUBLIC_BASE_DOMAIN` config, no wildcard-or-per-host TLS story for a shared hostname space) — real new infrastructure, not a migration. The screen ships showing only `custom` domains this cycle; `type` is modeled as an enum with a single allowed value today so it can be extended later without a second migration reshaping the column. |
| Automatic re-verification "a cada minuto" (as the mock's helper text claims) | No scheduler exists that could safely do this: a DNS+TLS check per domain, every minute, across every tenant's domains, both risks Let's Encrypt's per-hostname hourly rate limit on repeated failures and adds unbounded background load with no cap tied to how many domains exist. Matches the same "no invented automation" call already made for `poller-status-real-state`'s dropped restart action — verification stays a manual, rate-limited action (reusing the existing `verifyDomainCooldown` pattern) until there's a real capacity/rate-limit design for a scheduled version. |
| Changing `StatusPagesHandler.VerifyDomain`'s existing behavior or `StatusPage.State`/`TLSLastError` | That mechanism already works and is independently tested; this spec adds an equivalent, separate verification surface on `Domain` itself rather than risk regressing the existing status-page-scoped one. Some short-term duplication between the two is accepted rather than an invasive refactor of working code. |
| DNS record type validation (CNAME vs A vs ALIAS) | The verifier already checks IP-set overlap regardless of record type (`checkDNS`'s doc comment); this spec doesn't add record-type-specific rules beyond what already exists. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Schema | `domains` gains: `domain_type TEXT NOT NULL DEFAULT 'custom' CHECK (domain_type IN ('custom'))`, `status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','verified','error'))`, `ssl_status TEXT NOT NULL DEFAULT 'pending' CHECK (ssl_status IN ('pending','active','error'))`, `verified_at TIMESTAMPTZ NULL`, `last_error TEXT NULL` | Mirrors the mock's exact fields (type/status/ssl/verifiedAt/errorMsg) and the existing `incidents.status`-style CHECK-constraint convention already used twice this cycle. `domain_type` allowing only `'custom'` today is a deliberate narrow constraint, not a placeholder — adding `'subdomain'` later is a one-line `ALTER TYPE`/CHECK change once that feature is actually designed, not a silent gap. | y — agent default, no objection raised |
| CNAME target shown to the user | `config.Config.PublicDNSTarget` (already exists, already used by `StatusPagesHandler`'s `dnsTarget` for the exact same purpose) — the same value the operator already configured for their cluster, not a hardcoded `cname.vane.app` string as the mock's static copy shows | The mock's placeholder text is a fixed example for its own self-hosted demo; every real Vane installation already has its own `PUBLIC_DNS_TARGET`, and showing a value the operator didn't actually configure would be actively wrong instructions to a real end user. If `PublicDNSTarget` is unset (self-hosted operator never configured it), the response SHALL say so explicitly rather than showing an empty/misleading value. | y — agent default, no objection raised |
| New domain default state | `status: 'pending'`, `ssl_status: 'pending'`, `verified_at: NULL`, no verification attempt at creation time | Matches `AttachDomain`'s existing convention of starting a status page in a pending state (`pendingTLSState`) rather than eagerly dialing out; the operator triggers the first real check explicitly via the verify action, same as today's `VerifyDomain` flow. | y — agent default, no objection raised |
| Verify endpoint | New `POST /api/domains/{id}/verify` (`writeRoles`), reuses the existing `domainVerifier` interface/`netDomainVerifier` and the existing per-target `verifyDomainCooldown` (15s) pattern already implemented in `StatusPagesHandler`, applied per-domain here instead of per-status-page | Reuses the already-reviewed verification and cooldown logic instead of writing a second implementation of the same idea; keeping the cooldown prevents the same Let's-Encrypt-rate-limit risk `StatusPagesHandler.VerifyDomain`'s own comment already documents. | y — agent default, no objection raised |
| Mapping verifier result to persisted state | DNS resolves and either matches the configured target or no target is configured to compare against → `status: 'verified'`; DNS resolves but doesn't match the configured target → `status: 'error'` with `last_error` describing the mismatch; DNS doesn't resolve at all → `status: 'error'`, `last_error` from a fixed "DNS not resolved" message (matching the mock's own `d4` example: "Registro CNAME não encontrado..."). `ssl_status: 'active'` when `TLSCertValid`; `'error'` with `TLSError` in `last_error` when `TLSReachable` but not valid, or unreachable; `'pending'` only for a domain never yet checked (`verified_at` still NULL) | Directly derives the three-way badge (verified/pending/error) from the two real signals `domain_verifier.go` already produces, without inventing a fourth state the mock doesn't have. | y — agent default, no objection raised |
| RBAC | `POST /api/domains/{id}/verify` is `writeRoles` (owner/operator), matching `Create`/`Delete` on `DomainsHandler` today; `GET /api/domains` stays `anyRole`, now simply returning the additional fields | No change to who can read vs. mutate domains — only the response shape and one new mutating action. | y — agent default, no objection raised |

**Open questions:** none — all resolved or logged above.

---

## User Stories

### P1: Domain list shows real verification state ⭐ MVP

**User Story**: As any authenticated user, I want to see each domain's real status/SSL/verified-at state on the list, so I know which domains are actually working without opening each one.

**Why P1**: This is the entire redesigned list view's data source.

**Acceptance Criteria**:

1. WHEN `GET /api/domains` is called THEN the system SHALL include `domain_type`, `status`, `ssl_status`, `verified_at`, and `last_error` for every domain in the response.
2. WHEN a domain is first created via `POST /api/domains` THEN the system SHALL set `status: 'pending'`, `ssl_status: 'pending'`, `verified_at: NULL`, `domain_type: 'custom'` on the new row, matching the response of the immediate `POST` and any subsequent `GET`.
3. The response for a domain that has never been verified SHALL include the operator's configured CNAME target (`PublicDNSTarget`) so the mock's "aponte um registro CNAME para X" instruction is accurate for the real deployment, or an explicit signal that no target is configured.

**Independent Test**: Create a domain, `GET /api/domains`, confirm `status: 'pending'`/`ssl_status: 'pending'`/`verified_at: null` and the correct CNAME target from config.

---

### P1: Owner/operator triggers a real domain verification ⭐ MVP

**User Story**: As an owner/operator, I want to click "verificar" and have the domain's real DNS/TLS state persisted, so the badge reflects reality instead of staying `pending` forever.

**Why P1**: Without this, every domain would be stuck showing `pending` indefinitely — there's no other way the persisted state ever changes.

**Acceptance Criteria**:

1. WHEN an owner/operator calls `POST /api/domains/{id}/verify` for an existing domain THEN the system SHALL perform a real DNS+TLS check via the existing `domainVerifier`, persist the resulting `status`/`ssl_status`/`verified_at`/`last_error` on that domain, and respond `200` with the updated domain.
2. IF `{id}` does not reference an existing domain THEN the system SHALL respond `404` and SHALL NOT attempt a network check.
3. IF a verify call for the same `{id}` is made again within `verifyDomainCooldown` (15 seconds) of the previous one THEN the system SHALL respond with the existing persisted state (no new network check) rather than making a fresh network call, matching the existing per-target cooldown behavior on `StatusPagesHandler.VerifyDomain`.
4. WHEN the check succeeds (DNS resolves and matches the configured target, TLS cert valid) THEN the system SHALL set `status: 'verified'`, `ssl_status: 'active'`, and `verified_at` to the check's timestamp.
5. WHEN the check fails at either the DNS or TLS stage THEN the system SHALL set the corresponding field (`status` and/or `ssl_status`) to `'error'` and populate `last_error` with a description of what failed.

**Independent Test**: With a fake `domainVerifier` (matching the existing test-injection pattern), inject a successful result and confirm the domain becomes `verified`/`active`; inject a DNS failure and confirm `status: 'error'` with a populated `last_error`; call verify twice in immediate succession and confirm the second call doesn't trigger a second network call (assert on the fake verifier's call count).

---

## Edge Cases

- IF `PublicDNSTarget` is unset (self-hosted operator never configured `PUBLIC_DNS_TARGET`) THEN the domain-list/verify response SHALL say so explicitly (e.g. a null/empty target field with the frontend responsible for showing "not configured"), matching this codebase's existing convention for that same config value in `StatusPagesHandler`.
- WHEN a domain is deleted (`DomainsHandler.Delete`, already existing) THEN its verification state SHALL be deleted along with it — no separate cleanup needed since it lives on the same row.
- WHEN a verify check's DNS stage succeeds but no `PublicDNSTarget` is configured to compare against THEN the system SHALL treat DNS as satisfied (`status` not held at `error` purely for lacking a target to compare to) — mirrors `domain_verifier.go`'s own existing behavior where `DNSMatchesTarget` stays `nil` (no verdict) rather than false when no target is configured.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| DOMVER-01 | P1: List shows real verification state | Execute | Verified |
| DOMVER-02 | P1: New domain default state | Execute | Verified |
| DOMVER-03 | P1: CNAME target in response | Execute | Verified |
| DOMVER-04 | P1: Verify persists real result | Execute | Verified |
| DOMVER-05 | P1: Verify unknown domain (404) | Execute | Verified |
| DOMVER-06 | P1: Verify cooldown | Execute | Verified |
| DOMVER-07 | P1: Verify success mapping | Execute | Verified |
| DOMVER-08 | P1: Verify failure mapping | Execute | Verified |

**Coverage:** 8 total, 8 mapped to tasks, 0 unmapped (Medium scope — tasks were implicit in Execute, not a formal `tasks.md`)

---

## Success Criteria

- [x] `Domain` persists `domain_type`/`status`/`ssl_status`/`verified_at`/`last_error`.
- [x] `POST /api/domains/{id}/verify` performs a real check and persists the result, cooldown-protected.
- [x] The CNAME instruction shown to the user reflects the operator's real configured `PublicDNSTarget`, never a hardcoded example value.
- [x] `type: 'subdomain'` is not selectable or displayed — only `custom` domains exist this cycle.
