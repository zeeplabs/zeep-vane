# Domain Apex TXT Verification Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/domain-apex-txt-verification/design.md`
**Status**: Approved (restructured during Execute — see SPEC_DEVIATION below)

---

> **SPEC_DEVIATION (raised by Batch A worker, resolved before T1; approved by the user):** The original Task Breakdown had two compile-coupling defects.
> 1. `db.Domain.SSLStatus` and `DomainRepository.SetVerificationResult`'s `sslStatus` parameter are consumed by `internal/api/domains_handler.go` (interface, `domainResponse`, `toDomainResponse`, `mapDomainVerificationResult`, `Verify`). Removing them in a `internal/db`-only task cannot keep `make test-integration` green.
> 2. `domainVerifier` + `domainVerificationResult` are **shared** with `StatusPagesHandler.VerifyDomain` (subdomain CNAME/TLS flow), which `spec.md` (Out of Scope, "not deferred") and `design.md` require to stay untouched. Rewriting that shared struct to TXT-only would break the subdomain verify endpoint.
>
> **Resolution:** (a) the root-domain TXT check gets its **own** `apexTXTVerifier` interface + result type; `domainVerifier`/`netDomainVerifier`/`domainVerificationResult` stay byte-identical for status pages; (b) the `ssl_status` removal is **expand/contract** across two migrations — `0044` only adds `verification_token`, and `0045` drops `ssl_status` in T4 after every Go read path is gone, so no commit leaves `develop`'s integration gate red; (c) `Where` scopes widened to include the compile-coupled files. This supersedes `design.md`'s "internals rewritten in place" line (see its Amendments section).

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
| Go repository (`DomainRepository`) | integration | `Create` persists a non-empty `VerificationToken`; `ListPaginated`/`GetByID` scans include `verification_token` and stay RLS-correct; `SetVerificationResult` drops `ssl_status` in T4 - DATV-01 | `internal/db/domain_repository_test.go` | `go test -tags=integration ./internal/db` |
| Go domain logic (`netApexTXTVerifier`) | unit | All branches: exact match, value mismatch, record absent, multiple TXT records at the name with only one matching, resolver timeout - DATV-03, DATV-04 | `internal/api/domain_txt_verifier_test.go` | `go test ./internal/api` |
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

### T1: Migration `0044` - add `domains.verification_token`

**What**: New migration adds `verification_token TEXT NOT NULL` to `domains`, backfilling existing rows with a real generated value (`DEFAULT encode(gen_random_bytes(16), 'hex')` at add-time, then `ALTER COLUMN ... DROP DEFAULT` so new rows must supply their own token from application code, not the database). This migration intentionally does **not** touch `ssl_status` — the drop is deferred to T4's second migration `0045`, after every Go read path stops selecting the column (expand/contract; see SPEC_DEVIATION above), so the integration gate stays green across commits. `.down.sql` drops `verification_token` only.
**Where**: `internal/db/migrations/0044_domain_verification_token.up.sql`, `internal/db/migrations/0044_domain_verification_token.down.sql`
**Depends on**: None
**Reuses**: existing migration file-pair convention (see `0043_domain_last_rdap_success.up.sql`/`.down.sql`)
**Requirement**: DATV-01 (prerequisite)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `.up.sql` adds `verification_token` (backfilled, non-null, no lingering `DEFAULT`); `.down.sql` drops it
- [x] `ssl_status` is deliberately left intact in this migration (dropped in T4/`0045`)
- [x] Migration numbered `0044` (next after `0043`), matches naming convention exactly
- [x] Gate check passes: `go build ./... && gofmt -l . && go vet ./...`

