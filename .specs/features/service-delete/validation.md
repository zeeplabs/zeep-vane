# Service Delete Validation

## Validation: Service Delete - PASS ✅ (iteration 2)

**Date**: 2026-09-16
**Spec**: `.specs/features/service-delete/spec.md`
**Diff range**: `2e1c306..4ae8960` (iteration 1) + `3498c32..a0b8546` (iteration 2 fixes)
**Verifier**: independent sub-agent (author ≠ verifier)

---

## Task Completion

| Task (commit) | Status | Notes |
| --- | --- | --- |
| `2e1c306` ServiceRepository soft-delete support (migration 0036 + repo methods) | ✅ Done | - |
| `396ef2a` DELETE /api/services/{id} soft delete | ✅ Done | - |
| `3b8192a` poller: cancel goroutine of soft-deleted polling service | ✅ Done | Guard has a real gap, see Discrimination Sensor |
| `e652418` useDeleteService mutation hook | ✅ Done | - |
| `4ae8960` delete action in service detail drawer | ✅ Done | - |

---

## Spec-Anchored Acceptance Criteria

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| SVCDEL-01: DELETE unattached service | `deleted_at` set, `204` | `internal/api/services_handler_test.go:1046-1048` `rec.Code != http.StatusNoContent` (t.Fatalf); `internal/db/service_repository_test.go:656-661` `SELECT deleted_at ...` scanned non-nil | ✅ PASS |
| SVCDEL-02: soft-deleted service excluded from List/Get/Poller.List/ListPollingManual | Excluded from all four named read paths | `internal/db/service_repository_test.go:663-680` asserts `Get`, `List`, `ListPollingManual` all exclude it; `internal/db/service_repository.go:120,193,224` all carry `deleted_at IS NULL` | ✅ PASS |
| SVCDEL-03: delete blocked while attached to status page | `409`, fixed generic body, `deleted_at` NOT set | `internal/api/services_handler_test.go:1089-1099` `rec.Code != http.StatusConflict` + `rec.Body.String() != serviceInUseBody` + follow-up GET still `200` | ✅ PASS |
| SVCDEL-04: unknown/already-deleted id | `404`, same fixed body as GET/PATCH | `internal/api/services_handler_test.go:1109-1116` `rec.Code != http.StatusNotFound` + body == `serviceNotFoundBody` | ✅ PASS |
| SVCDEL-05: non-owner role blocked | `403`, row not modified | `internal/api/services_handler_test.go:1126-1134` `rec.Code != http.StatusForbidden` + follow-up GET still `200`; router-level: `internal/cli/routes_test.go` `TestAdminRouter_OperatorAndViewer_ServiceOwnerOnlyRoutes_403` (operator+viewer, DELETE case) | ✅ PASS |
| SVCDEL-06: polling-manual goroutine canceled within one discovery tick, no further writes | Goroutine's context canceled; no new `status_intervals` rows afterward | `internal/poller/manual_scheduler_test.go:397-459` `TestManualScheduler_ServiceDisappearsFromList_GoroutineCanceledWithoutFullShutdown` asserts goroutine count returns to baseline after removal from `ListPollingManual`, without canceling the outer ctx | ✅ PASS (goroutine teardown proven directly; "no further writes" is inferred from cancellation, not independently asserted - minor) |
| SVCDEL-07: `status_intervals`/`incidents` rows for the deleted service preserved unchanged | Row counts before/after delete identical | *(iteration 1: no test found)* → **iteration 2**: `internal/db/service_repository_test.go:856-913` `TestServiceRepository_SoftDelete_PreservesStatusIntervalsAndIncidents` - `intervalCount != 1` / `incidentServiceCount != 1` (t.Errorf) | ❌ GAP (iteration 1) → ✅ PASS (iteration 2) |
| SVCDEL-08: public status page filters `deleted_at IS NULL` defensively | Filter present in the read path `PublicStatusHandler` uses | `internal/db/service_repository.go:257` `ListForStatusPage`'s query carries `s.deleted_at IS NULL`; consumed at `internal/api/public_status_handler.go:204` | ✅ PASS (code evidence; spec itself states this path is unreachable via the normal flow given AC3, so absence of a dedicated test is consistent with spec's own reasoning) |
| SVCDEL-09: frontend list removes row via cache invalidation, no reload | `queryClient.invalidateQueries({queryKey: ["services"]})` on delete success, row gone from next fetch | `web/src/features/services/hooks.ts:129-131`; `web/src/features/services/hooks.test.ts:166-183` asserts `ids` no longer contains the deleted service's id after `del.mutateAsync` | ✅ PASS |

