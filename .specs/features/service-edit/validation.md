# Service Edit Validation

**Date**: 2026-09-16 (re-verified same day after fix `f650cc3`)
**Spec**: `.specs/features/service-edit/spec.md`
**Diff range**: `b6c58c7..418502b` (8e3e488's parent → tip; 4 authored commits: `8e3e488`, `62b9a35`, `5036669`, `418502b`); fix `f650cc3` re-verified on top
**Verifier**: independent sub-agent (author ≠ verifier)
**Result**: PASS — the sole gap from the original pass (SVCEDIT-05 route-wiring coverage) is closed by `f650cc3` and re-verified against the real router (`internal/cli/routes_test.go:568-602`).

---

## Task Completion

Medium-scope feature, no formal `tasks.md` — the 4 commits stand in for the task list.

| Commit | Description | Status | Notes |
| --- | --- | --- | --- |
| `8e3e488` | `internal/db/service_repository.go` `Update` method + repo tests | ✅ Done | - |
| `62b9a35` | `internal/api/services_handler.go` `Update` handler + `PATCH /api/services/{id}` route wiring in `internal/cli/routes.go` | ✅ Done | Route gated `ownerOnly`, matches spec's Assumptions table |
| `5036669` | `web/src/features/services/hooks.ts` `useUpdateService` mutation hook | ✅ Done | - |
| `418502b` | `ServiceDetailDrawer.tsx` rename UI + i18n strings + MSW mock | ✅ Done | - |

`b6f5f73` (`docs(service-delete): add feature spec`) also falls inside the numeric commit range but is unrelated to service-edit (a different feature's spec doc) — excluded from this review.

---

## Spec-Anchored Acceptance Criteria

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| SVCEDIT-01: WHEN owner PATCHes non-empty `name` THEN `name` updates and `monitor_mode`/`slo_id`/`slo_name`/`poll_*`/`current_status`/`last_status_change_at` stay unchanged | Name changes; all other listed columns byte-identical | `internal/db/service_repository_test.go:587-596` - `if got.Name != newName {...}`, `if got.SLOID != "slo-update-1" {...}`, `if got.SLOName != "Checkout latency SLO" {...}`, `if got.CurrentStatus != "not_configured" {...}` (repo layer); `internal/api/services_handler_test.go:924-948` - equivalent checks on `serviceResponse` at the handler layer | ✅ PASS |
| SVCEDIT-02: WHEN rename succeeds THEN respond `200` with full `serviceResponse` | HTTP 200, body = `serviceResponse` shape | `internal/api/services_handler_test.go:930` - `if rec.Code != http.StatusOK {...}`, then unmarshals into `serviceResponse` and asserts fields (`:936-947`) | ✅ PASS |
| SVCEDIT-03: IF `name` empty/absent THEN `422` fixed generic body, row unmodified | 422, `invalidServiceRequestBody` (same as Create), no DB change | `internal/api/services_handler_test.go:971` - `if rec.Code != http.StatusUnprocessableEntity {...}`; `:975-980` re-`GET`s and asserts `detail.Name` unchanged. Handler: `internal/api/services_handler.go:395-398` reuses `invalidServiceRequestBody` (same const `Create` uses, `:120`) | ✅ PASS |
| SVCEDIT-04: IF `{id}` unknown THEN `404` fixed generic body (`serviceNotFoundBody`) | 404, same body as `GET` | `internal/api/services_handler_test.go:992,995` - `if rec.Code != http.StatusNotFound {...}`, `if rec.Body.String() != serviceNotFoundBody {...}`; repo layer: `internal/db/service_repository_test.go:606-609` - `errors.Is(err, ErrNotFound)` | ✅ PASS |
| SVCEDIT-05: IF requester role ≠ `owner` THEN `403`, no row change | 403, row unmodified | `internal/api/services_handler_test.go:1009` - `if rec.Code != http.StatusForbidden {...}`; `:1013-1018` re-`GET`s and asserts `detail.Name` unchanged. Route wiring, now also proven through the real router: `internal/cli/routes_test.go:568-587` (`TestAdminRouter_OperatorAndViewer_ServiceOwnerOnlyRoutes_403`) and `:592-602` (`TestAdminRouter_Owner_ServiceOwnerOnlyRoutes_PassAuthorization`), against `internal/cli/routes.go:230` `protected.With(ownerOnly).Patch(...)` | ✅ PASS (was ⚠️ gap; closed by `f650cc3`, re-verified 2026-09-16 — see Re-verification section) |
| SVCEDIT-06: WHEN rename succeeds THEN `ServiceListPage` reflects new name without full reload (cache invalidation/refetch) | List + detail react-query caches invalidated, no reload | `web/src/features/services/hooks.ts` `onSuccess` - `queryClient.invalidateQueries({queryKey:["services"]})` + `["services","detail",variables.id]`; test evidence `web/src/features/services/hooks.test.ts:143-158` - `useUpdateService renomeia e invalida a lista e o detalhe` asserts `names` (from the live `useServices` query) contains the new name after `mutateAsync`; drawer-level: `web/src/features/services/ServiceDetailDrawer.test.tsx:189-204` - `owner consegue renomear o serviço pelo drawer (SVCEDIT-06)` | ✅ PASS |

**Status**: ✅ All 6 ACs covered by tests with spec-matching assertions. The SVCEDIT-05 route-wiring coverage gap flagged in the original pass is now closed (`f650cc3`, re-verified below) — 6/6 fully PASS.

---

## Discrimination Sensor

Isolated scratch: `git worktree add <scratch> 418502b` (detached at the feature tip), mutated there, never touched the real tree. Baseline `git status --porcelain` captured before sensor work; re-checked after — real tree diverged only due to unrelated, concurrent work by another process on `.specs/features/service-delete/spec.md` and `internal/poller/manual_scheduler*.go` (not touched by this Verifier). No file this Verifier edited (all under the scratch worktree path) appears in that delta. Scratch worktree removed via `git worktree remove --force`.

| # | File:line | Description | Killed? |
| --- | --- | --- | --- |
| 1 | `internal/db/service_repository.go:283` | Flipped `if tag.RowsAffected() == 0` → `!= 0` (inverts not-found detection) | ✅ Killed — `TestServiceRepository_Update_RenamesAndLeavesEverythingElseUnchanged`, `_UnknownID_ReturnsErrNotFound`, `_SameName_IsIdempotentNoError` all failed |
| 2 | `internal/api/services_handler.go:396` | Changed empty-name branch's status from `http.StatusUnprocessableEntity` → `http.StatusOK` | ✅ Killed — `TestUpdateService_EmptyName_422NoChange` failed (`status = 200, want 422`) |
| 3 | `internal/cli/routes.go:230` | Removed `ownerOnly` from the real route wiring: `protected.With(ownerOnly).Patch(...)` → `protected.Patch(...)` | ❌ Survived at time of original pass — see **Re-verification (2026-09-16)** below: now ✅ Killed |

**Sensor depth**: lightweight (default tier), 3 mutations
**Result** (original pass, 2026-09-16 morning): 2/3 killed — **FAIL** on mutation 3. Superseded by the re-verification below: 3/3 killed after the fix.

---

## Re-verification (2026-09-16) — Fix 1 closed

**Fix commit**: `f650cc3` — "test(cli): cover services ownerOnly routes through the real router" — adds `serviceOwnerOnlyRouteCases()` (`internal/cli/routes_test.go:436-460`) covering `PATCH /api/services/{id}` and `DELETE /api/services/{id}`, exercised through the real `buildAdminRouter` by two new tests: `TestAdminRouter_OperatorAndViewer_ServiceOwnerOnlyRoutes_403` (`routes_test.go:568-587`) and `TestAdminRouter_Owner_ServiceOwnerOnlyRoutes_PassAuthorization` (`routes_test.go:592-602`).

**Test run** (disposable Postgres, container `vane-reverify-pg`, port 5435, destroyed after use — `vane-dev-pg` untouched):

```
TEST_DATABASE_URL="postgres://vane:vane@localhost:5435/vane?sslmode=disable" \
  go test -tags=integration -p 1 -run "TestAdminRouter_.*ServiceOwnerOnly" ./internal/cli/... -v
```

Result: both new tests pass — `TestAdminRouter_OperatorAndViewer_ServiceOwnerOnlyRoutes_403` (operator and viewer, PATCH + DELETE, all 403) and `TestAdminRouter_Owner_ServiceOwnerOnlyRoutes_PassAuthorization` (owner, PATCH + DELETE, neither 401 nor 403). 0 failures.

**Sensor mutation 3 re-run** — confirmed the new tests actually discriminate, not just pass by construction. In an isolated `git worktree add /tmp/vane-reverify-scratch HEAD` (never touching the real tree), reproduced the original mutation: `internal/cli/routes.go:230` `protected.With(ownerOnly).Patch("/api/services/{id}", servicesHandler.Update)` → `protected.Patch("/api/services/{id}", servicesHandler.Update)` (dropped `.With(ownerOnly)`). Re-ran the same targeted test command against the scratch worktree:

```
--- FAIL: TestAdminRouter_OperatorAndViewer_ServiceOwnerOnlyRoutes_403 (0.11s)
    --- FAIL: .../operator/PATCH_/api/services/{id}: status = 404, want 403, body = {"error":"service not found"}
    --- FAIL: .../viewer/PATCH_/api/services/{id}: status = 404, want 403, body = {"error":"service not found"}
```

(404 rather than 403 because with the role gate removed the request falls through to the handler's not-found path for the fixture's nonexistent test ID — the assertion is still correctly violated: `want 403`, `got 404`, i.e. an unauthorized-role request no longer receives a 403 through the real router.) `TestAdminRouter_Owner_ServiceOwnerOnlyRoutes_PassAuthorization` still passed, as expected (owner was never gated by the mutation). **Mutant killed** — the new tests fail exactly the way that reveals the regression, closing the gap Fix 1 identified.

Scratch worktree removed via `git worktree remove --force /tmp/vane-reverify-scratch`; `git status --porcelain` on the real tree captured before and after the sensor work — identical (unrelated pre-existing modifications to `.specs/LESSONS.md`, `.specs/lessons.json` and an untracked `node_modules/` from other concurrent work, none touched by this re-verification).

**Sensor result after fix**: 3/3 killed — **PASS**.

### Root cause of survived mutant

`internal/api/services_handler_test.go:44-46` builds its own router (`newServicesRouter`) that manually re-declares `protected.With(RequireRole(db.RoleOwner)).Patch("/api/services/{id}", handler.Update)` to test the 403 behavior — a hand-written *mirror* of `routes.go`'s wiring, not the real `buildAdminRouter`. `internal/cli/routes_test.go` has no case for `PATCH /api/services/{id}` in its route-authorization tables (its `writeRoleRouteCases`/`adminManagementRouteCases` helpers don't include it). So the real production wiring in `internal/cli/routes.go:230` is untested end-to-end: an accidental regression there (e.g. `writeRoles` instead of `ownerOnly`, or the `.With(ownerOnly)` call dropped entirely) would ship without any test catching it.

This is not a hypothetical concern in this codebase — `routes_test.go:376-378`'s own comment documents that exact failure mode already happened once for the admin-management routes ("Before this, only `internal/api/admins_test.go` exercised these routes, through a router it assembles itself rather than `buildAdminRouter`, so removing `RequireRole` from `routes.go`'s real wiring broke nothing") and was fixed by adding `adminManagementRouteCases()` against the real router. That fix was never extended to cover the new `PATCH /api/services/{id}` route this feature adds.

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code | ✅ — `Update` methods/handler/hook are each small and single-purpose |
| Surgical changes | ✅ — only the 13 files in the stated diff touched |
| No scope creep | ✅ — `monitor_mode`/`slo_id`/`poll_*` left untouched, matches spec's Out of Scope table |
| Matches patterns | ✅ — reuses `invalidServiceRequestBody`/`serviceNotFoundBody`, `ownerOnly` convention from `DELETE /api/admins/{id}`, PATCH-for-partial-update convention |
| Spec-anchored outcome check (asserted values match spec) | ✅ (see table above) |
| Per-layer Coverage Expectation met (domain 1:1 ACs; routes happy+edge+error) | ✅ — domain/handler layers fully covered; route-wiring layer gap on SVCEDIT-05 closed by `f650cc3` (see Re-verification section) |
| Every test maps to a spec requirement — no unclaimed tests | ✅ — every new test's comment cites an SVCEDIT-NN or the spec.md Edge Case |
| Documented guidelines followed | AGENTS.md §4 (generic 500s, no raw error leakage), §5 (react-query cache key/invalidation, i18n strings) — both followed |

---

## Edge Cases

- [x] `name` is only whitespace → treated as empty (422): `strings.TrimSpace(req.Name) == ""` at `internal/api/services_handler.go:395`; frontend mirrors with `nameDraft.trim()` in `ServiceDetailDrawer.tsx`'s `saveRename`, tested at `ServiceDetailDrawer.test.tsx:206-219` (`nome vazio mostra erro e não salva`)
- [x] Same name resubmitted → 200 idempotent no-op, not an error: `internal/db/service_repository_test.go:614-629` (`TestServiceRepository_Update_SameName_IsIdempotentNoError`), `internal/api/services_handler_test.go:1025-1035` (`TestUpdateService_SameName_200Idempotent`)

---

## Gate Check

- **Gate command (backend)**: `go build ./... && go vet ./... && gofmt -l <changed files>` — clean, no output
- **Gate command (backend integration, disposable Postgres on port 5435, never touching `vane-dev-pg`)**: `TEST_DATABASE_URL=... go test -tags=integration -p 1 ./...`
- **Result**: all packages passed, including full-suite regression run (not just the new tests) — `internal/api` (47s), `internal/db` (37s), `internal/cli` (9s), and 14 other packages, 0 failures
- **Targeted new-test run**: `TestServiceRepository_Update_*` (3 tests) + `TestUpdateService_*` (5 tests) — 8/8 passed
- **Gate command (frontend)**: `npx tsc -b --noEmit` (clean) + `npx vitest run src/features/services/hooks.test.ts src/features/services/ServiceDetailDrawer.test.tsx` — 23/23 passed (9 in hooks.test.ts, 14 in ServiceDetailDrawer.test.tsx)
- **Test count before feature**: not independently re-derived (no formal tasks.md baseline); new tests added by this feature: 3 (repo) + 5 (handler) + 2 (hooks) + 4 (drawer) = 14
- **Skipped tests**: none
- **Failures**: none in the real tree; the 2 failures observed only inside the discrimination-sensor scratch worktree (`TestAdminRouter_UnmatchedNonAPIPath_ReturnsEmbeddedIndexHTML`, `TestNewHTTPSServer_RootPath_ServesEmbeddedSPA`) are pre-existing and unrelated — confirmed present identically before the mutation via a `git stash`/re-run control (missing embedded frontend build artifact in the ad hoc worktree, not a service-edit regression)

---

## Fix Plans

### Fix 1: `PATCH /api/services/{id}` ownerOnly gate untested against the real router

- **Root cause**: `internal/api/services_handler_test.go` proves `RequireRole(db.RoleOwner)` middleware behavior correctly, but does so against a hand-assembled router (`newServicesRouter`) that only *mirrors* `routes.go`'s wiring rather than exercising `buildAdminRouter` itself. `internal/cli/routes_test.go`'s route-authorization tables (`writeRoleRouteCases`, `adminManagementRouteCases`) don't include `PATCH /api/services/{id}`. A regression in `routes.go:230` (e.g. swapping `ownerOnly` for `writeRoles`, or dropping the `.With(...)` entirely) would pass every existing test.
- **Fix task**: Add `PATCH /api/services/{id}` as a case in `internal/cli/routes_test.go`'s owner-only route-authorization coverage (the same pattern used for `adminManagementRouteCases()`, following the precedent documented in that file's own comment at `routes_test.go:376-378`) — assert a non-owner role gets 403 through `buildAdminRouter` itself, not the handler test's private mirror router.
- **Priority**: Major (not Blocker — the middleware itself is proven correct at the handler layer, and the current `routes.go` wiring is correct; this is a test-suite discrimination gap, not a live security defect today). **RESOLVED** 2026-09-16 by `f650cc3` — see Re-verification section.

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| SVCEDIT-01 | Implementing | ✅ Verified |
| SVCEDIT-02 | Implementing | ✅ Verified |
| SVCEDIT-03 | Implementing | ✅ Verified |
| SVCEDIT-04 | Implementing | ✅ Verified |
| SVCEDIT-05 | Implementing | ✅ Verified |
| SVCEDIT-06 | Implementing | ✅ Verified |

---

## Summary

**Overall**: ✅ PASS

**Spec-anchored check**: 6/6 ACs matched spec-defined outcomes, all fully PASS (the SVCEDIT-05 coverage gap is now closed)
**Sensor**: 3/3 mutations killed (mutation 3 re-run 2026-09-16 against `f650cc3`'s new tests — now killed; see Re-verification section)
**Gate**: all backend + frontend gates green (full-suite regression run, 0 failures)

**What works**: Repository `Update`, handler `Update`, route wiring in `routes.go` (correct as shipped, and now proven through the real router by `f650cc3`), `useUpdateService` hook with correct cache invalidation, and the drawer's inline-rename UI all behave exactly per spec.md's 6 ACs and both listed edge cases. Full backend integration suite (17 packages) and full targeted frontend suite pass with zero regressions.

**Issues found**: None outstanding. Fix 1 (the `ownerOnly` gate on `PATCH /api/services/{id}` untested against the real router) was closed by `f650cc3` and re-verified 2026-09-16 (see Re-verification section above): the new tests pass against the real wiring and were confirmed to fail when the gate is removed in an isolated scratch worktree.

**Next steps**: None required for this feature. SVCEDIT-05 is now fully Verified; overall verdict is PASS.
