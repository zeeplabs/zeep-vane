# Domain Apex TXT Verification Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/domain-apex-txt-verification/design.md`
**Status**: Approved

---

## Requirement IDs

| ID | Acceptance Criterion (spec.md) |
| --- | --- |
| DATV-01 | P1-AC1: verification token generated and persisted at domain creation |
| DATV-02 | P1-AC2: detail view shows TXT record name/value instruction, not CNAME |
| DATV-03 | P1-AC3: verify performs real TXT lookup, matches token exactly to mark verified |
| DATV-04 | P1-AC4: TXT lookup failure maps to a distinct, non-CNAME/TLS error message |
| DATV-05 | P1-AC5: re-verification of an already-verified domain still performs a real check |
| DATV-06 | P2-AC1: SSL status card removed from domain detail view |
| DATV-07 | P2-AC2: SSL status column removed from domains list table |
| DATV-08 | P2-AC3: `ssl_status` field removed from API responses |

---

## Test Coverage Matrix

> Generated from codebase sampling (`internal/api/domain_verifier.go` + its handler test fakes, `internal/db/domain_repository_test.go`, `web/src/features/domains/*.test.tsx`) plus spec ACs. Guidelines applied: `AGENTS.md` §3 (backend gate commands, integration-DB hard rule) and §5 (frontend i18n/MSW-parity rule).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Go migration (`domains` columns) | none | Build/gate only - schema change verified by the repository tests that exercise it | `internal/db/migrations/` | `go build ./...` (embedded migration compile check) |
| Go repository (`DomainRepository`) | integration | `Create` persists a non-empty `VerificationToken`; `SetVerificationResult` no longer touches `ssl_status`; existing `ListPaginated`/`GetByID` scans updated and still RLS-correct - DATV-01 | `internal/db/domain_repository_test.go` | `go test -tags=integration ./internal/db` |
| Go domain logic (`netDomainVerifier.checkTXT`) | unit | All branches: exact match, value mismatch, record absent, multiple TXT records at the name with only one matching, resolver timeout - DATV-03, DATV-04 | `internal/api/domain_verifier_test.go` | `go test ./internal/api` |
| Go domain logic (`mapDomainVerificationResult`) | unit | Table-driven: found+match → verified/nil error; found+mismatch → error/mismatch message; not found → error/not-found message - DATV-03, DATV-04 | `internal/api/domains_handler_test.go` | `go test ./internal/api` |
| Go API (`DomainsHandler.Verify`, `.Create`, `.List`) | integration | `Create` response has no `ssl_status` and (if exposed) a TXT value field; `Verify` end-to-end against an injected fake verifier persists `status`/`verified_at` correctly on both match and mismatch; re-verify-when-already-verified still calls the verifier (not short-circuited by state) - DATV-01, DATV-03, DATV-05, DATV-08 | `internal/api/domains_handler_test.go` | `go test -tags=integration ./internal/api` |
| Frontend components (`DomainDetailDrawer`, `DomainsTable`) | unit (vitest + Testing Library) | TXT table renders record name/value; no SSL card/column renders anywhere; existing health-section/status-badge tests untouched - DATV-02, DATV-06, DATV-07 | `web/src/features/domains/DomainDetailDrawer.test.tsx`, `web/src/features/domains/DomainsTable.test.tsx` | `npm run test` (in `web/`) |
| Frontend types/i18n | none (typecheck + parity script only) | `Domain` type has no `ssl_status`; new TXT field typed; pt-BR/en keys in parity, no orphaned SSL keys | `web/src/types/api.ts`, `web/src/locales/*.json` | `npx tsc -b --noEmit && npm run i18n:check` (in `web/`) |

