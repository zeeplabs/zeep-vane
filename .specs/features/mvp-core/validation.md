# MVP Core (Zeep Vane) Validation

**Date**: 2026-08-19
**Spec**: `.specs/features/mvp-core/spec.md`
**Diff range**: `dc7035e..9efdcef` (full feature, 91 files changed across the branch, including all three fix commits)
**Verifier**: independent sub-agent (author ≠ verifier), iteration 3 of 3 (final before mandatory escalation)

---

## Task Completion

All 40 tasks (T1-T40) plus all three fix commits complete. No blocked/partial tasks found.

| Task range | Status | Notes |
| --- | --- | --- |
| T1-T31 (Foundation through Domains/TLS) | ✅ Done | Unchanged since iteration 1 and 2, re-confirmed via gate |
| T32-T35, T40 (Public status page, incidents) | ✅ Done, reachable | Iteration-2 Gap 1 (wiring) confirmed still closed |
| Fix 700d0d6 (SP-15 scoping, services + incidents SQL) | ✅ Done | SQL unchanged since iteration 2, re-verified by direct read |
| Fix 2d25be5 (wire HTTPS listener) | ✅ Done | Unchanged since iteration 2, re-verified by direct read |
| Fix 9efdcef (disjoint-incidents test) | ✅ Done, closes iteration-2's only open gap | See below |

---

## Gap Verification (from iteration 2) — Both CLOSED

### Gap 1 (wiring) — re-confirmed unchanged
`internal/cli/serve.go` diff between iteration 2 (`2d25be5`) and this commit (`9efdcef`) is empty — only `internal/cli/serve_test.go` and `tasks.md` changed. `newHTTPSServer` still builds `router.HostRouter(statusPages, http.HandlerFunc(publicHandler.Get))` and `RunE` still starts it via `httpsSrv.ListenAndServeTLS("", "")`. No regression possible since the file wasn't touched.

### Gap 2 (SP-15 scoping, incident-side test coverage) — CLOSED, independently killed myself
Read `internal/db/incident_repository.go:208-230` (`ListPublicForStatusPage`) directly and diffed it against the iteration-2 commit (`700d0d6..9efdcef`): **zero changes** to `incident_repository.go`, `service_repository.go`, `host_router.go`, or `public_status_handler.go`. The only production-code wiring change since iteration 2 was already reviewed (Gap 1). This confirms the author's manual revert-and-restore claim did not leave the scoping SQL altered — the JOIN/WHERE (`JOIN incident_services isv ... JOIN status_page_services sps ... WHERE sps.status_page_id = $1`) is exactly as it was when originally implemented.

Read the new test `TestNewHTTPSServer_TwoPublishedStatusPages_ReturnDisjointIncidents` (`internal/cli/serve_test.go:258-288`). It creates two published status pages, each linked to a disjoint service, each service linked to its own incident (`createServeTestIncident`, `serve_test.go:153-165`), and asserts actual set membership — not just "no error":

```go
if !containsIncidentTitle(bodyA.Incidents.Active, titleA) { t.Errorf(...) }  // A must contain its own incident
if containsIncidentTitle(bodyA.Incidents.Active, titleB) { t.Errorf(...) }   // A must NOT contain B's incident
```

This is a precise disjoint-set assertion, mirroring the already-verified services test.

**I did not trust the author's manual claim — I independently ran my own discrimination sensor (see below) and killed the mutant myself, in an isolated scratch, before accepting this as closed.**

---

## Spec-Anchored Acceptance Criteria

### SP-15 area (this iteration's focus)

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| SP-15 (services) two status pages → disjoint service lists | Page A's response excludes B's services and vice versa | `internal/cli/serve_test.go:207-248` `TestNewHTTPSServer_TwoPublishedStatusPages_ReturnDisjointServices` — `containsServiceName` assertions both directions | ✅ PASS |
| SP-15 (incidents) two status pages → disjoint incident lists | Page A's response excludes B's incidents and vice versa | `internal/cli/serve_test.go:258-288` `TestNewHTTPSServer_TwoPublishedStatusPages_ReturnDisjointIncidents` — `containsIncidentTitle` assertions both directions | ✅ PASS (newly closed) |
| Unregistered host → 404 | 404 status | `internal/cli/serve_test.go:294-316` `TestNewHTTPSServer_UnregisteredHost_404` — `resp.StatusCode != http.StatusNotFound` | ✅ PASS |

### Broad sweep — 12 ACs sampled across all 7 phases (auth, Datadog, poller, domains/TLS, public status, incidents, P3)

