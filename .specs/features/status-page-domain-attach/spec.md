# Status Page Domain Attach Specification

## Problem Statement

Today `POST /api/status-pages` requires `domain_id` (`status_pages.domain_id UUID NOT NULL`, `0007_status_pages.up.sql:5`), and the admin-only preview endpoint (`GET /api/status-pages/{id}/public-preview`) refuses to render unless `state == "published"` (`public_status_preview_handler.go:68`) - a state that only exists after CertMagic issues a real TLS certificate, which itself only happens after the operator points DNS at the server. The result, confirmed by manual testing: a freshly created status page cannot be viewed by anyone, including its own admin, until a domain is fully configured and its DNS has propagated. There is no way to preview content, layout, or linked services before that infrastructure work is done.

## Goals

- [ ] A status page can be created with no domain and its preview is viewable immediately by an authenticated admin.
- [ ] A domain can be attached to an existing (undomained) status page later, through a dedicated screen that shows the DNS record the operator needs to create.
- [ ] The three-state ambiguity reported by the user - `draft` meaning both "no domain yet" and "domain set, waiting on DNS/certificate" - becomes distinguishable in the UI without adding a new database state.

## Out of Scope

| Feature | Reason |
| ------- | ------ |
| Detaching or changing a domain once attached | Not requested; only the "attach once" path is needed now. Revisit if the user asks for it. |
| Multiple domains per status page | Not requested; one domain per page, same as today's model. |
| System-wide default/wildcard domain (`*.vane.app`-style) | Rejected in an earlier decision this session: breaks the self-hosted, single-tenant model (AD-002) and would require sharing a wildcard TLS private key or a shared DNS zone across unrelated installations. |
| ACME DNS-01 challenge / wildcard certificates | Not needed - this feature keeps today's on-demand HTTP-01/TLS-ALPN-01 flow for the attached domain; no new ACME challenge type. |
| Auto-detecting the server's public IP/hostname | Self-hosted installs have arbitrary infra the app cannot introspect reliably; the operator supplies the value once via config (see Assumptions). |
| Fixing the pre-existing lack of a `(domain_id, subdomain)` uniqueness constraint at `POST /api/status-pages` (creation-with-domain path) | Pre-existing gap, not introduced by this feature. This spec DOES add the equivalent check to the new attach endpoint (see SPD-05), since that is new surface being built now. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --------------------- | --------------- | --------- | ---------- |
| DNS target value shown in the UI | New optional config var (e.g. `PUBLIC_DNS_TARGET`), a plain string the operator sets once at deploy time, exposed read-only via a new authenticated endpoint | Self-hosted infra is arbitrary; the app cannot discover its own public IP/hostname reliably. The operator already knows this value (it's their own server) - the app just needs to surface it in the UI instead of the admin having to know where to find it. | y (resolved this session) |
| Behavior when the operator never set the DNS target | Endpoint returns `target: null`; UI shows a message telling the admin the operator needs to configure it, but does NOT block attaching the domain itself (the admin may already know the correct value from other documentation) | Decouples "can I still attach a domain" from "does this app happen to know the target string" - the attach action's correctness doesn't depend on this cosmetic hint. | y |
| Preview endpoint no longer requires `state == "published"` | Preview (`public-preview`) renders for a status page in ANY state, including no domain attached at all | This endpoint is authenticated, admin-only, and was never the real public path (`SPEC_DEVIATION` already documents it as a dev/preview convenience, not production fidelity). The original "mirror production exactly" design choice (I12) is superseded here because it is the direct cause of the reported bug - production fidelity has no value to an admin who wants to check their page before DNS/TLS exist at all. | y (supersedes part of the I12 rationale - see design.md AD note) |
| Schema change | `status_pages.subdomain` and `status_pages.domain_id` both become nullable; a status page is created with both `NULL` and gets both set together, exactly once, by the new attach endpoint | Splitting them would allow a domain without a subdomain (meaningless combination) or vice versa - keeping them paired preserves the existing invariant that a real hostname is `subdomain.hostname` or nothing at all. | y |
| Re-attach / change already-attached domain | Out of scope (see Out of Scope table) - attach endpoint rejects with `409` if `domain_id` is already set | User asked only for "attach once, later" - no edit flow requested. | y |
| Concurrent attach requests racing on the same status page | Handled with a single conditional `UPDATE ... WHERE domain_id IS NULL`; the loser sees `0` rows affected and gets `409`, exactly like the sequential case | Standard optimistic guard - avoids a read-then-write race without adding a version column. | y |
| `(domain_id, subdomain)` collision on attach | Rejected with `409` if another status page already holds that exact pair | The attach endpoint is new surface; not enforcing this here would let a second page silently collide with an existing hostname, which `HostPolicy`'s `StateByHostname` JOIN cannot disambiguate. | y |
| RBAC on the new attach endpoint and the DNS-target read endpoint | Same as existing status-page/domain writes: `owner`+`operator` (`writeRoles`) for both | Consistent with `POST /api/status-pages` and `POST /api/domains` already being `writeRoles`-gated; a `viewer` has no reason to configure infrastructure. | y |
| State label ambiguity fix | No new DB state; frontend derives the label from `domain_id == null` (→ "sem domínio configurado") vs `domain_id != null && state == "draft"` (→ "aguardando validação de DNS/certificado") vs `published`/`tls_failed` unchanged | Solves the reported UX bug without touching `internal/tls/manager.go`'s existing 3-state machine - the ambiguity was only ever a frontend rendering problem once `domain_id` can legitimately be null. | y |

**Open questions:** none - all resolved or logged above.

---

## User Stories

### P1: Admin previews a status page before any domain exists ⭐ MVP

**User Story**: As an admin, I want to create a status page and see what it will look like immediately, so I don't have to configure DNS and wait for certificate issuance just to check content and layout.

**Why P1**: This is the core bug being fixed - today there is no way to view a status page's content before its domain's DNS/TLS is fully resolved.

**Acceptance Criteria**:

1. WHEN an admin (`owner` or `operator`) submits `POST /api/status-pages` without `domain_id`/`subdomain` THEN the system SHALL create the status page with `domain_id: null`, `subdomain: null`, `state: "draft"`, and respond `201`.
2. WHEN any authenticated admin (any role) requests `GET /api/status-pages/{id}/public-preview` for a status page with `domain_id: null` THEN the system SHALL respond `200` with the composed preview payload, regardless of `state`.
3. WHEN any authenticated admin requests the preview for a status page that has a `domain_id` set but `state != "published"` THEN the system SHALL still respond `200` (the endpoint no longer gates on `state`).
4. IF the requested status page ID does not exist THEN the system SHALL respond `404` (unchanged from today).
5. The system SHALL NOT require `domain_id` or `subdomain` to create a status page (schema and handler validation both relaxed).

**Independent Test**: Create a status page via the admin panel without picking a domain, click the preview link, and confirm the public page shape renders (services/incidents/company identity) with no domain ever having been touched.

---

### P1: Admin attaches a custom domain to an existing status page

**User Story**: As an admin, I want to attach a custom domain to a status page I already created, and see exactly what DNS record to configure, so publishing it for real is a deliberate, separate step from creating it.

**Why P1**: Without this, a status page created domain-less per the story above could never actually go live - this closes the loop back into the existing TLS/`HostPolicy` flow.

**Acceptance Criteria**:

1. WHEN an admin (`owner` or `operator`) submits the new attach-domain endpoint with a valid `domain_id` and non-empty `subdomain` for a status page whose `domain_id` is currently `null` THEN the system SHALL set both fields, leave `state` as `"draft"` (unchanged - the existing on-demand TLS mechanism takes over from here), and respond `200` with the updated status page.
2. IF the target status page already has a non-null `domain_id` THEN the system SHALL respond `409` and SHALL NOT modify the row.
3. IF the given `domain_id` does not reference an existing `Domain` THEN the system SHALL respond `422` and SHALL NOT modify the row.
4. IF `subdomain` is empty THEN the system SHALL respond `422` and SHALL NOT modify the row.
5. IF the exact `(domain_id, subdomain)` pair is already used by another status page THEN the system SHALL respond `409` and SHALL NOT modify the row.
6. WHEN an admin (`owner` or `operator`) requests the DNS-target read endpoint THEN the system SHALL respond `200` with the configured target string, or `null` if the operator never set it.
7. The system SHALL restrict both the attach-domain endpoint and the DNS-target read endpoint to `owner`/`operator` roles (`403` for `viewer`).

**Independent Test**: Create a domain-less status page, open its "attach domain" screen, see the DNS instructions (with the target value if configured), submit an existing domain + subdomain, and confirm the page now shows the same "aguardando certificado" state the current flow already produces for a normally-created page.

---

### P2: Distinguishable status labels replace the ambiguous "Emitindo certificado"

**User Story**: As an admin, I want the status page list/detail views to tell me clearly whether a page has no domain yet versus a domain that's still waiting on DNS/certificate, so I'm not left guessing why nothing is happening.

**Why P2**: Directly caused the reported confusion, but is a pure frontend rendering fix once P1's domain-less state exists - not required to unblock the P1 stories themselves.

**Acceptance Criteria**:

1. WHILE a status page has `domain_id: null` THE system SHALL render a label distinct from the certificate-pending label (e.g. "Sem domínio configurado"), with a call to action to attach one.
2. WHILE a status page has `domain_id` set and `state == "draft"` THE system SHALL render a label indicating DNS/certificate is pending (e.g. "Aguardando validação de DNS/certificado"), replacing today's ambiguous "Emitindo certificado" text.
3. WHILE a status page has `state == "published"` or `state == "tls_failed"` THE system SHALL keep today's existing labels unchanged.

**Independent Test**: View the status pages list with one domain-less page, one freshly-attached page, one published page, and one `tls_failed` page open simultaneously (via seeded fixtures) and confirm all four render distinct, correct labels.

---

## Edge Cases

- IF two concurrent attach requests target the same domain-less status page THEN the system SHALL let exactly one succeed (`200`) and the other SHALL receive `409` (no double-attach, no lost update) - covered by the conditional `UPDATE ... WHERE domain_id IS NULL` guard.
- IF an admin attaches a domain and its `(domain_id, subdomain)` pair happens to already be resolvable as a real hostname elsewhere (e.g. a page created via the legacy `POST /api/status-pages` with-domain path) THEN the collision SHALL be caught by SPD's attach-endpoint uniqueness check (SPD-05) even though the legacy creation path itself still lacks this check (documented pre-existing gap, Out of Scope).
- WHILE the operator has not configured the DNS target value, the attach-domain screen SHALL still allow submitting `domain_id`/`subdomain` - the missing hint is cosmetic, never a functional blocker.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --------------- | ----- | ----- | ------ |
| SPD-01 | P1: Preview before domain exists | Design | Implementing |
| SPD-02 | P1: Preview before domain exists | Design | Implementing |
| SPD-03 | P1: Preview before domain exists | Design | Implementing |
| SPD-04 | P1: Preview before domain exists | Design | Implementing |
| SPD-05 | P1: Attach domain later | Design | Implementing |
| SPD-06 | P1: Attach domain later | Design | Implementing |
| SPD-07 | P1: Attach domain later | Design | Implementing |
| SPD-08 | P1: Attach domain later | Design | Implementing |
| SPD-09 | P1: Attach domain later | Design | Implementing |
| SPD-10 | P1: Attach domain later | Design | Implementing |
| SPD-11 | P1: Attach domain later (RBAC) | Design | Implementing |
| SPD-12 | P2: Distinguishable labels | Design | Implementing |
| SPD-13 | P2: Distinguishable labels | Design | Implementing |
| SPD-14 | P2: Distinguishable labels | Design | Implementing |

**ID format:** `SPD-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 14 total, 14 mapped to tasks, 0 unmapped

---

## Success Criteria

- [ ] A status page created with no domain renders its preview immediately, with zero dependency on DNS/TLS.
- [ ] An existing domain-less status page can have a domain attached through a dedicated screen showing DNS instructions, without ever needing to have been created with a domain.
- [ ] The status pages list shows 4 visually distinct states (no domain / DNS pending / published / failed) with zero remaining occurrences of the ambiguous shared "Emitindo certificado" label for the no-domain case.
