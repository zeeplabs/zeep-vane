# Domain Apex TXT Verification Validation

**Date**: 2026-09-30
**Spec**: `.specs/features/domain-apex-txt-verification/spec.md`
**Diff range**: `73fbdb0..HEAD` (base `73fbdb0` = feature spec commit; HEAD = `131a55811cd5463c59f6a88c9140f83073ea1a0b`)
**Verifier**: independent sub-agent (author ≠ verifier). Read-only over the real tree; mutations ran only in a temporary `git worktree` at HEAD, discarded afterward.
**Baseline `git status --porcelain` at start**: empty. **After sensor cleanup**: empty (unchanged).

---

## Task Completion

| Task | Status | Notes |
| ---- | ------ | ----- |
| T1 Migration `0044` add `verification_token` | ✅ Done | `0044_domain_verification_token.up/down.sql` |
| T2 `DomainRepository.VerificationToken` (additive) | ✅ Done | `internal/db/domain_repository.go:84-125,138,181` |
| T3 `apexTXTVerifier` / `netApexTXTVerifier` | ✅ Done | `internal/api/domain_txt_verifier.go` |
| T4 handler wiring + drop `ssl_status` (migration `0045`) | ✅ Done | `internal/api/domains_handler.go`, `0045_drop_domain_ssl_status.*` |
| T5 `verification_txt_value` type + MSW (additive) | ✅ Done | `web/src/types/api.ts:129`, `web/src/test/msw/handlers.ts` |
| T6 drawer TXT block replaces CNAME, SSL card removed | ✅ Done | `web/src/features/domains/DomainDetailDrawer.tsx:111-131` |
| T7 SSL column removed from list | ✅ Done | `web/src/features/domains/DomainsTable.tsx:68-92` |
| T8 dead SSL helpers removed | ✅ Done | `web/src/features/domains/domainStatusMeta.ts` (no `sslStatusLabel`/`sslStatusColor`) |
| T9 drop `ssl_status` from type/fixtures/locales | ✅ Done | `web/src/types/api.ts`, locales, fixtures |
| T10 `.specs/STATE.md` AD-042 | ✅ Done | `AD-042` added, cross-references AD-039 |
| T11 `README.md` domain model row | ✅ Done | `README.md:297` |

All 11 tasks marked `[x]` with matching commits (see Commit Range).

---

## Spec-Anchored Acceptance Criteria