| # | Criterion (spec ID) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- | --- |
| 1 | SP-02 invalid Datadog key | 422, key not persisted | `internal/api/integrations_handler_test.go:134` `TestConnectDatadog_InvalidCredentials_422NothingSaved` — `rec.Code==422`, row lookup `found=false` | ✅ PASS |
| 2 | SP-03 service↔SLO link persisted | Link saved | `internal/api/services_handler_test.go:84` `TestCreateService_ValidRequest_201SavesSLOLink` — `201`, `slo_id` matches | ✅ PASS |
| 3 | SP-05 SLO fetch failure → retry w/ backoff, structured log | Retries before marking failed; failure logged | `internal/poller/retry_test.go:68,81`, `internal/poller/poller_test.go:137` — 3 attempts on timeout, `LastError` set | ⚠️ Spec-precision gap: retry behavior proven; no test asserts the log's structured fields specifically |
| 4 | SP-08 poll every 2 min via scheduled job, public page never calls Datadog directly | Ticker-driven poll; handler never imports Datadog client | `internal/poller/poller.go:74-82` ticker; `internal/api/public_status_handler.go` has no Datadog import | ⚠️ Spec-precision gap: architectural guarantee (no import), not a test that actively proves zero network calls |
| 5 | SP-09 Datadog failure → cached status + timestamp + admin notified | Public page keeps last snapshot; admin sees invalid status + reason | `internal/api/public_status_handler_test.go:177`, `internal/api/integrations_handler_test.go:225` | ✅ PASS |
| 6 | SP-11/12 domain+subdomain → TLS issuance → published/HTTPS | State becomes "published" on cert obtained | `internal/tls/manager_integration_test.go:74` `TestOnEvent_CertObtained_MarksStatusPagePublished` — `state=="published"` | ✅ PASS |
| 7 | SP-13 cert issuance failure → "pendente de publicação" + reason shown | Status page not published, reason visible | `internal/tls/manager_integration_test.go:103` `TestOnEvent_CertFailed_MarksStatusPageTLSFailedWithReason` — `state=="tls_failed"`, `tls_last_error` set | ⚠️ Spec-precision gap: code's state label is `tls_failed`, not literally "pendente de publicação" from spec text; behavior (unpublished + reason surfaced) matches intent — label wording should be confirmed with product, not a functional gap |
| 8 | SP-16 incident linked to services → published on public page | Incident appears in public "active" list | `internal/api/incidents_handler_test.go:81`, `internal/api/public_status_handler_test.go:310` | ✅ PASS |
| 9 | SP-21/22 valid login → session; invalid → identical response (no enumeration) | Byte-identical response, wrong-password vs nonexistent-email | `internal/api/auth_handler_test.go:96,133` | ✅ PASS |
| 10 | SP-24 expired/used reset token rejected | 401, generic body | `internal/api/password_reset_handler_test.go:152,195` | ✅ PASS |
| 11 | Edge case: service w/o SLO → "not configured" admin, omitted public | Omitted from public services list | `internal/api/public_status_handler_test.go:230` `TestPublicStatusGet_NotConfiguredService_HiddenValidServiceShown` | ✅ PASS |
| 12 | P3 SP-26 90-day uptime graph | Out of MVP scope per traceability (`Pending`) | No occurrences of "uptime" anywhere in `internal/` | ✅ Confirmed correctly unimplemented, no partial/broken code |

