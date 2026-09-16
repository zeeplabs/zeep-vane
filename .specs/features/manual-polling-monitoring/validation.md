# Manual Polling Monitoring — Verification

**Verdict: PASS**

Verified after T7/T8/T9 (this session) on top of T1–T6 (prior session, commits
`e3fe8ec..f187804`). All 9 tasks committed; whole-feature Build gate
(`go build/vet/test`, `gofmt`, `make test-integration`-equivalent disposable-container
run, `tsc -b --noEmit`, `npm run test`) passes clean.

## 1. Spec-anchored outcome check (MP-01..MP-20)

| AC | Story | Evidence (file:line) | Notes |
| --- | --- | --- | --- |
| MP-01 | P1.1 | `internal/db/service_repository_test.go:306` `TestServiceRepository_Create_PollingMode_PersistsAllFourFieldsAndNilSLOID`; `internal/api/services_handler_test.go:387/425/450` (HTTP/TCP/Ping 201); `web/src/features/services/AddServiceDrawer.test.tsx:137` (P3 AC3 UI) | Full stack: DB → handler → drawer |
| MP-02 | P1.2 | `internal/api/services_handler_test.go:387` `TestCreateService_PollingMode_HTTP_201PersistsFields` (asserts `monitor_mode="polling"`, no `slo_id` required) | |
| MP-03 | P1.3 | `internal/api/services_handler_test.go:478` `TestCreateService_PollingMode_SSRFBlockedTarget_422NoServiceCreated`; `internal/checks/check_test.go` safety tests | |
| MP-04 | P1.4 | `internal/checks/format_test.go:8..62` (all 3 types, valid/invalid); `internal/api/services_handler_test.go:500` `TestCreateService_PollingMode_MalformedTarget_422NoServiceCreated` | |
| MP-05 | P1.5 | `internal/api/services_handler_test.go:521` `TestCreateService_PollingModeWithSLOID_422NoServiceCreated`; `:547` (slo-mode + poll fields rejected) | |
| MP-06 | P2.4 | `internal/poller/manual_scheduler_test.go:72` `TestManualScheduler_RunCheck_SuccessWritesOperationalAndResetsStreak`; `:134` (2nd failure → outage) | |
| MP-07 | P2.1 | `internal/checks/check_test.go` (`RunCheck` timeout param honored); `manual_scheduler_test.go:240` (check runs on interval) | |
| MP-08 | P2.2/P2.3 | `internal/checks/check_test.go` (HTTP 2xx/3xx success, other=fail; TCP/Ping dial success/fail) | |
| MP-09 | P2.9 | `internal/poller/manual_scheduler_test.go:205` `TestManualScheduler_Run_ZeroServices_StartsAndReturnsCleanly` | |
| MP-10 | P2.5 | `internal/poller/manual_scheduler_test.go:102` `TestManualScheduler_RunCheck_OneFailureCarriesStatusForward` | |
| MP-11 | P2.6 | `internal/poller/manual_scheduler_test.go:134` `TestManualScheduler_RunCheck_SecondConsecutiveFailureFlipsToOutage` | |
| MP-12 | P2.7 | `internal/poller/manual_scheduler_integration_test.go:45` (status_intervals row shape identical to Datadog poller's) | |
| MP-13 | P2.8 | `internal/cli/poller_manager_test.go:243` `TestPollerManager_RunLeaderLoop_ManualScheduler_StartsWithoutDatadogIntegration`; `:309` two-replica stop/failover test | Leader-only wiring, T7 |
| MP-14 | P2.9 (dup id) | Same as MP-09 (`Run_ZeroServices...`) | |
| MP-15 | P2.10 | `internal/checks/check_test.go:147` `TestRunCheck_RejectsLiteralBlockedTarget_ViaControlHook`; `manual_scheduler_test.go:178` `TestManualScheduler_RunCheck_BlockedRangeTarget_TreatedAsFailedCheck` | DNS-rebind-at-poll-time |
| MP-16 | P3.1 | `web/src/features/services/AddServiceDrawer.test.tsx:124` "defaults to 'Baseado em SLO'..." (P3 AC1) | |
| MP-17 | P3.2 | Same test (`:124`): Datadog chip active, New Relic chip present-but-disabled, SLO search visible | |
| MP-18 | P3.3 | `AddServiceDrawer.test.tsx:137` "selecting 'Polling manual' swaps in..." (P3 AC3); `:154` target label/placeholder switch | |
| MP-19 | P3.4 | `AddServiceDrawer.test.tsx:171` "clicking the disabled 'New Relic' chip..." (P3 AC4) | |
| MP-20 | P3.5 | `AddServiceDrawer.test.tsx:182` "submit is disabled until target+interval are filled..." (P3 AC5); toggle-back-to-SLO test at `:229` | |

Additional non-numbered but spec-mandated behavior:
- Goroutine-leak guard (Risks & Concerns): `internal/poller/manual_scheduler_test.go` `TestManualScheduler_Run_GoroutineCountReturnsToBaselineAfterCtxCancel`.
- T9's own display-fallback requirement (not a new MP-id, tracked under MP-01's "list/detail must render every service correctly"): `ServiceListPage.test.tsx:230/253`, `ServiceDetailDrawer.test.tsx:150/165`.
- `Restart` must not disturb an already-running manual scheduler (T7 design decision, no MP-id): `internal/cli/poller_manager_test.go` `TestPollerManager_Restart_DoesNotDisturbRunningManualScheduler`.

