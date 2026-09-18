# provider-disconnect Validation

**Date**: 2026-09-17
**Spec**: `.specs/features/provider-disconnect/spec.md`
**Diff range**: `627aec4..HEAD` (path-scoped per the caveat in the task brief - T6's actual code changes landed in `40c06a2`, an unrelated commit, due to a `git stash` collision between concurrent agents sharing this working directory; verified by path, not by author-intent commit boundary)
**Verifier**: independent sub-agent (author != verifier)

---

## Task Completion

| Task | Status  | Notes |
| ---- | ------- | ----- |
| T1   | Done    | `internal/db/email_provider_repository.go:187` `DeleteProvider` |
| T2   | Done    | `internal/db/llm_provider_repository.go` `DeleteProvider` (mirrors T1) |
| T3   | Done    | `internal/email/service.go:152` `Service.Disconnect` |
| T4   | Done    | `internal/llm/service.go:241` `Service.Disconnect` (mirrors T3) |
| T5   | Done    | `internal/api/email_providers_handler.go:174` `Disconnect` handler |
| T6   | Done    | `internal/cli/routes.go:242` route wired; audit integration test present. Code landed via commit `40c06a2` due to a documented `git stash` collision between concurrent agents, not `410978f` - confirmed by path diff, treated as in-scope per the task brief's explicit caveat |
| T7   | Done    | `internal/api/llm_providers_handler.go:210` `Disconnect` handler (mirrors T5) |
| T8   | Done    | `internal/cli/routes.go:248` route wired; audit integration test present |
| T9   | Done    | Backend build/vet/gofmt/test/integration gate re-run independently below - matches T9's documented pre-existing failures exactly |
| T10  | Done    | `web/src/features/email-providers/hooks.ts:84` `useDisconnectEmailProvider` |
| T11  | Done    | `web/src/features/email-providers/EmailProvidersPage.tsx` disconnect button + dialog |
| T12  | Done    | `web/src/features/settings/hooks.ts:147` `useDisconnectLLMProvider` |
| T13  | Done    | `web/src/features/settings/AISettings.tsx` disconnect button + dialog (frontend gate green: 97/97 files, 682/682 tests, matching T13's documented count) |

All 13 tasks show `[x]` in `tasks.md` and are backed by code found at the paths above.

---

## Spec-Anchored Acceptance Criteria

### P1: Disconnect an email provider

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: DELETE for connected provider -> delete row + 204 | row removed from `email_providers`, status 204 | `internal/db/email_provider_repository_test.go:203-219` `TestEmailProviderRepository_DeleteProvider_ExistingRow_RemovesIt` (`repo.Get` after delete returns `ErrNotFound`); `internal/api/email_providers_handler_test.go:386-402` `TestDisconnect_ConnectedProvider_204AndCapturesRowBeforeDelete` (`rec.Code != http.StatusNoContent`) | ✅ PASS |
| AC2: active provider disconnected -> `active_provider` cleared to NULL via FK | `GetActiveProvider()` returns `""` after delete | `internal/db/email_provider_repository_test.go:230-252` `TestEmailProviderRepository_DeleteProvider_ActiveProvider_ClearsActiveProviderViaFK` (`active != ""` check); `internal/email/service_test.go:916-933` `TestDisconnect_ActiveProvider_ClearsActiveProvider` (`store.activeProvider != ""`) | ✅ PASS |
| AC3: non-active provider disconnected -> `active_provider` unchanged | active provider stays the same, not cleared | No dedicated test asserts "unchanged when the *other* provider was deleted" (all existing tests disconnect the *same* provider they activated) | ⚠️ Spec-precision gap - AC3 has no direct positive-case test; behavior is implied correct by the FK semantics (only the deleted row's own activation is cleared) but not independently asserted |
| AC4: unknown provider name -> 404, unchanged body | 404 with `unknownEmailProviderBody` | `internal/api/email_providers_handler_test.go:363-376` `TestDisconnect_UnknownProvider_404` (`rec.Code != http.StatusNotFound`, 0 Disconnect calls) | ✅ PASS |
| AC5: recognized but never connected -> 204, no error | 204 No Content, idempotent | `internal/db/email_provider_repository_test.go:221-228` `TestEmailProviderRepository_DeleteProvider_NeverConnected_NoError`; `internal/email/service_test.go:937-944` `TestDisconnect_NeverConnected_NoError`; `internal/api/email_providers_handler_test.go:409-422` `TestDisconnect_NeverConnected_204Idempotent` (`rec.Code != http.StatusNoContent`) | ✅ PASS |
| AC6: audit entry `email_provider_disconnected` recorded | exactly one `admin_audit_log` row, action `email_provider_disconnected`, target_label = provider display name | `internal/api/email_providers_audit_integration_test.go:149-184` `TestDisconnectEmailProvider_ValidRequest_RecordsEmailProviderDisconnectedAudit` (`auditRowCount != 1`, `gotTargetLabel != "SendGrid"`) | ✅ PASS |
| AC7: viewer role -> 403 | 403 Forbidden | No per-handler viewer test for Disconnect; covered generically per T5's `Done when` note (`internal/api/middleware_role_test.go`'s `RequireRole` tests) - same posture as Connect/Activate, neither of which has a per-handler viewer test either | ⚠️ Spec-precision gap (pre-existing convention, not a regression) - AC7 relies entirely on the generic `RequireRole` middleware test, never a Disconnect-specific 403 assertion |

### P1: Disconnect an LLM provider

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: DELETE for connected provider -> delete row + 204 | row removed, 204 | `internal/llm/service_test.go:386-407` `TestDisconnect_ConnectedProvider_RemovesRow`; `internal/api/llm_providers_handler_test.go:441-457` `TestLLMDisconnect_ConnectedProvider_204AndCapturesRowBeforeDelete` (`rec.Code != http.StatusNoContent`) | ✅ PASS |
| AC2: active provider disconnected -> `active_provider` cleared via FK | cleared to NULL | `internal/llm/service_test.go:409-429` `TestDisconnect_ActiveProvider_ClearsActiveProvider` | ✅ PASS |
| AC3: unknown provider -> 404 | 404 with `unknownLLMProviderBody` | `internal/api/llm_providers_handler_test.go:419-434` `TestLLMDisconnect_UnknownProvider_404` (`rec.Code != http.StatusNotFound`) | ✅ PASS |
| AC4: recognized but never connected -> 204 idempotent | 204, no error | `internal/llm/service_test.go:431-438` `TestDisconnect_NeverConnected_NoError`; `internal/api/llm_providers_handler_test.go:462-478` `TestLLMDisconnect_NeverConnected_204Idempotent` | ✅ PASS |
| AC5: audit entry `llm_provider_disconnected` recorded | one `admin_audit_log` row, action `llm_provider_disconnected`, target_label `OpenAI` | `internal/api/llm_providers_audit_integration_test.go:140-172` `TestDisconnectLLMProvider_ValidRequest_RecordsLLMProviderDisconnectedAudit` (`gotDisconnectTargetLabel != "OpenAI"`) | ✅ PASS |
| AC6: viewer role -> 403 | 403 | Same generic `RequireRole` coverage as email's AC7, no Disconnect-specific test | ⚠️ Spec-precision gap (same pre-existing convention) |

### P1: Disconnect button with confirmation in the admin UI

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: click Disconnect -> confirmation dialog naming provider, no API call | dialog shown, provider name visible, no request yet | `web/src/features/email-providers/EmailProvidersPage.test.tsx:221-237` (`within(dialog).getByText(/SendGrid/)`, `card.textContent` still "Conectado"); `web/src/features/settings/AISettings.test.tsx` mirrors (per T13) | ✅ PASS |
| AC2: confirm -> DELETE called, list refreshed | mutation fires, row disappears via `invalidateQueries` | `EmailProvidersPage.test.tsx:254-267` (`within(card).getByText("Não conectado")` after confirm) | ✅ PASS |
| AC3: cancel -> no API call, provider still connected | dialog closes, no request, row unchanged | `EmailProvidersPage.test.tsx:239-252` (dialog gone, `within(card).getByText("Conectado")`) | ✅ PASS |
| AC4: DELETE fails -> error message shown, list unchanged | error surfaced, provider stays listed | `EmailProvidersPage.test.tsx:289-306` (`within(card).findByRole("alert")` contains `/erro inesperado/`, `Conectado` still present) | ✅ PASS |
| AC5: button disabled while mutation pending | disabled attribute set during in-flight request | `EmailProvidersPage.test.tsx:269-287` (`await waitFor(() => expect(confirmButton).toBeDisabled())`) | ✅ PASS |

**Status**: ❌ 19/21 criteria fully spec-anchored, ✅ PASS; 2 flagged **⚠️ Spec-precision gap** (email AC3 "unchanged when a different provider is disconnected" has no direct positive test; both viewer-403 ACs rely on generic middleware coverage rather than a Disconnect-specific test) - neither gap is a functional defect, both are pre-existing test-design conventions this feature inherited rather than introduced.

---

## Discrimination Sensor

Ran in an isolated `git worktree add <scratch> HEAD` (never `git stash`); baseline `git status --porcelain` captured before mutation, confirmed identical after `git worktree remove --force`.

| Mutation | File:line | Description | Killed? |
| -------- | --------- | ------------ | ------- |
| 1 | `internal/llm/service.go:241` (worktree copy) | Flipped idempotent-delete: `Disconnect` now calls `s.repo.Get` first and returns its error (never-connected -> error, not success) | ✅ Killed - `TestDisconnect_NeverConnected_NoError` (`internal/llm/service_test.go:437`) failed: `got "llm: provider record not found", want nil` |
| 2 | `internal/api/email_providers_handler.go:197` (worktree copy) | Changed success status `http.StatusNoContent` (204) -> `http.StatusOK` (200) | ✅ Killed - `TestDisconnect_ConnectedProvider_204AndCapturesRowBeforeDelete` and `TestDisconnect_NeverConnected_204Idempotent` both failed: `status = 200, want 204` |
| 3 | `internal/api/llm_providers_handler.go` (worktree copy) | Removed the `h.audit.Record(...)` call from `LLMProvidersHandler.Disconnect` entirely | ✅ Killed - `TestDisconnectLLMProvider_ValidRequest_RecordsLLMProviderDisconnectedAudit` (real-DB integration test, run against a disposable container on port 5436) failed: `admin_audit_log rows with action=llm_provider_disconnected = 0, want exactly 1` |

**Sensor depth**: lightweight (default tier, 3 targeted mutations)
**Result**: 3/3 killed - PASS ✅

Mutation 3 required a real database because no unit-level (httptest, no-DB) test asserts audit-record insertion for the LLM Disconnect handler - only the real-DB integration test does, matching the same split Connect/Activate already use (documented in T5/T7's `Done when`). A disposable container (`vane-sensor-pg`, port 5436, stopped and auto-removed after) was used, never `vane-dev-pg`.

---

## Code Quality

| Principle | Status |
| --- | --- |
| No features beyond what was asked | ✅ - only `DeleteProvider`/`Disconnect`/route/hook/UI additions, nothing extra |
| No abstractions for single-use code | ✅ |
| No unnecessary "flexibility" added | ✅ |
| Only touched files required for task | ✅ - scope matches the task brief's file list exactly (the `admins_test.go` 7-line change and `degraded-interval-analysis` files are correctly out of scope and were not touched by this feature's own commits) |
| Didn't "improve" unrelated code | ✅ |
| Matches existing patterns/style | ✅ - Disconnect mirrors Connect/Activate's structure (unknown-provider 404, "capture row before mutate" from `ServicesHandler.Delete`, same `writeRoles` gating, same dialog/mutation-hook pattern as Activate) |
| Would senior engineer approve? | ✅ |
| Tests map to acceptance criteria and are non-shallow (spot-check: email story) | ✅ - spot-checked `email_providers_handler_test.go` and `EmailProvidersPage.test.tsx`; every test named after and asserting the exact PROVDISC criterion it claims |
| Spec-anchored outcome check | ✅ 19/21 direct; 2 spec-precision gaps flagged (not silently passed) |
| Per-layer Coverage Expectation met | ✅ - repository/service have 1:1 branch coverage (happy, active-clear, never-connected); handler covers happy/unknown/idempotent/500; router wiring covered by real-route audit integration test |
| Every test in scope maps to a spec AC, listed edge case, or Done-when criterion | ✅ - no unclaimed tests found in the Disconnect-specific additions |
| Documented project quality/testing guidelines followed | ✅ - `AGENTS.md` §3 (disposable Postgres, never `vane-dev-pg`, `-p 1`) and §5 (i18n, MSW envelope) both followed |

---

## Edge Cases

- [x] Active-and-only-connected-provider disconnect still succeeds, leaves `active_provider: null` - `internal/db/email_provider_repository_test.go:230` and `internal/llm/service_test.go:409` both exercise exactly this (connect one, activate it, disconnect it)
- [x] Double-disconnect (idempotent, second call = 204 not error) - `TestDisconnect_NeverConnected_204Idempotent` / `TestLLMDisconnect_NeverConnected_204Idempotent` cover the underlying "delete zero rows = success" case a second call would hit
- [x] No active provider after disconnect -> existing `ErrNoActiveProvider` path untouched - `Service.Disconnect` (both email and llm) contains no changes to `SendAdminInvite`/`Send*` methods; `go test ./internal/email/... ./internal/llm/...` still green, confirming no regression to that pre-existing path

---

## Gate Check

- **Gate command**: `go build ./... && go test ./... && go vet ./... && gofmt -l <changed files>`, plus frontend `npx tsc -b --noEmit && npm run test`, plus `make test-integration`-equivalent (`TEST_DATABASE_URL=... go test -tags=integration -p 1 ./...` against a disposable container, port 5433, stopped after)
- **Result**:
  - `go build ./...`: clean, no output
  - `go vet ./...`: clean, no output
  - `gofmt -l` (feature's changed `.go` files): clean, no output
  - `go test ./...` (non-integration): all packages `ok` (`internal/api`, `internal/db`, `internal/email`, `internal/llm`, `internal/cli`, `internal/poller`, `internal/tls`, etc.)
  - Frontend `npx tsc -b --noEmit`: clean, no output
  - Frontend `npm run test -- --run`: 97/97 test files passed, 682/682 tests passed (matches T13's documented count exactly)
  - `go test -tags=integration -p 1 ./...` (disposable container, port 5433): **2 failures**, both pre-existing and already documented in `tasks.md` under T6/T8/T9:
    - `TestEmailProviderRepository_SetActiveProvider_UpdatesSingletonRow` (`internal/db`)
    - `TestLLMProviderRepository_SetActiveProvider_ThenGetActiveProvider_RoundTrips` (`internal/db`)
  - `internal/poller`'s previously-documented `TestPoller_PollOnce_HungLLMEnrichment_DoesNotDelayRemainingServices` and `internal/tls`'s `TestPostgresStorage_Lock_OutOfBandKill_AutoReleases` **passed cleanly** on this run (both timing/flakiness-sensitive tests per their own T9/T13 notes) - no new failures found, and no fewer failures than a flake would explain; matches the "not caused by this feature" conclusion already on record
- **Test count before feature**: not independently re-derived (would require checking out `627aec4` and re-running the full suite, out of scope for this validation pass) - deferred to the git history itself as the record
- **Test count after feature**: 682 frontend tests (exact); backend test count not tallied numerically but zero unexplained failures/skips
- **Delta**: net-new Disconnect-specific tests confirmed present in every listed file (repository x2, service x2, handler x2, audit integration x2, frontend hooks x2, frontend components x2)
- **Skipped tests**: none found
- **Failures**: the 2 pre-existing `internal/db` tenant-scoping leaks, both already documented and root-caused in `tasks.md` (T6) as unrelated to `GetActiveProvider`/`SetActiveProvider` code this feature did not touch

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| PROVDISC-01 | Pending | ✅ Verified |
| PROVDISC-02 | Pending | ✅ Verified |
| PROVDISC-03 | Pending | ✅ Verified |
| PROVDISC-04 | Pending | ✅ Verified |
| PROVDISC-05 | Pending | ✅ Verified |
| PROVDISC-06 | Pending | ✅ Verified |
| PROVDISC-07 | Pending | ✅ Verified |
| PROVDISC-08 | Pending | ✅ Verified |

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 19/21 ACs matched the spec-defined outcome directly; 2 spec-precision gaps flagged (email AC3's "unchanged, non-active provider" case has no direct positive test; both viewer-403 ACs lean on generic `RequireRole` middleware coverage rather than a Disconnect-specific 403 test - both are pre-existing conventions inherited from Connect/Activate, not regressions introduced here)

**Sensor**: 3/3 mutations killed

**Gate**: build/vet/gofmt/go test clean; frontend tsc + 682/682 tests clean; integration gate shows exactly the 2 pre-existing, already-documented `internal/db` failures and no new ones

**What works**: Both email and LLM Disconnect endpoints delete the row, clear `active_provider` via the existing FK when the deleted provider was active, are idempotent on a second/never-connected call, reject unknown provider names with 404, record a correctly-named audit entry, and are gated by `writeRoles`. The frontend button/dialog pair on both settings screens opens on click, makes no call on cancel, calls the mutation and refreshes the list on confirm, disables while pending, and surfaces an error message on failure without losing the connected row from the list.

**Issues found**: None requiring a fix task. Two spec-precision gaps noted above are process observations (test-design conventions this feature mirrored from Connect/Activate) rather than functional defects, and do not block PASS.

**Next steps**: None required for this feature. If the two spec-precision gaps are worth closing, a follow-up could add (a) a "disconnect the non-active provider, confirm the other one stays active" repository/service test, and (b) a Disconnect-specific viewer-403 handler test for both email and LLM - both would be small, additive, low-risk tasks, not required to ship.