**Status**: 9/9 SP-15-area assertions PASS (all 3 tested), 9/12 broader-sample ACs PASS, 3/12 flagged as pre-existing spec-precision gaps (not regressions, not introduced by this iteration's fix, not blocking — items 3, 4, 7 above). No AC failed outright.

---

## Discrimination Sensor

Ran independently in my own isolated `git worktree` (`git worktree add <scratch> HEAD`, `HEAD` = `9efdcef`) — did not reuse or trust the author's manual claim. Baseline `git status --porcelain` on the real tree before the sensor: `?? .specs/features/mvp-core/validation.md` (the pre-existing iteration-2 report, about to be overwritten by this report — not a sensor artifact). Confirmed byte-identical after cleanup.

| # | File:line | Description | Killed? |
| --- | --- | --- | --- |
| 1 | `internal/db/incident_repository.go:208-217` (scratch copy) | Removed `JOIN incident_services` / `JOIN status_page_services` from `ListPublicForStatusPage`, replacing the scoping `WHERE` with a no-op predicate that keeps the query valid but returns all incidents globally | ✅ Killed — `TestNewHTTPSServer_TwoPublishedStatusPages_ReturnDisjointIncidents` FAILED on both cross-contamination assertions (`serve_test.go:278`, `serve_test.go:286`) |
| 2 | `internal/db/service_repository.go:76-82` (scratch copy) | Same technique applied to `ListForStatusPage` — re-verifying iteration 2's mutation still holds | ✅ Killed — `TestNewHTTPSServer_TwoPublishedStatusPages_ReturnDisjointServices` FAILED (`serve_test.go:238`, `serve_test.go:246`) |
| 3 | `internal/router/host_router.go:59-60` (scratch copy) | Removed `WithStatusPageID` context attachment — `publicHandler.ServeHTTP(w, r)` instead of `r.WithContext(ctx)` — re-verifying iteration 2's mutation still holds | ✅ Killed — both `TestNewHTTPSServer_TwoPublishedStatusPages_ReturnDisjointServices` and `...ReturnDisjointIncidents` FAILED with 500 (missing-context guard in `public_status_handler.go`) |

**Sensor depth**: lightweight (3 targeted mutations, standard-feature tier — SP-15 scoping is data-isolation-relevant but not a P0 payment/auth critical path)
**Result**: 3/3 killed → sensor PASS

Each mutation was tested individually and reverted (`git checkout --`) before the next was applied — never stacked. Scratch worktree removed (`git worktree remove --force`) after all three mutations were exercised. Real tree's `git status --porcelain` confirmed unchanged (only the untracked validation.md, both before and after) throughout the sensor run.

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code | ✅ fix commit 9efdcef touches 2 files (`serve_test.go`, `tasks.md`), no production code changed |
| Surgical changes | ✅ |
| No scope creep | ✅ |
| Matches patterns | ✅ new test mirrors the existing services-disjoint test's structure and naming |
| Spec-anchored outcome check (asserted values match spec) | ✅ SP-15 fully precise on both services and incidents; 3 pre-existing minor spec-precision gaps noted above, none introduced by this fix |
| Per-layer Coverage Expectation met (domain 1:1 ACs; routes happy+edge+error) | ✅ SP-15 now has 1:1 coverage on both scoped repository methods |
| Every test maps to a spec requirement - no unclaimed tests | ✅ |
| Documented guidelines followed | tasks.md Test Coverage Matrix and Gate Check Commands — followed as specified |

---

## Edge Cases

- [x] Duplicate root domain rejected (409) — `internal/api/domains_handler_test.go:102`
- [x] Poller delay reflected in public timestamp, never faked "now" — `internal/api/public_status_handler_test.go:177`
- [x] Last-write-wins on concurrent status page edits — no optimistic lock added anywhere, consistent with spec's explicit "sem lock otimista"
- [x] Service with no SLO shown "not configured" in admin, omitted from public page — `internal/api/public_status_handler_test.go:230`
- [x] Two status pages with disjoint **services** — `internal/cli/serve_test.go:207-248`
- [x] Two status pages with disjoint **incidents** — `internal/cli/serve_test.go:258-288` (closed this iteration)

---

## Gate Check

- **Gate command**: `go build ./... && gofmt -l . && go test ./... -tags=integration && go vet ./...`
- **Result**: build ✅ (exit 0), gofmt ✅ (0 files need formatting), vet ✅ (exit 0, no findings), tests: **100 passed, 0 failed, 0 skipped** (fresh run, `-count=1`, against `TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable`, the running `zeep-vane-test-pg` container)
- **Test count before this iteration's fix**: 99
- **Test count after**: 100
- **Delta**: +1 (`TestNewHTTPSServer_TwoPublishedStatusPages_ReturnDisjointIncidents`)
- **Skipped tests**: none
- **Failures**: none

---

## Fix Plans

None required for a PASS verdict. Three pre-existing spec-precision gaps are flagged for product/spec follow-up (not code fixes, not blocking):

1. SP-05 "log estruturado" — retry behavior is proven, but no test asserts the structured log's specific fields. Low priority; behavior works, only the log-shape assertion is missing.
2. SP-08 "nunca chamar Datadog diretamente a partir de uma requisição da página pública" — guaranteed today by the handler's import graph (it never imports the Datadog client), not by an active test. Low priority; would only regress if someone later wired a direct call into the handler package.
3. SP-13 "pendente de publicação" — the implemented state is named `tls_failed`, not a literal "pending" label. Functionally equivalent (unpublished + reason shown to admin) but worth a one-line confirmation with product that the UX copy matches intent.

None of these were introduced by this iteration's fix and none affect the SP-15 gap this iteration was scoped to close.

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| SP-01 through SP-14, SP-16 through SP-25 | ✅ Verified (iteration 1/2) | ✅ Verified (re-confirmed, no regression) |
| SP-15 (services) | ✅ Verified (iteration 2) | ✅ Verified (re-confirmed) |
| SP-15 (incidents) | ⚠️ Structurally correct, test coverage gap (iteration 2) | ✅ Verified (test added, mutant independently killed) |
| SP-26 (P3) | Pending (out of MVP scope) | Pending (unchanged, correctly unimplemented) |

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: SP-15 fully closed on both services and incidents (3/3 assertions PASS); 9/12 broader-sample ACs matched their spec-defined outcome exactly; 3/12 flagged as pre-existing, non-blocking spec-precision gaps (not regressions, not introduced by this fix)

**Sensor**: 3/3 mutations independently injected and killed by this Verifier (not the author's claim) — incident scoping, service scoping, and context-wiring mutations all correctly caught by the test suite

**Gate**: 100 passed, 0 failed, 0 skipped; build/gofmt/vet clean

**What works**: All three rounds of gaps (dead-code wiring, services scoping, incidents scoping) are genuinely closed, each independently re-verified by direct code reading and my own discrimination sensor pass — not by trusting prior reports or author claims. The full P1-P2 surface (Datadog connection, SLO status mapping, polling, domains/TLS, incident CRUD/timeline, auth/reset) shows no regression across a 12-AC sample spanning all 7 phases.

**Issues found**: None blocking. Three minor spec-precision gaps noted above for product follow-up, none introduced by this iteration.

**Next steps**: Feature is verified. Recommend closing out mvp-core's Design→Verified traceability for SP-01 through SP-25; SP-26 (P3) remains correctly out of scope for this MVP per spec's Success Criteria.
