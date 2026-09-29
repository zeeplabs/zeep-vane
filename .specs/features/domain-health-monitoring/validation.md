# Domain Health Monitoring Validation

**Date**: 2026-09-28
**Spec**: `.specs/features/domain-health-monitoring/spec.md`
**Diff range**: `f389e73..HEAD` (18 commits: `0e2267d` artifacts, T1-T14, `510cf1e` lock-key fix, `5643edd` DHM-03 regression test, `f512ee3` dead-code removal)
**Verifier**: independent sub-agent (author ≠ verifier)

**Verdict**: ✅ PASS (fix→re-verify iteration 2) — the previously surviving mutant M7 is now killed by `5643edd`'s regression test. All 10 ACs have `file:line` evidence, the build gate is green, and 8/8 discrimination-sensor mutations are killed. The band-widening-renewal case remains a documented spec-precision gap (not a blocker).

---

## Task Completion

| Task | Status | Notes |
| ---- | ------ | ----- |
| T1 | ✅ Done | `0041_domain_health_monitoring.{up,down}.sql`, seven nullable columns |
| T2 | ✅ Done | `0042_domain_notification_types.{up,down}.sql` + constants/defaults |
| T3 | ✅ Done | `DomainRepository.SetHealthCheckResult` + `Domain` fields |
| T4 | ⚠️ Partial | `GET /api/domains/{id}` named in tasks.md does not exist; fields ride on `GET /api/domains` (documented SPEC_DEVIATION, see deviations below) |
| T5 | ✅ Done | `NotifyDomainExpiring`/`NotifyDomainNSDrift` + templates |
| T6 | ✅ Done | `internal/rdap/client.go` |
| T7 | ✅ Done | `DomainHealthScheduler` |
| T8 | ✅ Done | `serve.go` wiring + boot-wiring test |
| T9 | ✅ Done | Frontend `Domain` type + MSW mocks (snake_case SPEC_DEVIATION) |
| T10 | ✅ Done | `DomainDetailDrawer` health section |
| T11 | ✅ Done | `DomainsTable` at-risk badge |
| T12 | ✅ Done | Placeholder copy fix (copy-only, no test) |
| T13 | ✅ Done | README (manual review) |
| T14 | ✅ Done | `AD-039` in `.specs/STATE.md:403` |

---

