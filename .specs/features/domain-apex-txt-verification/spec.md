# Domain Apex TXT Verification Specification

## Problem Statement

A root domain registered in `vane`'s `domains` table (`internal/db/domain_repository.go`) is, by design, always the *parent* a status page's subdomain is later published under (`Domain` doc comment: "a registered root domain a status page's subdomain can be published under"; confirmed with the user this session — the domain-creation screen only ever registers the root/apex domain, and a subdomain is created later, at attach time, when a status page is linked to it via `AttachDomain`). The root domain itself never serves `vane` traffic and never gets a TLS certificate of its own — only `subdomain.hostname` does, once attached (`internal/db/status_page_repository.go`'s `PublicHostnameByID`/`hostnameMatch`, `internal/tls/manager.go`'s `HostPolicy`, `internal/api/status_pages_handler.go`'s `VerifyDomain`, all confirmed this session to operate on the full `subdomain.hostname` string, never the bare root).

Despite that, `internal/api/domain_verifier.go` (`netDomainVerifier.Verify`, used by `DomainsHandler.Verify` / `POST /api/domains/{id}/verify`) checks the **root hostname itself** for CNAME/A-record resolution against `PUBLIC_DNS_TARGET`, then performs a real TLS handshake against `hostname:443` expecting a valid certificate (`dialTLS`/`verifyServedCert`). Both checks are structurally unsatisfiable for a real root domain that already has its own DNS (MX, A/ALIAS for a marketing site, SPF/DKIM, etc.) pointing anywhere but `vane` — which is the normal, expected case, not an edge case. A real customer hit exactly this (Starbem, `starbem.ai`): the domain detail view asked for a CNAME record on the bare apex, which is both operationally wrong (would hijack the domain's existing routing) and technically invalid for most DNS providers (CNAME cannot coexist with other record types at the zone apex, RFC 1034 §3.6.2).

`vane` has no way today to confirm an operator actually controls a root domain without demanding it be reconfigured to serve `vane` traffic — which the product has already explicitly decided it should never do (existing `domain-health-monitoring` AD in `.specs/STATE.md`: "It never adds the ability to serve `vane` traffic on a tenant's root/apex domain; the root domain stays pointed at whatever infrastructure the operator already chose"). Domain ownership and domain routing are two different questions, and today's `Verify` conflates them.

The subdomain side (`AttachDomainDrawer.tsx`, `StatusPagesHandler.VerifyDomain`, `HostPolicy`) already works correctly end-to-end and is **out of scope** — this spec only changes how the root domain row itself proves ownership.

## Goals

- [ ] Registering a root domain in `vane` SHALL generate a unique verification token and expose the exact TXT record (name + value) the operator needs to create at their DNS provider, in place of today's CNAME instruction.
- [ ] `POST /api/domains/{id}/verify` SHALL check for that TXT record via a real DNS lookup and mark the domain `verified` only when it finds the exact expected token, instead of checking CNAME/A resolution and performing a TLS handshake against the root hostname.
- [ ] The domain detail view (`DomainDetailDrawer.tsx`) SHALL show the TXT record instruction (name, expected value, copy-to-clipboard) in place of today's CNAME table.
- [ ] The `ssl_status` concept (`pending`/`active`/`error`, `DomainDetailDrawer`'s SSL card, `DomainsTable`'s SSL column) SHALL be removed from the root domain model — a root domain never has its own TLS certificate under this product's design (only the attached subdomain does, tracked entirely by the existing, unrelated `StatusPage` state machine), so a per-domain SSL status field has no correct value to hold.
- [ ] Every existing domain row (`domain_type = 'custom'`, the only value that has ever existed) is treated uniformly by this change — this is a correction of what "verified" means for the one domain type that exists today, not the introduction of a second type.
- [ ] The existing subdomain-attach flow (`AttachDomainDrawer.tsx`, `PUBLIC_DNS_TARGET`, `StatusPagesHandler.VerifyDomain`, `HostPolicy`) is unchanged — it already asks for and verifies a CNAME on `subdomain.hostname`, which is correct and stays exactly as-is.

## Out of Scope

| Feature | Reason |
| --- | --- |
| A second `domain_type` value ("apex" vs "subdomain-CNAME") chosen by the operator at creation | Considered and rejected this session — confirmed with the user that `domains` rows are *always* root domains; the subdomain is created only at attach time and is not a `domains` row at all. There is only one kind of thing being verified here, so no new type/branch is needed. |
| Auto-detecting apex vs. subdomain via a public-suffix list | Rejected along with the above — moot once there's only one domain kind to verify. `golang.org/x/net/publicsuffix` (already an indirect dependency, confirmed in `go.mod`) is not needed. |
| Blocking `AttachDomain` for some "apex-typed" domain | Rejected along with the above — moot for the same reason. Note the existing invariant already holds without any new code: `AttachDomain`/`StatusPagesHandler.AttachDomain` already requires a non-empty `Subdomain` (422 otherwise), so a bare root hostname can never itself become a status page's public hostname today. |
| Any change to `AttachDomainDrawer.tsx`, `StatusPagesHandler.VerifyDomain`, `HostPolicy`, or `PUBLIC_DNS_TARGET`/CNAME instructions for the subdomain | Confirmed this session to already be implemented correctly end-to-end on the full `subdomain.hostname` string. Out of scope entirely, not deferred. |
| Re-verifying a domain automatically after the TXT record is removed post-verification | Deferred — same posture as today's on-demand cooldown-gated `Verify` (`verifyDomainCooldown`); this spec doesn't add periodic re-checking of the TXT record. A separate, already-shipped feature (`domain-health-monitoring`) periodically checks NS/RDAP for registered domains regardless of verification status; extending that scheduler to also recheck the TXT record is a natural follow-up but not required by this spec's goals. |
| Changing the domain-health-monitoring scheduler (`internal/cli/domain_health_scheduler.go`) | Unrelated — that feature already runs against every domain row regardless of verification state and does not touch DNS/TLS verification at all. |
| Multiple TXT tokens / rotation of an existing token | Rejected for simplicity — one token generated at `Create` time, immutable for the domain's lifetime (mirrors how `verified_at`/`status` already behave: set once per verification attempt, not user-editable). |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Root domain is the only thing `domains` rows represent; no new `domain_type` value | Single verification path for all existing/future rows | Confirmed explicitly by the user this session: the domain-creation screen only registers the root domain; the subdomain is created at attach time. | y |
| TXT record name | `_vane-verify.<hostname>` (a dedicated subdomain-of-the-record, not the bare apex) | Avoids colliding with or being confused for the domain's real TXT records at the apex (SPF, DKIM, other verification tools) — a dedicated name is the same convention used by most platforms with this exact ownership-proof pattern (e.g. `_github-challenge-<org>`, `_vercel`). Confirmed explicitly by the user this session over the bare-apex-with-prefix alternative. | y |
| Token format/generation | Random, sufficiently long opaque string (e.g. 32 hex chars from `crypto/rand`), generated once at `Create`, stored in a new `domains.verification_token` column | Matches the codebase's existing token-generation conventions elsewhere (session/invite tokens use `crypto/rand`); Design confirms the exact helper reused. | y (mechanism), n (exact length/encoding — Design decides) |
| `ssl_status` removal blast radius | Column dropped from `domains` (migration), `Domain` struct, `domainResponse`, `DomainDetailDrawer`'s SSL card, `DomainsTable`'s SSL column, `domainStatusMeta.ts`'s `sslStatusLabel`/`sslStatusColor`, both locale files, and all associated tests | Confirmed as a Goal above (no correct value to hold); Design enumerates the exact file list. | y (removal), n (exact column-drop migration — Design decides, may prefer a nullable/deprecated column now and a hard drop in a later migration if any read path outside this codebase depends on it — none found in this session's investigation) |
| Error messages for a failed TXT check | New copy replacing `dnsNotResolvedError`/`dnsMismatchError` (e.g. "TXT record not found" / "TXT record found but value does not match") | Mirrors the existing pattern (`internal/api/domains_handler.go`'s `mapDomainVerificationResult`) with new copy for the new check. | y |
| Re-verify cooldown (`verifyDomainCooldown`) | Unchanged | This spec changes what `Verify` checks, not its rate-limiting behavior. | y |

**Open questions:** none blocking Design.

---

## User Stories

### P1: Prove ownership of a root domain via TXT record ⭐ MVP

**User Story**: As an operator, I want to register my company's root domain in `vane` and prove I own it without being told to point its DNS at `vane` (which would break the domain's existing routing), so I can safely use `vane` for a status-page subdomain without touching the root domain's real configuration.

**Why P1**: This is the actual bug reported — the current CNAME/TLS instruction is unsatisfiable and, if followed literally, actively harmful to a domain already in use for other purposes.

**Acceptance Criteria**:

1. WHEN a root domain is registered (`POST /api/domains`) THEN the system SHALL generate a unique verification token for it and persist it alongside the domain row. <!-- event-driven -->
2. WHERE a domain has not yet been verified, the domain detail view SHALL display the exact TXT record name and value the operator must create at their DNS provider. <!-- state-driven -->
3. WHEN an operator triggers verification (`POST /api/domains/{id}/verify`) THEN the system SHALL perform a real DNS TXT lookup for the expected record name and mark the domain `verified` only if the record's value exactly matches the stored token. <!-- event-driven -->
4. WHEN the TXT lookup finds no matching record (not present, or present with a different value) THEN the system SHALL mark the domain `error` with a message describing which case occurred (not found vs. value mismatch), without ever suggesting a CNAME or TLS action. <!-- event-driven -->
5. WHEN a domain is already `verified` and the operator triggers verification again THEN the system SHALL still perform the TXT lookup (proving continued ownership, not just a one-time check) and update `status`/`verified_at` accordingly. <!-- event-driven -->

**Independent Test**: Register a root domain with no real DNS pointed at `vane`, add only the instructed TXT record via a test DNS zone (or an injected fake resolver in tests), trigger verify, confirm `status = "verified"`. Remove the record and re-verify; confirm `status = "error"` with a "not found" message.

### P2: Remove the meaningless SSL status for root domains

**User Story**: As an operator, I don't want to see an "SSL: Error" badge on a root domain that was never supposed to have its own certificate, so I'm not confused into thinking something is broken when nothing is.

**Why P2**: Direct consequence of P1 — today's `ssl_status` is always `error` for a correctly-configured root domain (it dials `hostname:443` expecting a cert `vane` never issues there), which is actively misleading, but it's a smaller/cleanup-shaped change than the core ownership-proof mechanism.

**Acceptance Criteria**:

1. The domain detail view SHALL NOT display an SSL status card. <!-- ubiquitous -->
2. The domains list table SHALL NOT display an SSL status column. <!-- ubiquitous -->
3. `GET /api/domains` and `POST /api/domains/{id}/verify` responses SHALL NOT include an `ssl_status` field. <!-- ubiquitous -->

**Independent Test**: Load the Domínios list and a domain's detail view; confirm no SSL-related badge/column/card is rendered anywhere; confirm the API response for a domain has no `ssl_status` key.

---

## Requirement Traceability

| ID | Acceptance Criterion (User Story) | Status |
| --- | --- | --- |
| DATV-01 | P1-AC1: verification token generated and persisted at domain creation | complete |
| DATV-02 | P1-AC2: detail view shows TXT record name/value instruction, not CNAME | pending |
| DATV-03 | P1-AC3: verify performs real TXT lookup, matches token exactly to mark verified | complete |
| DATV-04 | P1-AC4: TXT lookup failure maps to a distinct, non-CNAME/TLS error message | complete |
| DATV-05 | P1-AC5: re-verification of an already-verified domain still performs a real check | complete |
| DATV-06 | P2-AC1: SSL status card removed from domain detail view | pending |
| DATV-07 | P2-AC2: SSL status column removed from domains list table | pending |
| DATV-08 | P2-AC3: `ssl_status` field removed from API responses | complete |