| ID | Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| -- | ------------------------- | -------------------- | ----------------------- | ------ |
| DATV-01 | Domain registered → unique verification token generated + persisted | non-empty, 32-hex; two domains never share a token | `internal/db/domain_repository_test.go:179` `TestDomainRepository_Create_PersistsNonEmptyVerificationToken` → `len(VerificationToken) != 32` fails at `:191`; `:199` `TestDomainRepository_Create_TwoDomains_DistinctVerificationTokens` → `a.VerificationToken == b.VerificationToken` fails at `:213`; impl `internal/db/domain_repository.go:102-122`; API surface `internal/api/domains_handler_test.go:460` `created.VerificationTxtValue == ""` fails | ✅ PASS |
| DATV-02 | Unverified domain detail view shows TXT name + value (not CNAME) | record name `_vane-verify.<hostname>`, value = stored token | `web/src/features/domains/DomainDetailDrawer.test.tsx:103-121` → `within(block).getByText("_vane-verify.txt.example.com")` (`:119`), `getByText("abc123token")` (`:120`); impl `DomainDetailDrawer.tsx:123,126` | ✅ PASS |
| DATV-03 | Verify → real TXT lookup; `verified` only on exact match | `LookupTXT("_vane-verify."+hostname)`, any value `== expectedToken` | `internal/api/domain_txt_verifier_test.go:32-47` → `TXTFound`/`TXTMatches` true and `resolver.name == "_vane-verify.example.com"` (`:44`); handler e2e `internal/api/domains_handler_test.go:683-714` → `updated.Status == "verified"` (`:699`); impl `domain_txt_verifier.go:62,67-72`, `domains_handler.go:305` | ✅ PASS |
| DATV-04 | TXT failure → `error` with distinct not-found vs mismatch copy, never CNAME/TLS | not-found and mismatch messages differ; no "CNAME"/"DNS not resolved" | `internal/api/domains_handler_test.go:745-773` → not-found: `strings.Contains(*LastError,"TXT")` (`:767`) and NOT `CNAME`/`DNS not resolved` (`:770`); `:778-806` → mismatch: `strings.Contains(*LastError,"does not match")` (`:800`), not CNAME (`:803`); impl `domains_handler.go:253-273` | ✅ PASS |
| DATV-05 | Already-`verified` domain → verify still performs a real check | verifier is called again (not short-circuited by `status`), state updated from fresh result | `internal/api/domains_handler_test.go:882-928` → `counter.calls != 2` fails (`:918`) and `second.Status != "error"` fails (`:925`) after fresh mismatch; impl `domains_handler.go:284-306` (no `status` guard before `h.verifier.Verify`) | ✅ PASS |
| DATV-06 | Domain detail view shows no SSL status card | no SSL card/badge/label rendered | `web/src/features/domains/DomainDetailDrawer.test.tsx:97-99` → `queryByText("SSL/TLS")` and `queryByText("domains.detail.sslLabel")` not in document; impl: no SSL block in `DomainDetailDrawer.tsx` | ✅ PASS |
| DATV-07 | Domains list table shows no SSL column | no SSL header or per-row SSL status cell | `web/src/features/domains/DomainsTable.test.tsx:109-111` → `queryByText("SSL")` not in document, `queryByText("Ativo")` not in row; impl `DomainsTable.tsx:68-92` | ✅ PASS |
| DATV-08 | `GET /api/domains` and `POST /api/domains/{id}/verify` responses have no `ssl_status` | no `ssl_status` key on List/Create/Verify | `internal/api/domains_handler_test.go:229` (List), `:463` (Create), `:711` (Verify) → `strings.Contains(rec.Body.String(), "ssl_status")` fails; impl `domainResponse` has no `SSLStatus` (`domains_handler.go:74-113`); frontend `web/src/types/api.ts:114-129` has no `ssl_status` | ✅ PASS |

**Status**: ✅ All 8 ACs covered, assertion values match spec-defined outcomes.

### SPEC_DEVIATION resolution checks (both hold)

- **A1 — verifier stays separate.** `git diff --name-status 73fbdb0..HEAD` shows `internal/api/domain_verifier.go` and `internal/api/status_pages_handler.go` **not modified**; `domainVerifier` still declared at `internal/api/domain_verifier.go:54` and consumed by `internal/api/status_pages_handler.go:51`. The new root-domain path is the independent `apexTXTVerifier` (`internal/api/domain_txt_verifier.go:26-31`), injected into `DomainsHandler` only (`domains_handler.go:46,64`). ✅
- **A2 — `ssl_status` fully gone.** Repo-wide sweep (`rg "ssl_status|SSLStatus|sslStatus|DomainSSLStatus"`) returns only: migration `0045` (drop/restore), historical migration `0029`, code **comments** (`web/src/types/api.ts:112`, `hooks.test.ts:60`), and tests asserting the key's **absence**. No runtime Go read, no SQL column select, no frontend type/field. ✅

---

## Discrimination Sensor

Scratch: temporary `git worktree add <tmp> HEAD` (detached at `131a558`); `web/node_modules` symlinked in only for frontend runs; all mutations applied to scratch copies; worktree removed with `git worktree remove --force`. No `git stash` used. Real-tree porcelain verified empty before and after.