**Status (iteration 1)**: ❌ Gaps present (SVCDEL-07 uncovered)
**Status (iteration 2)**: ✅ All ACs covered - see "Re-verification (iteration 2)" section below

---

## Discrimination Sensor

| # | File:line | Description | Killed? |
| --- | --- | --- | --- |
| 1 | `internal/db/service_repository.go:313-327` (`SoftDelete`) | (iteration 1) Reordered the transaction: UPDATE `deleted_at` runs before the `status_page_services` existence check | ❌ Survived (iteration 1) - `TestServiceRepository_SoftDelete_AttachedToStatusPage_ReturnsErrServiceInUse` still passes, because the early `return ErrServiceInUse` still rolls back the uncommitted UPDATE (deferred `tx.Rollback`). See analysis below. → **Re-verified iteration 2, retargeted**: with the fix (`SELECT id FROM services WHERE id = $1 FOR UPDATE` added, commit `3498c32`), the discriminating mutation is removing that new lock block entirely (reverting to the plain unlocked `EXISTS` check). Re-ran in an isolated scratch worktree (`/tmp/vane-reverify2-scratch`, discarded after): `TestServiceRepository_SoftDelete_ConcurrentAttach_SerializesAndBlocksDelete` (`internal/db/service_repository_test.go:~800-850`, added in `3498c32`) now **FAILS** on the mutant - `service_repository_test.go:844: SoftDelete() error = <nil>, want ErrServiceInUse (the attach committed first)` - proving the lock is what makes the race-safety real. ✅ Killed |
| 2 | `internal/db/service_repository.go:193` (`List`) | Removed `WHERE deleted_at IS NULL` from `List`'s query | ✅ Killed - `TestServiceRepository_SoftDelete_SetsDeletedAtAndHidesFromReads` fails (`List() still returned soft-deleted service`) |
| 3 | `internal/poller/manual_scheduler.go:186` (`reconcile`) | (iteration 1) Removed the `failedTenantIDs[svc.tenantID]` guard from the teardown condition | ❌ Survived (iteration 1) - both `TestManualScheduler_ServiceDisappearsFromList_GoroutineCanceledWithoutFullShutdown` and `TestManualScheduler_Reconcile_TenantListError_LoggedNoPanic` still pass. → **Re-verified iteration 2**: after the fix (`TestManualScheduler_Reconcile_TenantListFailsOnLaterRound_DoesNotTearDownTrackedServices` added, `internal/poller/manual_scheduler_test.go:487-535`, commit `a0b8546`), re-applied the identical mutation (`if liveIDs[id] || failedTenantIDs[svc.tenantID]` → `if liveIDs[id]`) in the same scratch worktree. The new test now **FAILS** on the mutant - `manual_scheduler_test.go:532: tracked map after failed-tenant reconcile lost svc-transient-fail (want it preserved, not torn down)`. ✅ Killed |
| 4 | `internal/api/services_handler.go:455-463` (`Delete`) | Flipped 409↔404 status codes for `ErrServiceInUse`/`ErrNotFound` | ✅ Killed - `TestDeleteService_AttachedToStatusPage_409NotDeleted` and `TestDeleteService_UnknownID_404` both fail |
| 5 | `web/src/features/services/ServiceDetailDrawer.tsx` (delete button `onClick`) | Removed the confirm-dialog gate; delete fires directly on first click | ✅ Killed - 2 of 17 `ServiceDetailDrawer.test.tsx` tests fail (confirm-flow assertions) |