## Spec-Anchored Acceptance Criteria

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| ------------------------- | -------------------- | ----------------------- | ------ |
| DHM-01: daily RDAP query per domain, expiration/registrar persisted | `expiresAt` + registrar parsed and written | `internal/rdap/client_test.go:47` - `!expiresAt.Equal(wantExpires)`; `client_test.go:50` - `*registrar != "GoDaddy.com, LLC"`; `internal/cli/domain_health_scheduler_test.go:174` - `!got.ExpiresAt.Equal(expires)`; `:177` - `*got.Registrar != registrar`; `internal/db/domain_repository_test.go:430`,`:433` | ✅ PASS |
| DHM-02: notification at 30/15/7, once per crossing | threshold per band, once only | `internal/cli/domain_health_scheduler_test.go:358-364` (all 30/15/7 + same-band cases) + `:369-378` assertions; integration `:327-331` (`ThresholdDays == expirationThreshold15`, len==1) + `:337` (still 1 after 2nd cycle); `internal/notify/service_test.go:391` (`DaysRemaining==6`,`ThresholdDays==7`); templates `internal/email/domain_health_templates_test.go:12`,`:48` | ✅ PASS |
| DHM-03: RDAP failure recorded, **no spurious alert**, cycle continues, retry next cycle | failure recorded; other domains still processed; no alert on the failed domain | recorded + continue: `internal/cli/domain_health_scheduler_test.go:276` (`RDAPLastError != nil`), `:290` (`healthy.ExpiresAt.Equal(...)` proves the cycle continued); rdap error branches `internal/rdap/client_test.go:88`,`:104`,`:122`. **no-spurious-alert**: `internal/cli/domain_health_scheduler_test.go:335` - `len(notifier.expiring) != 0` → 0 expected when RDAP fails with a preserved in-band expiry; `:343` - `got.RDAPLastError == nil`; `:346` - `ExpiresAt` preserved; `:349` - `Registrar` preserved (test `TestDomainHealthScheduler_RDAPFailureWithStoredExpiry_NoSpuriousAlert` at `:305`) | ✅ PASS |
| DHM-04: detail view shows expiration date, days remaining, registrar | all three rendered | `web/src/features/domains/DomainDetailDrawer.test.tsx:158` test; `:176` registrar `findByText("GoDaddy.com, LLC")`; `:177` `toHaveTextContent("20 dias restantes")`; API fields `internal/api/domains_handler_test.go:868` (`ExpiresAt.Unix()==expires.Unix()`). Expiration *date string* is rendered (`DomainDetailDrawer.tsx:166`) but not asserted | ✅ PASS (minor note) |
| DHM-05: first check with no baseline records current NS, no alert | `ExpectedNS` learned, drift false, no drift notification | `internal/cli/domain_health_scheduler_test.go:165` (`nsSetsEqual(got.ExpectedNS, ...)`), `:171` (`NSDriftDetected` false), `:183` (no notifications); `internal/db/domain_repository_test.go:436` (`reflect.DeepEqual(got.ExpectedNS, ns)`) | ✅ PASS |
| DHM-06: NS differing from baseline marks drift and notifies | `NSDriftDetected=true`, one notification with both sets | `internal/cli/domain_health_scheduler_test.go:220` (`NSDriftDetected` true), `:223` (len drift==1), `:226`/`:229` (ExpectedNS/CurrentNS carried); `internal/notify/service_test.go:431`,`:440`,`:443`; drawer `DomainDetailDrawer.test.tsx:183-203` | ✅ PASS |
| DHM-07: DNS lookup failure recorded, baseline never overwritten/cleared | `ExpectedNS`/`CurrentNS` survive a nil NS result | `internal/db/domain_repository_test.go:478` (`reflect.DeepEqual(got.ExpectedNS, baseline)`), `:481` (`CurrentNS` preserved); scheduler `internal/cli/domain_health_scheduler_test.go:279`,`:282` | ✅ PASS |
| DHM-08: detail view AND list indicate drift | both surfaces show the state | drawer `web/src/features/domains/DomainDetailDrawer.test.tsx:199` (`ns-drift-badge` `toHaveTextContent("Drift de NS")`), `:200`/`:201` expected vs current; list `web/src/features/domains/DomainsTable.test.tsx:190` (`data-risk="drift"`), `:209` (expiring), `:228` (healthy none), `:245` (unchecked none); API `internal/api/domains_handler_test.go:880` | ✅ PASS |
| DHM-09: every domain in `domains` included, with or without attached status page | no attached-page filter in the cycle | `internal/cli/domain_health_scheduler.go:201` - `ListPaginated(tenantCtx, page, domainHealthPageSize)` has no status-page join/filter; tests process domains with no attached page (`internal/cli/domain_health_scheduler_test.go:157`,`:268`). No test seeds a domain *with* an attached page (the harder unattached case is the one exercised) | ✅ PASS (minor note) |
| DHM-10: `AddDomainDrawer` placeholder is a root-domain example | value is `suaempresa.com` / `yourcompany.com` | `web/src/locales/pt-BR.json:148` - `"hostnamePlaceholder": "suaempresa.com"`; `web/src/locales/en.json:148` - `"hostnamePlaceholder": "yourcompany.com"` (static copy; no regression test, per T12 copy-only) | ✅ PASS (static) |

**Status**: ✅ All 10 ACs covered and matched to spec outcome. DHM-03's previously-missing no-spurious-alert branch is now asserted.

---

## Discrimination Sensor

Scratch: `git worktree add /tmp/dhm-reverify-33033 HEAD` (isolated; real tree never mutated). Baseline `git status --porcelain` before = `.specs/LESSONS.md` M, `.specs/lessons.json` M, `.specs/features/.../validation.md` ??; after `git worktree remove --force` + `prune` = identical (`diff` clean). Tests run against a disposable `vane-reverify-pg` Postgres on `:5437` (never `vane-dev-pg`); container stopped afterward.