| # | Mutation | File:line (scratch = HEAD) | Test run | Killed? |
| - | -------- | -------------------------- | -------- | ------- |
| a | TXT match `==` → `!=` | `internal/api/domain_txt_verifier.go:68` | `go test ./internal/api -run TestNetApexTXTVerifier` | ✅ Killed (3 tests fail: exact-match, mismatch, none-matches) |
| b | Drop `_vane-verify.` prefix from lookup name | `internal/api/domain_txt_verifier.go:62` | `go test ./internal/api -run TestNetApexTXTVerifier` | ✅ Killed (`looked up "example.com", want "_vane-verify.example.com"`) |
| c | `mapDomainVerificationResult` not-found branch → `"verified", nil` | `internal/api/domains_handler.go:264-267` | integration: `go test -tags=integration -run TestVerifyDomain_TXTNotFound_ErrorWithTXTMessage ./internal/api` | ✅ Killed (`Status = "verified", want "error"`) |
| d | Skip verifier when `domain.Status == "verified"` (DATV-05 guard) | `internal/api/domains_handler.go:305` (inserted) | integration: `go test -tags=integration -run TestVerifyDomain_AlreadyVerified_ReVerifiesAndUpdates ./internal/api` | ✅ Killed (`calls = 1, want 2`) |
| e | Stop rendering TXT value in drawer | `web/src/features/domains/DomainDetailDrawer.tsx:126` | `npx vitest run src/features/domains/DomainDetailDrawer.test.tsx` | ✅ Killed (2 tests fail: DATV-02 value + fixture token) |
| f | Re-add `SSL` header to domains table | `web/src/features/domains/DomainsTable.tsx:73` (inserted) | `npx vitest run src/features/domains/DomainsTable.test.tsx` | ✅ Killed (`expected document not to contain element, found <span>SSL</span>`) |
| g | Token generator returns constant `"fixed-token"` | `internal/db/domain_repository.go:91` body | integration: `go test -tags=integration -run TestDomainRepository_Create_(TwoDomains_DistinctVerificationTokens\|PersistsNonEmptyVerificationToken) ./internal/db` | ✅ Killed (both: `len = 11, want 32`; identical tokens) |

**Sensor depth**: full (7 targeted behavior-level mutations across all 8 ACs — exceeds the default lightweight minimum).
**Result**: 7/7 killed — PASS ✅.

---

## Interactive UAT Results

Not performed — this is a backend + admin-UI feature whose behavior is fully covered by automated tests (no human-judgment visual/UAT required; the drawer/list DOM state is asserted directly). The design's Testing Strategy explicitly scopes verification to unit/integration/frontend tests.

---

## Code Quality

| Principle | Status |
| --------- | ------ |
| Minimum code | ✅ |
| Surgical changes | ✅ (`git diff --name-status` = feature files + compile-coupled test fixtures only) |
| No scope creep | ✅ (subdomain flow `AttachDomainDrawer.tsx`/`StatusPagesHandler.VerifyDomain`/`HostPolicy`/`PUBLIC_DNS_TARGET` untouched — not in diff) |
| Matches patterns | ✅ (`mapDomainVerificationResult` switch mirrors prior shape; fake-injection seam mirrors `domainVerifier`) |
| Spec-anchored outcome check (asserted values match spec) | ✅ |
| Per-layer Coverage Expectation met (domain 1:1 ACs; routes happy+edge+error) | ✅ |
| Every test maps to a spec requirement - no unclaimed tests | ✅ (new tests carry explicit DATV/DOMVER/DHM/DSP tags) |
| Documented guidelines followed: `AGENTS.md` §3/§4/§5, `tasks.md` gate commands | ✅ |

**Minor observations (non-blocking, no fix required):**

1. `tasks.md:48` claims a plain-unit table-driven `mapDomainVerificationResult` test in `domains_handler_test.go`, but that file is `//go:build integration` — the quick gate `go test ./internal/api` does not exercise the mapping directly; it is covered by the integration-gated handler tests (`:745`/`:778`). Behavior is covered; only the stated test level differs.
2. DATV-04 copy is asserted by substring (`"TXT"`, `"does not match"`) rather than the exact full string example in `design.md:58` — acceptable, since the spec only requires "a message describing which case occurred", and the no-CNAME/TLS guard is explicitly asserted.

---

## Edge Cases

- [x] TXT record absent (resolver error / NXDOMAIN) → `TXTFound=false` → error/not-found — `domain_txt_verifier_test.go:67`, handler `:745`.
- [x] Empty answer (no values) → not-found — `domain_txt_verifier_test.go:83`.
- [x] Value mismatch (record present, wrong value) → error/mismatch — `domain_txt_verifier_test.go:51`, handler `:778`.
- [x] Multiple TXT records, exactly one matches → match (`:100`); none matches → mismatch (`:116`).
- [x] Resolver timeout → bounded 5s lookup, not-found — `domain_txt_verifier_test.go:134`, impl `domain_txt_verifier.go:59-65`.
- [x] Cooldown still applies (no fresh lookup within window) — handler `:848-875` (`calls == 1`).
- [x] Existing rows backfilled with a real token (not placeholder) — migration `0044` `DEFAULT encode(gen_random_bytes(16),'hex')` then `DROP DEFAULT`; integration gate runs migrations cleanly.
- [x] Unknown domain verify → 404 — handler `:820-830`.