## Gate Check Commands

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick (backend, unit only) | After a backend task with no DB-backed test | `go test ./...` |
| Full (backend, integration) | After a backend task with a DB-backed/integration test - **disposable Postgres container only, never `vane-dev-pg`** (`AGENTS.md` §3) | `make test-integration` |
| Build (backend) | After phase completion | `go build ./... && gofmt -l . && go vet ./...` |
| Frontend (unit) | After a frontend task | `cd web && npm run test` |
| Frontend (full) | After frontend phase completion | `cd web && npx tsc -b --noEmit && npm run test && npm run i18n:check` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Schema

Task: T1

### Phase 2: Backend - repository, verifier, handler

Tasks: T2, T3, T4

### Phase 3: Frontend

Tasks: T5, T6, T7, T8, T9

### Phase 4: Docs

Tasks: T10, T11

---

## Task Breakdown

### T1: Migration - `domains.verification_token` added, `domains.ssl_status` dropped

**What**: New migration adds `verification_token TEXT NOT NULL` to `domains`, backfilling existing rows with a real generated value (`DEFAULT encode(gen_random_bytes(16), 'hex')` at add-time, then `ALTER COLUMN ... DROP DEFAULT` so new rows must supply their own token from application code, not the database). Same migration drops `ssl_status`. `.down.sql` reverses both: drops `verification_token`, re-adds `ssl_status TEXT NOT NULL DEFAULT 'pending'` matching `0029`'s original definition (including its `CHECK (ssl_status IN ('pending','active','error'))`).
**Where**: `internal/db/migrations/0044_domain_txt_verification.up.sql`, `.down.sql`
**Depends on**: None
**Reuses**: existing migration file-pair convention (see `0043_domain_last_rdap_success.up.sql`/`.down.sql`)
**Requirement**: DATV-01 (prerequisite), DATV-08 (prerequisite)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `.up.sql` adds `verification_token` (backfilled, non-null, no lingering default) and drops `ssl_status`; `.down.sql` exactly reverses both, including the original `CHECK` constraint on the restored `ssl_status`
- [ ] Migration numbered `0044` (next after `0043`), matches naming convention exactly
- [ ] Gate check passes: `go build ./... && gofmt -l . && go vet ./...`