| Mutation | File:line | Description | Killed? |
| -------- | --------- | ----------- | ------- |
| M1 | `internal/cli/domain_health_scheduler.go:326` | Removed the `threshold == previous` guard so every cycle re-alerts | ✅ Killed |
| M2 | `internal/cli/domain_health_scheduler.go:356` | Inverted empty-baseline guard (`nsDrift` returns true when no baseline) | ✅ Killed |
| M3 | `internal/rdap/client.go:124` | Flipped expiration `eventAction` match (`!=` → `==`) | ✅ Killed |
| M4 | `internal/db/domain_repository.go:236` | Dropped `COALESCE` on `current_ns` so a nil NS clears the stored set | ✅ Killed |
| M5 | `internal/db/domain_repository.go:247` | Removed the `RowsAffected()==0 → ErrNotFound` check | ✅ Killed |
| M6 | `internal/db/domain_repository.go:209` | Removed the `TenantTxFromContext` fast path (always bare `pool.Begin`) | ✅ Killed |
| M7 (re-run) | `internal/cli/domain_health_scheduler.go:256` | Weakened `rdapErrMsg == nil && expiresAt != nil` → `expiresAt != nil` (spurious alert on RDAP failure with stored expiry) | ✅ **Killed** by `internal/cli/domain_health_scheduler_test.go:335` (`expiration notifications = %d, want 0`) |
| M8 | `internal/cli/domain_health_scheduler.go:336` | Boundary off-by-one `daysRemaining <= 7` → `< 7` | ✅ Killed |

**Sensor depth**: lightweight (8 targeted mutations across the four highest-risk areas named in the brief).
**Result**: 8/8 killed — ✅ PASS.

M7 re-sensor (iteration 2): re-applied the exact previous mutant in a fresh scratch worktree. The new test `TestDomainHealthScheduler_RDAPFailureWithStoredExpiry_NoSpuriousAlert` seeds a domain with `expires_at` ~20 days out, a `last_rdap_check_at` 15 days earlier (when it was still >30 days out), and a `fakeRDAPClient` returning `rdap: timeout`; the unmutated gate yields 0 expiration notifications and a recorded `RDAPLastError`, while the weakened `expiresAt != nil` gate yields exactly 1 spurious notification and FAILs the assertion. Mutant killed; unmutated scratch passes.

---

## Code Quality

| Principle | Status |
| --------- | ------ |
| Minimum code | ✅ |
| Surgical changes | ✅ — additive columns/methods/endpoints; existing `notifyIncident` refactored into a shared `notify` without behavior change |
| No scope creep | ✅ — no unrelated files touched |
| Matches patterns | ✅ — mirrors `digest_scheduler.go`, `IncidentRepository.Create` tenant-tx pattern, `Page[T]`/`toDomainResponse` mapping, `DomainStatusTag` conventions |
| Spec-anchored outcome check (asserted values match spec) | ✅ — DHM-03 no-spurious-alert now asserted (`domain_health_scheduler_test.go:335`) |
| Per-layer Coverage Expectation met (domain 1:1 ACs; routes happy+edge+error) | ✅ — RDAP unit branches + threshold/NS unit + repository integration + scheduler integration + API integration + vitest components |
| Every test maps to a spec requirement - no unclaimed tests | ✅ (33 Go + 8 vitest new tests all trace to DHM-*/T tasks) |
| Documented guidelines followed: `AGENTS.md` §3/§5 | ✅ — integration gate via disposable container, MSW shape parity, i18n parity |
| Dead code | ✅ — `isDomainAtRisk` removed in `f512ee3`; `grep -rn isDomainAtRisk web/src` returns no references; consumed helper `domainRisk` (`domainStatusMeta.ts:69`) still used by `DomainsTable.tsx:113` |

---

## Edge Cases