---

## Gate Check

- **Gate commands** (`tasks.md` Gate Check Commands): backend build/quick/full + frontend full.
- **Backend build**: `go build ./... && gofmt -l . && go vet ./...` → exit 0, no output.
- **Backend quick**: `go test ./...` → all packages `ok`.
- **Backend integration**: `make test-integration` (disposable `vane-test-pg` on port 5433, `-p 1`, destroyed by Makefile trap) → all packages `ok` (`internal/api` 59.0s, `internal/db` 48.9s). No container left running afterward.
- **Frontend full**: `npx tsc -b --noEmit` → exit 0; `npm run test` → **98 files / 719 tests passed, 0 failed, 0 skipped**; `npm run i18n:check` → `parity OK (pt-BR, en): 872 keys each`.
- **Test count delta** (Go, within-range): +16 `func Test` added, 3 CNAME/DNS-era tests removed (`TestVerifyDomain_Success_VerifiedActive`, `TestVerifyDomain_DNSFailure_ErrorWithLastError`, `TestVerifyDomain_DNSMismatch_ErrorWithLastError`) and replaced by the TXT-specific `TestVerifyDomain_Success_Verified` / `_TXTNotFound_ErrorWithTXTMessage` / `_TXTMismatch_ErrorWithTXTMessage` — net +13, no silent weakening. Frontend suite is green at 719.
- **Skipped tests**: none.
- **Failures**: none.

---

## Fix Plans (if issues found)

None — PASS.

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| ----------- | --------------- | ---------- |
| DATV-01 | complete | ✅ Verified |
| DATV-02 | complete | ✅ Verified |
| DATV-03 | complete | ✅ Verified |
| DATV-04 | complete | ✅ Verified |
| DATV-05 | complete | ✅ Verified |
| DATV-06 | complete | ✅ Verified |
| DATV-07 | complete | ✅ Verified |
| DATV-08 | complete | ✅ Verified |

---

## Feature Commits (`git log --oneline 73fbdb0..HEAD`, oldest→newest)

1. `aff178b` feat(db): add domain verification token (T1)
2. `21d17b3` feat(db): generate verification token on domain creation (T2)
3. `34a3b1f` feat(api): add TXT-based domain ownership verifier (T3)
4. `cb31d57` feat(api): verify root domains via TXT, drop ssl_status from domain API (T4)
5. `9c82c98` feat(web): add verification TXT value field to Domain type and MSW fixtures (T5)
6. `292e66f` feat(web): show TXT verification instructions in domain detail drawer (T6)
7. `46391d3` feat(web): remove SSL status column from domains list (T7)
8. `bf1e1ea` chore(web): remove dead SSL status helpers from domainStatusMeta (T8)
9. `dbb1cf1` chore(web): drop ssl_status from Domain type, update locales for TXT verification (T9)
10. `86e452b` docs(specs): add AD for domain apex TXT verification (T10)
11. `131a558` docs: correct domain model description for TXT-based ownership verification (T11)

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 8/8 ACs matched spec outcome | 0 spec-precision gaps (2 non-blocking observations noted)
**Sensor**: 7/7 mutations killed
**Gate**: backend build + quick + integration green; frontend tsc + 719 vitest + i18n parity green

**What works**: Token generated/persisted at creation (unique, 32-hex); detail drawer shows `_vane-verify.<hostname>` + token value in place of CNAME; `Verify` performs a real `LookupTXT` with exact-match semantics and distinct not-found/mismatch errors free of CNAME/TLS wording; already-verified domains are re-checked; SSL card/column/field removed end-to-end; the shared `domainVerifier`/subdomain CNAME/TLS path is byte-identical.

**Issues found**: none.

**Next steps**: none — feature verifies clean; safe to proceed to release flow.