**Tests**: none directly (exercised by T2's integration tests)
**Gate**: build

**Commit**: `feat(db): add domain verification token, drop ssl_status`

---

### T2: `DomainRepository` - `VerificationToken` field, drop `SSLStatus`

**What**: `Domain` struct gains `VerificationToken string`; `SSLStatus` field removed. `Create`'s `INSERT`/`RETURNING`, `ListPaginated`/`GetByID`'s `SELECT` column lists, and `SetVerificationResult`'s `UPDATE`/signature are all updated to match (drop `ssl_status` everywhere, add `verification_token` to `Create`/`ListPaginated`/`GetByID`). `SetVerificationResult` drops its `sslStatus string` parameter. Token generation itself happens in `Create` (or the caller, per whichever existing `crypto/rand`-based token helper the codebase already uses elsewhere - confirm exact call site before writing a new one).
**Where**: `internal/db/domain_repository.go`, `internal/db/domain_repository_test.go`
**Depends on**: T1
**Reuses**: existing `crypto/rand` token-generation helper (exact location confirmed at implementation time)
**Requirement**: DATV-01

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `Domain.VerificationToken` populated by `Create`, never empty
- [ ] `Domain.SSLStatus` removed from struct and all SQL
- [ ] `SetVerificationResult` signature no longer takes `sslStatus`
- [ ] Integration tests updated/added for token presence and the new `SetVerificationResult` signature, run under RLS per `AGENTS.md` §3 (disposable container only)
- [ ] Gate check passes: `make test-integration`

**Tests**: `internal/db/domain_repository_test.go`
**Gate**: full (integration)

**Commit**: `feat(db): generate verification token on domain creation`

---

### T3: `domain_verifier.go` - TXT lookup replaces CNAME/TLS check

**What**: `domainVerificationResult` rewritten: drop `ResolvedIPs`, `DNSMatchesTarget`, `TLSReachable`, `TLSCertValid`, `TLSError`; add `TXTFound bool`, `TXTMatches bool`. `netDomainVerifier.Verify(ctx, hostname, expectedToken string)` performs `net.Resolver.LookupTXT(ctx, "_vane-verify."+hostname)` and checks whether any returned value equals `expectedToken` exactly. `checkDNS`, `resolveIPs`, `dialTLS`, `verifyServedCert`, `ipSetsOverlap`, and the TLS-related imports/fields (`dialTimeout`'s ACME-headroom comment, `crypto/tls`, `crypto/x509` imports) are deleted - replaced by a single `checkTXT` method with its own bounded lookup timeout (mirrors today's 5s `context.WithTimeout` on `checkDNS`). The `domainVerifier` interface's `Verify` signature keeps its shape (`ctx, hostname, expectedTarget/expectedToken string`) so `DomainsHandler`'s test fake pattern is unaffected beyond the field-name rename.
**Where**: `internal/api/domain_verifier.go`, its test file (create if none exists, or extend `internal/api/domains_handler_test.go`'s existing fake verifier if that's where it lives today)
**Depends on**: T1 (needs `verification_token` to exist as the thing being checked, though this task itself has no DB dependency directly)
**Reuses**: existing `net.Resolver`/context-timeout pattern from today's `checkDNS`
**Requirement**: DATV-03, DATV-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `checkTXT` performs a real `LookupTXT` against `_vane-verify.<hostname>` and reports `TXTFound`/`TXTMatches` correctly for: exact match, mismatch, absent record, multiple records with one matching
- [ ] All TLS-dial code paths removed - no code in this file ever opens a TCP/TLS connection
- [ ] Unit tests cover all branches listed above via an injected fake `*net.Resolver`-equivalent (mirror whatever seam today's tests use, or introduce a small resolver interface if none exists) - DATV-03, DATV-04
- [ ] Gate check passes: `go test ./internal/api && go build ./... && gofmt -l . && go vet ./...`

**Tests**: `internal/api/domain_verifier_test.go` (or colocated per final file layout)
**Gate**: quick

**Commit**: `feat(api): verify root domain ownership via TXT record, not CNAME/TLS`

---

### T4: `domains_handler.go` - wire TXT verification through `Verify`/`Create`/`List`

**What**: `domainResponse` drops `SSLStatus`, gains a TXT-instruction field carrying the expected record value (mirrors the existing `dns_target` pattern - exact JSON field name decided here, e.g. `verification_txt_value`). `toDomainResponse` updated accordingly. `Verify` calls `h.verifier.Verify(r.Context(), domain.Hostname, domain.VerificationToken)` (no longer passing `h.dnsTarget` into this call - that field/config stays on the handler only for the unrelated `dns_target` API field the subdomain-attach flow still needs). `mapDomainVerificationResult` rewritten around `TXTFound`/`TXTMatches`, returns `(status string, lastError *string)` (no more `sslStatus`). `dnsNotResolvedError`/`dnsMismatchError` replaced with TXT-specific constants and copy. Audit log entries (`"domain_verified"`) unchanged.
**Where**: `internal/api/domains_handler.go`, `internal/api/domains_handler_test.go`
**Depends on**: T2, T3
**Reuses**: existing `verifyDomainCooldown`/`checkVerifyCooldown`, existing audit-log call pattern, existing `Page`-adjacent response-shape convention (`domainsPageResponse`'s loose `dns_target` field, per `AGENTS.md` §4)
**Requirement**: DATV-01, DATV-03, DATV-04, DATV-05, DATV-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `GET /api/domains`, `POST /api/domains`, `POST /api/domains/{id}/verify` responses contain no `ssl_status` key and do contain the new TXT-value field
- [ ] `Verify` on an already-`verified` domain still invokes the verifier (not skipped by current state) and updates `status`/`verified_at` from the fresh result, subject only to the existing cooldown
- [ ] New error copy is TXT-specific, no residual "CNAME"/"DNS not resolved" language reachable from this handler
- [ ] Integration tests updated/added for all of the above, run under RLS per `AGENTS.md` §3
- [ ] Gate check passes: `make test-integration`

**Tests**: `internal/api/domains_handler_test.go`
**Gate**: full (integration)

**Commit**: `feat(api): drop ssl_status from domain API responses, surface TXT verification value`

---

### T5: `web/src/types/api.ts` - drop `DomainSSLStatus`/`ssl_status`, add TXT field

**What**: `DomainSSLStatus` type and `Domain.ssl_status` field removed. `Domain` gains the new TXT-value field matching T4's API shape (e.g. `verification_txt_value: string | null`).
**Where**: `web/src/types/api.ts`
**Depends on**: T4
**Reuses**: existing `Domain` type shape/conventions
**Requirement**: DATV-02, DATV-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `DomainSSLStatus` type deleted, no remaining reference anywhere in `web/src`
- [ ] New TXT field typed and matches the backend JSON key exactly
- [ ] `npx tsc -b --noEmit` passes (in `web/`) with zero new errors

**Tests**: none (type-only change, exercised by downstream component tests)
**Gate**: frontend (unit not required for this task alone; full typecheck sufficient here)

**Commit**: `chore(web): drop ssl_status from Domain type, add verification TXT value field`

---

### T6: `domainStatusMeta.ts` - remove SSL status helpers

**What**: `sslStatusLabel` and `sslStatusColor` exports deleted (dead once T7/T8 remove their only call sites).
**Where**: `web/src/features/domains/domainStatusMeta.ts`, `web/src/features/domains/domainStatusMeta.test.ts`
**Depends on**: T5
**Reuses**: n/a (deletion)
**Requirement**: DATV-06, DATV-07

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `sslStatusLabel`/`sslStatusColor` removed from the module and its test file
- [ ] No remaining import of either symbol anywhere in `web/src` (grep confirms zero hits)

**Tests**: `web/src/features/domains/domainStatusMeta.test.ts`
**Gate**: frontend (unit)

**Commit**: `chore(web): remove dead SSL status helpers from domainStatusMeta`

---

### T7: `DomainDetailDrawer.tsx` - TXT table replaces CNAME table, SSL card removed

**What**: The existing `current.domain_type === "custom"` CNAME block (record name `CNAME`, value from `useDNSTarget()`) is replaced with a TXT block: record name `_vane-verify.<hostname>`, value from the new `verification_txt_value` field, using the same card/grid styling. The adjacent SSL status grid cell (`domains.detail.sslLabel`) is deleted; the "verified at" cell is regrouped to stand alone or with the domain-health section per implementation's layout judgment (no behavior change either way). A copy-to-clipboard affordance is added for the TXT value if an existing reusable "copyable value" component is found elsewhere in `web/src/components/ui/`; otherwise this task ships without one (not blocking - clipboard convenience is not an acceptance criterion).
**Where**: `web/src/features/domains/DomainDetailDrawer.tsx`, `web/src/features/domains/DomainDetailDrawer.test.tsx`
**Depends on**: T5, T6
**Reuses**: existing drawer card/grid layout, `useDNSTarget()`'s sibling hook pattern (a new hook or an extension of the existing domain-fetch path surfaces `verification_txt_value` - no new endpoint)
**Requirement**: DATV-02, DATV-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] TXT record name and value render correctly for an unverified domain
- [ ] No SSL card renders anywhere in the drawer
- [ ] `data-testid="domain-health"` section (unrelated, from `domain-health-monitoring`) untouched and still passes its existing tests
- [ ] Component tests updated: TXT table renders, no SSL text/testid present
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: `web/src/features/domains/DomainDetailDrawer.test.tsx`
**Gate**: frontend (unit)

**Commit**: `feat(web): show TXT verification instructions in domain detail drawer`

---

### T8: `DomainsTable.tsx` - SSL column removed

**What**: SSL status column (using `sslStatusColor`/`sslStatusLabel`, current lines ~91-92) deleted from the table. No replacement column added.
**Where**: `web/src/features/domains/DomainsTable.tsx`, `web/src/features/domains/DomainsTable.test.tsx`
**Depends on**: T6
**Reuses**: n/a (deletion)
**Requirement**: DATV-07

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] SSL column removed from table header and body
- [ ] Existing Status/health-risk columns (unrelated to this change) still render correctly
- [ ] Component test updated to assert no SSL column/header text present
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: `web/src/features/domains/DomainsTable.test.tsx`
**Gate**: frontend (unit)

**Commit**: `feat(web): remove SSL status column from domains list`

---

### T9: i18n - drop dead SSL keys, add TXT instruction keys (pt-BR/en parity)

**What**: `domains.detail.sslLabel` and `domains.sslStatusLabel.*` keys removed from both locale files. New keys added for the TXT instruction block (e.g. `domains.detail.txtConfigLabel`, `domains.detail.txtRecordName`, `domains.detail.txtRecordValue`, plus any copy-button label if T7 ships one) - both `pt-BR.json` and `en.json` updated together, never one without the other.
**Where**: `web/src/locales/pt-BR.json`, `web/src/locales/en.json`
**Depends on**: T7 (keys must match what the component actually references)
**Reuses**: existing locale-key naming convention under `domains.detail.*`
**Requirement**: DATV-02, DATV-06, DATV-07

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] No orphaned SSL-related key remains in either locale file
- [ ] Every new key referenced by T7/T8 exists in both locales with equivalent meaning
- [ ] `npm run i18n:check` passes (in `web/`)