**Result: 20/20 ACs have real test evidence** (not just "code exists" — each test asserts the spec-defined outcome: persisted fields, HTTP status codes, status transitions, DOM state).

## 2. Discrimination sensor

Three mutations applied in isolation (file copied aside to a scratch dir first, mutated in place, relevant tests run, then the original restored from the scratch copy — never `git stash`). `git diff --stat` confirmed a clean tree after each restore.

| # | Mutation | File | Command | Result |
| --- | --- | --- | --- | --- |
| 1 | `manualFailureThreshold` 2 → 1 (hysteresis threshold) | `internal/poller/manual_scheduler.go` | `go test -tags=integration -run TestManualScheduler ./internal/poller/...` | **Killed** — `TestManualScheduler_RunCheck_OneFailureCarriesStatusForward` and `TestManualScheduler_RunCheck_BlockedRangeTarget_TreatedAsFailedCheck` both failed |
| 2 | Inverted `startManualScheduler`'s idempotency guard (`!= nil` → `== nil`), so it only "starts" when already running (never actually starts fresh) | `internal/cli/poller_manager.go` | `go test -tags=integration -run TestPollerManager ./internal/cli/...` | **Killed** — 4 tests failed: `StartsWithoutDatadogIntegration`, `ManualScheduler_StopsOnStop`, `StopsOnLeadershipLoss`, `Restart_DoesNotDisturbRunningManualScheduler` |
| 3 | Inverted `serviceSubtitle`'s mode check (`=== "polling"` → `!== "polling"`) | `web/src/features/services/statusMeta.ts` | `npx vitest run src/features/services/ServiceListPage.test.tsx src/features/services/ServiceDetailDrawer.test.tsx` | **Killed** — 4 tests failed (both the new polling-subtitle tests and the slo-mode regression tests, since the branches swapped) |

**Result: 3/3 mutations killed.** No surviving mutants found — no gap to close.

## 3. Whole-feature Build gate (re-confirmed after sensor restores)

- `go build ./...` — clean
- `go vet ./...` — clean
- `gofmt -l <changed .go files>` — no output
- `go test -tags=integration -p 1 ./...` against a disposable `postgres:16-alpine` container (port 5433, destroyed after each run) — all packages `ok`
- `cd web && npx tsc -b --noEmit` — clean
- `cd web && npm run test` — 81 test files, 472 tests passed

## Deviations / notes

- `internal/api/services_handler.go`'s `serviceResponse` (List/Get) did not include `monitor_mode`/`poll_type`/`poll_target`/`poll_interval_seconds` prior to T9 — only the *create request* type had them. This was a real gap: without it, the frontend's T9 display fallback would have had no real data to render outside MSW mocks. Closed as part of T9 (additive fields, existing `services_handler_test.go` JSON-decode tests unaffected since they decode into the same struct that gained fields).
- `TestPollerManager_RunLeaderLoop_ManualScheduler_StopsOnLeadershipLoss` (T7) uses a two-replica pattern (mirroring the pre-existing `LeaderBackendKilled_FailoverAndAbort` test) rather than a single-replica kill-and-observe: a single-instance version proved flaky by design, not by bug — the same manager instance re-acquires the just-freed lock and restarts its own manual scheduler within the same loop iteration (no delay imposed), often faster than a 20ms-interval polling assertion can observe the intermediate "stopped" state. Documented in the test's own comment.
- No task was left incomplete; no scope was cut to make a gate pass.