- [x] RDAP failure (timeout/non-2xx/malformed/unsupported TLD): recorded, cycle continues — `internal/cli/domain_health_scheduler_test.go:276-295`, `internal/rdap/client_test.go:80-127`. "No spurious alert" sub-case now covered: `internal/cli/domain_health_scheduler_test.go:305-350` (M7 killed).
- [x] DNS failure preserving baseline: `internal/db/domain_repository_test.go:453-484`; scheduler `:279-284`.
- [x] NS drift vs no drift vs no baseline: `internal/cli/domain_health_scheduler_test.go:133-232`.
- [x] Zero domains: `internal/cli/domain_health_scheduler_test.go:114-127` (no-op, no notification).
- [x] Domain deleted mid-cycle: code treats a `SetHealthCheckResult` `ErrNotFound` as a logged per-domain failure that does not abort the cycle (`internal/cli/domain_health_scheduler.go:268`,`:176-182`); no direct test seeds the delete race, but the not-found path is covered at the repository layer (`internal/db/domain_repository_test.go:486-496`).
- [x] Threshold boundary exactly 7/15/30 days: covered indirectly (M8 killed) — same-band case at exactly 7 and 30 prevents off-by-one.
- ⚠️ Renewal extending expiry from inside a narrow band into a wider band (e.g. 5d → 20d) could fire a "15-day" alert because the band widened (`internal/cli/domain_health_scheduler.go:326-329` fires on any band change). Not defined by the spec and not tested — spec-precision gap (does not block PASS).

---

## Gate Check

- **Gate command**: `make test-integration` (backend, disposable `vane-test-pg`, removed on exit) + `cd web && npx tsc -b --noEmit && npm run test && npm run i18n:check`
- **Result**: all Go packages `ok`, **0 failed**; frontend `tsc` clean ("No errors found"), **715 passed / 0 failed** (98 files), `i18n key parity OK (pt-BR, en): 869 keys each`
- **Test count before feature**: ~1351 Go top-level tests (`-tags=integration`), 707 vitest
- **Test count after feature**: 1384 Go top-level tests, 715 vitest
- **Delta**: **+33 Go test functions** (0 deleted; +1 in `5643edd`), **+8 vitest cases** (0 deleted)
- **Skipped tests**: none
- **Failures**: none
- **Test Integrity**: no deletions, no weakened assertions detected in the diff

---

## Fix Plans (resolved)

### Fix 1: Cover DHM-03's no-spurious-alert branch (surviving mutant M7) — ✅ RESOLVED

- **Resolution commit**: `5643edd` — adds `TestDomainHealthScheduler_RDAPFailureWithStoredExpiry_NoSpuriousAlert` (`internal/cli/domain_health_scheduler_test.go:298-350`).
- **Re-sensor**: mutation re-applied → test FAILS (mutant killed); unmutated → PASS.

### Fix 2 (minor): Remove or exercise `isDomainAtRisk` — ✅ RESOLVED

- **Resolution commit**: `f512ee3` removed the unused export. `grep` confirms no remaining references; `domainRisk` (the consumed helper) is untouched, frontend `tsc` + 715 tests green.

---

## Deviations Investigated Independently