**Tests**: covered by `i18n:check`, no dedicated test file
**Gate**: frontend (full, since this is the last frontend task in the phase)

**Commit**: `chore(web): update pt-BR/en locale keys for TXT domain verification`

---

### T10: `.specs/STATE.md` - new AD entry

**What**: New `AD-NNN` (next sequential number) documenting: root-domain verification switched from DNS-resolution + TLS-handshake to TXT-based ownership proof; the `ssl_status` field removed because a root domain never holds its own certificate under this product's design; explicit cross-reference to the existing `domain-health-monitoring` AD's "root domain stays pointed at whatever infrastructure the operator already chose" decision as the thing this correction now actually matches in the verification mechanism.
**Where**: `.specs/STATE.md`
**Depends on**: T1-T9 (written once the change is fully implemented, describing what shipped)
**Reuses**: existing `AD-NNN` entry format used throughout the file
**Requirement**: n/a (documentation, not a traced AC)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] New AD entry added with the *why*, not just the *what*, per `AGENTS.md` §6
- [ ] Cross-reference to the `domain-health-monitoring` AD is explicit and accurate

**Tests**: none
**Gate**: none (docs)

**Commit**: `docs(specs): add AD for domain apex TXT verification`

---

### T11: `README.md` - domain model table wording check

**What**: Confirm/adjust the `domains` row in the Domain model table (currently: "Custom hostnames the operator has registered with Vane (validated ownership, DNS target)") so it doesn't imply a DNS-target/CNAME concept for the root domain row itself post-change. Minimal wording fix only - no new feature-table entry needed (this is a correction to existing behavior, not a new user-facing feature), per `AGENTS.md` §6.
**Where**: `README.md`
**Depends on**: T1-T9
**Reuses**: existing table structure
**Requirement**: n/a (documentation)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Domain model table wording accurately reflects TXT-based ownership proof for the root domain, with no leftover CNAME/DNS-target implication for that row
- [ ] No other README section (e.g. the subdomain-attach/CertMagic sections) is touched - they remain accurate as-is

**Tests**: none
**Gate**: none (docs)

**Commit**: `docs: correct domain model description for TXT-based ownership verification`