**Tests**: none directly (exercised by T2's integration tests)
**Gate**: build

**Commit**: `feat(db): add domain verification token`

---

### T2: `DomainRepository` - add `VerificationToken` (additive)

**What**: `Domain` struct gains `VerificationToken string`. `Create` generates a token via the existing `crypto/rand`-based helper (confirm exact call site before writing a new one) and includes it in its `INSERT`/`RETURNING`; `ListPaginated`/`GetByID`'s `SELECT` column lists add `verification_token`. `SSLStatus` and `SetVerificationResult`'s `sslStatus` parameter are deliberately left in place here — their removal, and the column drop, happen atomically in T4 once the handler stops reading them (expand/contract, see SPEC_DEVIATION).
**Where**: `internal/db/domain_repository.go`, `internal/db/domain_repository_test.go`
**Depends on**: T1
**Reuses**: existing `crypto/rand` token-generation helper (exact location confirmed at implementation time)
**Requirement**: DATV-01

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Domain.VerificationToken` populated by `Create`, never empty
- [x] `ListPaginated`/`GetByID` scans include `verification_token`
- [x] `SSLStatus`/`sslStatus` parameter intentionally untouched (removed in T4)
- [x] Integration tests added for token presence, run under RLS per `AGENTS.md` §3 (disposable container only)
- [x] Gate check passes: `make test-integration`

**Tests**: `internal/db/domain_repository_test.go`
**Gate**: full (integration)

**Commit**: `feat(db): generate verification token on domain creation`

---

### T3: new `apexTXTVerifier` - TXT lookup for root-domain ownership

**What**: New self-contained verifier for the root-domain flow in `internal/api/domain_txt_verifier.go`: `type apexTXTResult struct { TXTFound, TXTMatches bool }` and `type apexTXTVerifier interface { Verify(ctx context.Context, hostname, expectedToken string) apexTXTResult }`, implemented by `netApexTXTVerifier` performing `net.Resolver.LookupTXT(ctx, "_vane-verify."+hostname)` under a bounded 5s `context.WithTimeout` (mirrors today's `checkDNS`). `TXTMatches` is true when **any** returned value equals `expectedToken` exactly; `TXTFound` is true when the lookup returned at least one TXT value (false on NXDOMAIN/empty/timeout). The existing `domainVerifier`/`netDomainVerifier`/`domainVerificationResult` are left **byte-identical** — they remain the verifier for `StatusPagesHandler.VerifyDomain` (subdomain CNAME/TLS, out of scope). Introduce a small resolver seam (interface or injected `*net.Resolver`) so tests inject a fake, mirroring the existing fake-injection pattern.
**Where**: `internal/api/domain_txt_verifier.go`, `internal/api/domain_txt_verifier_test.go`
**Depends on**: None (pure unit; logically placed after T2 since it checks the token T2 stores)
**Reuses**: existing `net.Resolver`/context-timeout pattern from today's `checkDNS`
**Requirement**: DATV-03, DATV-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `netApexTXTVerifier.Verify` performs a real `LookupTXT` against `_vane-verify.<hostname>` and reports `TXTFound`/`TXTMatches` correctly for: exact match, value mismatch, absent record, multiple records with exactly one matching
- [x] No TCP/TLS dial exists in this file
- [x] Unit tests cover all branches above via an injected fake resolver - DATV-03, DATV-04
- [x] Existing `domainVerifier`/`netDomainVerifier`/`domainVerificationResult` are unchanged
- [x] Gate check passes: `go test ./internal/api && go build ./... && gofmt -l . && go vet ./...`

**Tests**: `internal/api/domain_txt_verifier_test.go`
**Gate**: quick

**Commit**: `feat(api): add TXT-based domain ownership verifier`

---

### T4: `domains_handler.go` + repository contract - wire TXT, drop `ssl_status` (migration `0045`)

**What**: `DomainsHandler` gains a verifier field typed `apexTXTVerifier`, constructed in `NewDomainsHandler` as `newNetApexTXTVerifier()`. `domainResponse` drops `SSLStatus` and gains a TXT-instruction field carrying the expected record value (`verification_txt_value string`, sourced from `domain.VerificationToken`; mirrors the loose `dns_target` convention, `AGENTS.md` §4). `toDomainResponse` updated. `Verify` calls the new verifier with `domain.VerificationToken` (no longer passing `h.dnsTarget`). `mapDomainVerificationResult` rewritten around `apexTXTResult`, returning `(status string, lastError *string)`. `dnsNotResolvedError`/`dnsMismatchError` replaced with TXT-specific copy — no residual "CNAME"/"TLS"/"DNS not resolved" wording reachable from this handler. Then the contract cleanup + column drop, all in this commit: `Domain.SSLStatus` removed from the struct and every SQL column list, and `SetVerificationResult` drops its `sslStatus` parameter and `ssl_status` from its `UPDATE`. A second migration pair `0045_drop_domain_ssl_status.up.sql`/`.down.sql` drops the column, with `.down.sql` re-adding `ssl_status TEXT NOT NULL DEFAULT 'pending'` matching `0029`'s original `CHECK (ssl_status IN ('pending','active','error'))`. `h.dnsTarget` and the `dns_target` API field stay — still used by the subdomain-attach flow (`AttachDomainDrawer.tsx`), out of scope. `verifyDomainCooldown` and the audit-log entry are unchanged.
**Where**: `internal/api/domains_handler.go`, `internal/api/domains_handler_test.go`, `internal/db/domain_repository.go`, `internal/db/domain_repository_test.go`, `internal/db/migrations/0045_drop_domain_ssl_status.up.sql`, `internal/db/migrations/0045_drop_domain_ssl_status.down.sql`
**Depends on**: T2, T3
**Reuses**: existing `verifyDomainCooldown`/`checkVerifyCooldown`, existing audit-log call pattern, existing `domainsPageResponse` loose-field convention
**Requirement**: DATV-01, DATV-03, DATV-04, DATV-05, DATV-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `GET /api/domains`, `POST /api/domains`, `POST /api/domains/{id}/verify` responses contain no `ssl_status` key and do contain `verification_txt_value`
- [ ] `Verify` on an already-`verified` domain still invokes the verifier (not skipped by current state) and updates `status`/`verified_at` from the fresh result, subject only to the existing cooldown
- [ ] New error copy is TXT-specific, no residual "CNAME"/"DNS not resolved" language reachable from this handler
- [ ] `Domain.SSLStatus` and the `sslStatus` parameter are gone; `0045` drops the column and its `.down.sql` restores it with the original `CHECK`
- [ ] Integration tests updated/added for all of the above, run under RLS per `AGENTS.md` §3
- [ ] Gate check passes: `make test-integration`

**Tests**: `internal/api/domains_handler_test.go`, `internal/db/domain_repository_test.go`
**Gate**: full (integration)

**Commit**: `feat(api): verify root domains via TXT, drop ssl_status from domain API`

---

### T5: `web/src/types/api.ts` + MSW - add `verification_txt_value` (additive)

**What**: `Domain` gains the new TXT-value field matching T4's API shape (`verification_txt_value: string`). `DomainSSLStatus` and `Domain.ssl_status` are **kept for now** — deleting them before their consumers (`DomainDetailDrawer`, `DomainsTable`, `domainStatusMeta`, fixtures) are gone would break `tsc` (expand/contract, SPEC_DEVIATION). MSW domain fixtures (`web/src/test/msw/handlers.ts`) gain the field so the mock mirrors the real backend response shape (`AGENTS.md` §5).
**Where**: `web/src/types/api.ts`, `web/src/test/msw/handlers.ts`
**Depends on**: T4
**Reuses**: existing `Domain` type shape/conventions, existing MSW domain fixture
**Requirement**: DATV-02, DATV-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `verification_txt_value` typed on `Domain`, matching the backend JSON key exactly
- [ ] MSW domain fixtures include the field (shape parity with T4's response)
- [ ] `npx tsc -b --noEmit` passes (in `web/`) with zero new errors

**Tests**: none (type + fixture; exercised by downstream component tests)
**Gate**: frontend (typecheck)

**Commit**: `feat(web): add verification TXT value field to Domain type and MSW fixtures`

---

### T6: `DomainDetailDrawer.tsx` - TXT record section replaces CNAME, SSL card removed

**What**: The existing `current.domain_type === "custom"` CNAME block (record name `CNAME`, value from `useDNSTarget()`) is replaced with a TXT block: record name `_vane-verify.<hostname>`, value from the new `verification_txt_value` field, using the same card/grid styling. The adjacent SSL status grid cell (`domains.detail.sslLabel`) is deleted; the "verified at" cell is regrouped to stand alone or with the domain-health section per implementation's layout judgment (no behavior change either way). A copy-to-clipboard affordance is added for the TXT value only if an existing reusable "copyable value" component is found in `web/src/components/ui/`; otherwise this task ships without one (clipboard convenience is not an acceptance criterion). `sslStatusLabel`/`sslStatusColor` are no longer called from this file (the untouched exports are removed in T8).
**Where**: `web/src/features/domains/DomainDetailDrawer.tsx`, `web/src/features/domains/DomainDetailDrawer.test.tsx`
**Depends on**: T5
**Reuses**: existing drawer card/grid layout, `useDNSTarget()`'s sibling hook pattern (the existing domain-fetch path already surfaces `verification_txt_value` - no new endpoint)
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

### T7: `DomainsTable.tsx` - SSL column removed

**What**: The SSL status column (using `sslStatusColor`/`sslStatusLabel`, current lines ~91-92) is deleted from the table header and body. No replacement column is added. The Status/health-risk columns are untouched. `sslStatusLabel`/`sslStatusColor` are no longer called from this file (their exports are removed in T8).
**Where**: `web/src/features/domains/DomainsTable.tsx`, `web/src/features/domains/DomainsTable.test.tsx`
**Depends on**: T5
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

### T8: `domainStatusMeta.ts` - remove dead SSL status helpers

**What**: `sslStatusLabel` and `sslStatusColor` exports deleted, now that T6 and T7 removed their last call sites; the corresponding cases in `domainStatusMeta.test.ts` are removed. No remaining import of either symbol anywhere in `web/src` (grep confirms zero hits).
**Where**: `web/src/features/domains/domainStatusMeta.ts`, `web/src/features/domains/domainStatusMeta.test.ts`
**Depends on**: T6, T7
**Reuses**: n/a (deletion)
**Requirement**: DATV-06, DATV-07

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `sslStatusLabel`/`sslStatusColor` removed from the module and its test file
- [ ] No remaining import of either symbol anywhere in `web/src` (grep confirms zero hits)
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: `web/src/features/domains/domainStatusMeta.test.ts`
**Gate**: frontend (unit)

**Commit**: `chore(web): remove dead SSL status helpers from domainStatusMeta`

---

### T9: drop `ssl_status` from Domain type/fixtures + locale keys (final frontend cleanup)

**What**: `DomainSSLStatus` type and `Domain.ssl_status` field removed from `web/src/types/api.ts`; every remaining fixture/test that references `ssl_status` is updated (`web/src/test/msw/handlers.ts`, `web/src/features/domains/hooks.test.ts`, `web/src/features/domains/DomainsStatusPagesPage.test.tsx`, `web/src/features/status-pages/StatusPageDetailDrawer.test.tsx`). Both locale files drop the now-dead `domains.detail.sslLabel` and `domains.sslStatusLabel.*` keys and gain the TXT-instruction keys the components reference (`domains.detail.txtConfigLabel`, `domains.detail.txtRecordName`, `domains.detail.txtRecordValue`, plus a copy-button label if T6 shipped one) — `pt-BR.json` and `en.json` updated together, never one without the other.
**Where**: `web/src/types/api.ts`, `web/src/test/msw/handlers.ts`, `web/src/features/domains/hooks.test.ts`, `web/src/features/domains/DomainsStatusPagesPage.test.tsx`, `web/src/features/status-pages/StatusPageDetailDrawer.test.tsx`, `web/src/locales/pt-BR.json`, `web/src/locales/en.json`
**Depends on**: T6, T7, T8
**Reuses**: existing `domains.detail.*` locale-key naming convention
**Requirement**: DATV-02, DATV-06, DATV-07, DATV-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] No `ssl_status`/`DomainSSLStatus` reference remains anywhere in `web/src`
- [ ] No orphaned SSL-related locale key remains in either locale file
- [ ] Every new TXT key referenced by T6 exists in both locales with equivalent meaning
- [ ] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test && npm run i18n:check`

**Tests**: covered by typecheck + `npm run test` + `i18n:check`
**Gate**: frontend (full - last frontend task in the phase)

**Commit**: `chore(web): drop ssl_status from Domain type, update locales for TXT verification`

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