| Deviation (marker) | Verdict |
| ------------------ | ------- |
| T4: `GET /api/domains/{id}` named but no such route | **Confirmed and reasonable.** `internal/cli/routes.go:243,281,282,287` register only `POST /api/domains`, `DELETE /api/domains/{id}`, `POST /api/domains/{id}/verify`, `GET /api/domains`. The detail drawer receives its row from the list query (`web/src/features/domains/DomainDetailDrawer.test.tsx:27` mocks `GET /api/domains`); the seven fields are emitted by `toDomainResponse` (`internal/api/domains_handler.go:116-119`) on every domain serialization, and the API test reads them across pages of `GET /api/domains`. Fields reach the detail view. |
| T9: frontend fields use snake_case | **Confirmed and correct.** `web/src/types/api.ts:144-150` mirrors the backend JSON; every sibling type in that file is also snake_case, and MSW `toDomainResponse` (`web/src/test/msw/handlers.ts:486-492`) matches. A camelCase mapping layer would desync type from API. |
| T7: preserves last-known expiry/registrar on RDAP failure | **Confirmed.** `internal/cli/domain_health_scheduler.go:229` - `expiresAt, registrar = domain.ExpiresAt, domain.Registrar` on error; the persisted `registrar` also keeps the old value. The suppression of the alert on this path is now covered (`domain_health_scheduler_test.go:305-350`). |

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| ----------- | --------------- | ---------- |
| DHM-01 | implemented | ✅ Verified |
| DHM-02 | implemented | ✅ Verified |
| DHM-03 | implemented | ✅ Verified (was ❌ Needs Fix; resolved by `5643edd`) |
| DHM-04 | implemented | ✅ Verified |
| DHM-05 | implemented | ✅ Verified |
| DHM-06 | implemented | ✅ Verified |
| DHM-07 | implemented | ✅ Verified |
| DHM-08 | implemented | ✅ Verified |
| DHM-09 | implemented | ✅ Verified |
| DHM-10 | implemented | ✅ Verified (static copy, no test) |

---

## Summary

**Overall**: ✅ Ready — all ACs covered, all sensor mutants killed.

**Spec-anchored check**: 10/10 ACs matched spec outcome, 0 gaps (band-widening renewal remains a documented spec-precision gap).
**Sensor**: 8/8 mutations killed (M7 re-run and killed).
**Gate**: 0 failed (all Go packages ok; 715 frontend tests passed; tsc clean).

**What works**: RDAP client parsing and error branches (including the no-spurious-alert-on-failure branch); threshold-crossing fire-once semantics; NS baseline learning and drift detection with baseline preservation on DNS failure; tenant-scoped `SetHealthCheckResult` (RLS cross-tenant rejection proven); API serialization of the seven fields; drawer health section and list at-risk badge; wiring, leader election and lock-key uniqueness; frontend type/MSW/i18n parity.

**Issues found**: none blocking. Minor non-asserted details remain (expiration date string, the attached-status-page variant of DHM-09, and the band-widening renewal case) — documented above.

**Next steps**: None. Feature is verified; the two fix commits (`5643edd`, `f512ee3`) close the prior FAIL gaps.

---

## Post-validation addendum (2026-09-28): non-blocking gaps closed

After the PASS verdict, the non-blocking gaps it flagged were addressed (issue a follow-up, not a re-verification).

### Gap A — band-widening on renewal (spec-precision) → fixed

The `expirationAlert` doc comment already described narrowing-only semantics ("a crossing is new only when the current band is narrower than the previous one"), but the code only checked `threshold == previous`, so a widening band (a renewal pushing expiry from e.g. 5 days out to 20) fired a spurious wider-band alert. `internal/cli/domain_health_scheduler.go:323` now skips any non-narrowing band change. Covered by `internal/cli/domain_health_scheduler_test.go:417` (`renewal widening 7 to 30 does not re-alert`) and `:418` (widening 15 → 30); the new cases fail against the old condition (sensor re-run, 2/2 killed).

### Gap B — expiration date string not asserted → fixed

`DomainDetailDrawer` now exposes `data-testid="expires-at"` (`web/src/features/domains/DomainDetailDrawer.tsx:162`), asserted in `web/src/features/domains/DomainDetailDrawer.test.tsx:180` against `formatDateTime(expiresAt, "pt-BR")`.

### Gap C — DHM-09 with an attached status page → not added (intentional)

The unattached-status-page case is the one a naive implementation could get wrong (a filter/join would silently drop the root-domain case that motivated the feature); the attached case is the ordinary path and is implicitly covered. Adding it is low value and was deliberately skipped.

### Accepted deviations (no change)

- T4: tasks.md named `GET /api/domains/{id}`, which does not exist; fields ride the list response, which is what design.md specified.
- T9: frontend fields use snake_case, matching `web/src/types/api.ts`'s convention and the MSW mocks.

**Gate after this follow-up**: `make test-integration` all packages ok; frontend `tsc` clean + 715 tests passed.