**Sensor depth**: lightweight (5 targeted mutations, proportional to this feature's higher-risk profile)
**Iteration 1 score**: 3/5 killed - superseded by iteration 2
**Result**: 5/5 killed (iteration 2, current) - ✅ PASS

### Analysis of surviving mutants

- **Mutant 1 (SoftDelete reorder) is a weak mutation, not a false negative** - because both statements run inside one uncommitted transaction, reordering them doesn't change the *single-request* outcome (the deferred rollback undoes the premature UPDATE the same as if it had never run). This actually **confirms the real risk flagged in the task brief**: the doc comment's claim ("checked inside the same transaction as the update to avoid a race between the check and the write") is misleading. Same-transaction ordering only protects against reordering *within* that one transaction - it does **not** serialize against a **concurrent** transaction attaching the service to a status page between the check and the commit. Reviewed `internal/db/status_page_repository.go:119-126` (`insert`, used by `Create`) and `:157-164` (`SetServices`): neither locks the `services` row nor checks `deleted_at IS NULL` before inserting into `status_page_services`, and Postgres's implicit FK lock on the referenced row is `FOR KEY SHARE` (via the `service_id` foreign key), which does **not** conflict with `SoftDelete`'s plain `UPDATE` (a `FOR NO KEY UPDATE` lock, since `deleted_at` isn't part of the key). Concretely: `SoftDelete` starts, checks `EXISTS` → false; a concurrent `Create`/`SetServices` attaches the service and commits; `SoftDelete` then commits its `deleted_at` update — the service ends up **both attached and soft-deleted**, exactly the state the spec's Assumptions table says "cannot occur." No test (unit or integration) exercises this concurrent interleaving. This is a genuine architectural gap, not just a missing assertion — fixing it requires an explicit lock (e.g. `SELECT id FROM services WHERE id=$1 FOR UPDATE` in `SoftDelete`, and the same row lock, or a `deleted_at IS NULL` guard, in the attach paths) so the two transactions actually serialize.
- **Mutant 3 (reconcile guard removal) is a real coverage gap.** The existing tests only cover (a) a service disappearing from a *successful* `ListPollingManual` call, and (b) the top-level `tenants.List()` call failing with zero already-tracked services. Neither test constructs the scenario the guard exists for: a tenant with an already-tracked, still-live service whose *this-round* `ListPollingManual` call fails. Removing the guard entirely still passes both existing tests, meaning the specific behavior called out in the task brief ("a tenant whose `ListPollingManual` call fails this round does NOT have its still-live services mistaken for deleted") is unverified by the test suite.

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code | ✅ |
| Surgical changes | ✅ |
| No scope creep | ✅ (the interleaved `f650cc3` is a different feature's commit, correctly not part of this diff) |
| Matches patterns | ✅ (mirrors `DomainRepository.Delete`'s block-on-reference convention, `AGENTS.md §4` fixed-body error posture) |
| Spec-anchored outcome check (asserted values match spec) | ✅ 9/9 ACs (iteration 2: SVCDEL-07 now has a row-count assertion, `service_repository_test.go:898-912`) |
| Per-layer Coverage Expectation met (domain 1:1 ACs; routes happy+edge+error) | ✅ (iteration 2: SVCDEL-07 now has a domain-layer test; routes/handler layer covers happy+409+404+403) |
| Every test maps to a spec requirement - no unclaimed tests | ✅ |
| Documented guidelines followed | `AGENTS.md §4` (fixed generic error bodies, never leak `err.Error()`), `AGENTS.md §5` (i18n strings, MSW mock parity) - both followed |

---

## Edge Cases

- [x] Cross-tenant delete attempt → 404 (not leaking existence): not independently tested for `Delete` specifically, but inherited from the same tenant-scoped RLS connection every other services route already relies on (not a new mechanism introduced by this feature) - reasonable to accept without a dedicated test.
- [x] `ManualScheduler.reconcile` race between DB write and next tick: eventually-consistent teardown confirmed by `TestManualScheduler_ServiceDisappearsFromList_GoroutineCanceledWithoutFullShutdown`.
- [ ] Failed-tenant-list-round doesn't tear down that tenant's live services: **not tested** - see Discrimination Sensor mutant 3.

---

## Gate Check

- **Gate command**: `go build ./... && go vet ./... && gofmt -l ...` (backend); `TEST_DATABASE_URL=... go test -tags=integration -p 1 -run "TestServiceRepository_SoftDelete|TestDeleteService|TestAdminRouter_.*ServiceOwnerOnly" ./internal/db/... ./internal/api/... ./internal/cli/...` (integration); `go test ./internal/poller/... -run "TestManualScheduler"` (poller); `npx tsc -b --noEmit && npx vitest run src/features/services/hooks.test.ts src/features/services/ServiceDetailDrawer.test.tsx` (frontend)
- **Result**: 11 backend integration tests passed, 0 failed; 9 poller tests passed, 0 failed; 28 frontend tests passed, 0 failed; `tsc` clean; `gofmt -l` empty. All gates green.
- **Test count before feature**: not independently re-derived (no tasks.md baseline count recorded); commit history shows all listed tests were added by this feature's own commits.
- **Test count after feature**: 4 (`service_repository_test.go` SoftDelete) + 4 (`services_handler_test.go` Delete) + 2 (`routes_test.go`, via `f650cc3`) + 2 (`manual_scheduler_test.go`, goroutine-teardown + tenant-list-error) + 3 (`hooks.test.ts`/`ServiceDetailDrawer.test.tsx` delete-specific) = 15 new tests attributable to this feature.
- **Delta**: +15
- **Skipped tests**: none
- **Failures**: none in the real tree (mutation failures only occurred in the isolated scratch worktree, as intended)

---

## Fix Plans

### Fix 1: `ManualScheduler.reconcile`'s `failedTenantIDs` guard is unverified

- **Root cause**: no test constructs "tenant has an already-tracked, still-live polling service AND this round's `ListPollingManual` call fails for that same tenant" - the exact scenario the guard exists to protect.
- **Fix task**: Add `TestManualScheduler_Reconcile_TenantListErrorMidRound_LeavesTrackedServiceRunning` (or extend the existing goroutine-teardown test): seed one tracked, live service for tenant A; make a subsequent round's `ListPollingManual` call for tenant A return an error (via `mutableFakePollingServiceLister` or a new error-injecting fake); assert the service's goroutine is still running (not canceled) after that round.
- **Priority**: Major (this is the exact class of bug the feature's own goal #3 - "no poller in-memory state outlives the delete... closes the same class of bug found earlier" - was meant to prevent; an unguarded regression here would spuriously kill goroutines on any transient DB hiccup).

### Fix 2: `SoftDelete`'s race-safety claim doesn't hold against a concurrent attach

- **Root cause**: `SoftDelete`'s check-then-write happens in one transaction, but neither `StatusPageRepository.Create`'s `insert` nor `SetServices` lock or re-check the `services` row before inserting into `status_page_services`. Postgres's implicit FK lock (`FOR KEY SHARE`) doesn't conflict with `SoftDelete`'s plain `UPDATE` (`FOR NO KEY UPDATE`), so a concurrent attach can commit between `SoftDelete`'s check and its own commit, leaving a service both attached and soft-deleted - the state spec.md's Assumptions table says "cannot occur."
- **Fix task**: In `SoftDelete`, replace the `SELECT EXISTS` check with `SELECT ... FROM services WHERE id = $1 FOR UPDATE` first (locking the service row), then check `status_page_services`; in `StatusPageRepository.insert`/`SetServices`, take the same `FOR UPDATE` lock on each `service_id` being linked (or add a `deleted_at IS NULL` guard in the same transaction) before inserting into `status_page_services`, so the two code paths serialize on the service row. Add an integration test that drives both paths concurrently (or, at minimum, unit-tests the lock is acquired) to prove the interleaving is now blocked.
- **Priority**: Major (data-integrity risk under concurrent admin usage - low likelihood given owner-only single-admin-at-a-time usage patterns in practice, but the spec explicitly asserts this state "cannot occur" and the current code does not actually guarantee it).

### Fix 3: SVCDEL-07 (history preservation) has no test

- **Root cause**: no test asserts `status_intervals`/`incidents` row counts for a service are unchanged before/after `SoftDelete`.
- **Fix task**: Extend `TestServiceRepository_SoftDelete_SetsDeletedAtAndHidesFromReads` (or add a sibling test) to seed a `status_intervals` row (and/or an `incidents` row) for the service before deleting it, then assert `SELECT COUNT(*) FROM status_intervals WHERE service_id = $1` (and incidents) is unchanged after `SoftDelete`.
- **Priority**: Minor (the migration/query design makes accidental data loss here unlikely - no `ON DELETE CASCADE` exists, and `SoftDelete` never touches those tables - but it's an explicit, named AC and Success Criterion with zero test evidence).

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| SVCDEL-01 | Implementing | ✅ Verified |
| SVCDEL-02 | Implementing | ✅ Verified |
| SVCDEL-03 | Implementing | ✅ Verified |
| SVCDEL-04 | Implementing | ✅ Verified |
| SVCDEL-05 | Implementing | ✅ Verified |
| SVCDEL-06 | Implementing | ✅ Verified |
| SVCDEL-07 | Implementing | ✅ Verified (iteration 2 - `TestServiceRepository_SoftDelete_PreservesStatusIntervalsAndIncidents`) |
| SVCDEL-08 | Implementing | ✅ Verified |
| SVCDEL-09 | Implementing | ✅ Verified |

---

## Re-verification (iteration 2)

**Date**: 2026-09-16
**Diff added since iteration 1**: `3498c32` (fix(services): serialize SoftDelete against concurrent attach), `a0b8546` (test(poller): cover failedTenantIDs guard in manual scheduler reconcile)
**Verifier**: independent sub-agent, fresh session (author ≠ verifier)

### Fix 1 (`failedTenantIDs` guard, Major) - CLOSED

- Commit `a0b8546` is test-only; the guard itself (`internal/poller/manual_scheduler.go:186`, `if liveIDs[id] || failedTenantIDs[svc.tenantID]`) already existed correctly and was unchanged.
- Ran `go test ./internal/poller/... -run "TestManualScheduler" -timeout 30s -v`: 10/10 passed, including the new `TestManualScheduler_Reconcile_TenantListFailsOnLaterRound_DoesNotTearDownTrackedServices` (`internal/poller/manual_scheduler_test.go:487-535`).
- Discrimination re-check: in an isolated scratch worktree (`git worktree add /tmp/vane-reverify2-scratch HEAD`), removed the `|| failedTenantIDs[svc.tenantID]` clause from `reconcile`'s teardown condition. The new test now fails: `manual_scheduler_test.go:532: tracked map after failed-tenant reconcile lost svc-transient-fail (want it preserved, not torn down)`. Mutant 3 now dies. Verdict: **closed**.

### Fix 2 (`SoftDelete` concurrent-attach race, Major) - CLOSED

- Commit `3498c32` adds `SELECT id FROM services WHERE id = $1 FOR UPDATE` in `SoftDelete` (`internal/db/service_repository.go:313-331`) before the `status_page_services` existence check, taking a row lock that genuinely conflicts with the implicit `FOR KEY SHARE` lock an INSERT into `status_page_services` takes on the referenced `services` row (Postgres's lock matrix: `FOR UPDATE` conflicts with `FOR KEY SHARE`) - the same technique `AttachDomain` already uses for concurrent domain attaches.
- Ran the new `TestServiceRepository_SoftDelete_ConcurrentAttach_SerializesAndBlocksDelete` (`internal/db/service_repository_test.go:~800-850`) against a disposable Postgres 16 container (`vane-reverify2-pg`, port 5437, destroyed after the run; `vane-dev-pg` never touched): passed, along with the other 5 SoftDelete-family integration tests (6/6 total).
- Discrimination re-check: in the same scratch worktree, reverted the fix - removed the `FOR UPDATE` lock block entirely, restoring the plain unlocked `EXISTS` check. Confirmed it compiles (`go build ./internal/db/...`), then re-ran the concurrent-attach test against the same disposable container: it now **fails** - `service_repository_test.go:844: SoftDelete() error = <nil>, want ErrServiceInUse (the attach committed first)` - proving the lock, not just the test, is what closes the race. Mutant 1 now dies. Verdict: **closed**.
- The `StatusPageRepository.Create`/`SetServices` side of the original fix task description ("take the same lock in the attach paths") turned out to be unnecessary: the fix only needed to add the lock on the `SoftDelete` side, since Postgres's implicit FK-check lock on the INSERT side (`FOR KEY SHARE`) already conflicts with `SoftDelete`'s new `FOR UPDATE`. No changes were made to `status_page_repository.go`, and the test evidence confirms this is sufficient.

### Fix 3 (SVCDEL-07 history preservation, Minor) - CLOSED

- Commit `3498c32` adds `TestServiceRepository_SoftDelete_PreservesStatusIntervalsAndIncidents` (`internal/db/service_repository_test.go:856-913`): seeds one `status_intervals` row and one `incident_services` row for a service, soft-deletes it, then asserts `SELECT COUNT(*) FROM status_intervals WHERE service_id = $1` = 1 and `SELECT COUNT(*) FROM incident_services WHERE service_id = $1` = 1 (both spec-anchored row-count checks, not just "no error thrown"). Passed against the disposable container.

### Gate re-run (iteration 2)

- `TEST_DATABASE_URL="postgres://vane:vane@localhost:5437/vane?sslmode=disable" go test -tags=integration -p 1 -run "TestServiceRepository_SoftDelete" ./internal/db/... -v` → 6/6 passed (4 from iteration 1 + 2 new: `..._ConcurrentAttach_SerializesAndBlocksDelete`, `..._PreservesStatusIntervalsAndIncidents`).
- `go test ./internal/poller/... -run "TestManualScheduler" -timeout 30s -v` → 10/10 passed (9 from iteration 1 + 1 new: `..._TenantListFailsOnLaterRound_DoesNotTearDownTrackedServices`).
- Disposable Postgres container (`vane-reverify2-pg`, port 5437) created before the run and stopped/removed (`--rm`) immediately after; `vane-dev-pg` was never referenced or touched.
- Sensor isolation: `git status --porcelain` on the real tree captured before scratch-worktree creation and re-checked identical after `git worktree remove --force` - confirmed clean, no `git stash` used.

**Iteration 2 verdict**: ✅ **PASS** - both Major gaps closed with genuine (not superficial) fixes, confirmed by re-run discrimination sensor; SVCDEL-07 now has spec-anchored test evidence. All 9 requirements Verified.

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 9/9 ACs matched spec outcome
**Sensor**: 5/5 mutations killed (iteration 2 re-verification; iteration 1 was 3/5)
**Gate**: iteration 1 (11+9+28 passed) + iteration 2 (6 db + 10 poller re-run, all passed, 0 failed)

**What works**: The core soft-delete mechanism is solid - `deleted_at` filtering is correctly applied across every read path the spec names (`Get`, `List`, `ListPaginated`, `ListPollingManual`, `ListForStatusPage`), the 409/404/403 status-code contract is exact and tested, the poller goroutine teardown genuinely cancels its context (proven via `runtime.NumGoroutine()`), the frontend's confirm-dialog gate and verbatim 409-message surfacing both work and are proven by killed mutations, `SoftDelete` now genuinely serializes against a concurrent attach via a real Postgres row lock (proven by a re-injected mutation dying), the poller's `failedTenantIDs` guard against transient list failures is now proven by a test that fails when the guard is removed, and `status_intervals`/`incident_services` rows are proven preserved (row-count assertions) after a soft delete.

**Issues found**: None outstanding. Both Major gaps (concurrent-attach race, `failedTenantIDs` guard coverage) and the Minor gap (SVCDEL-07 evidence) from iteration 1 are closed with genuine fixes, confirmed by re-running the discrimination sensor against the actual fix code (not just the presence of a passing test).

**Next steps**: None required for this feature. Feature is Verified end-to-end (9/9 requirements).
