# Validation: public-status-hourly-history

**Result: PASS**

Independent Verifier run. Author of the implementation != verifier of this document.

---

## 1. Spec-anchored outcome check (UPT-01..08)

| AC | Test(s) | Evidence |
| --- | --- | --- |
| UPT-01 (24 hourly bars, oldest→newest, ending at current hour) | `internal/history/hourly_test.go:TestBuildHourly_ReturnsWindowHoursBucketsOldestFirst` (hourly.go:29); `internal/api/public_status_handler_test.go:150` `TestPublicStatusGet_NoAuthHeader_200WithServiceStatus` (asserts `len(HourlyHistory) == historyWindowHours`, line 188); `web/.../PublicStatusPage.test.tsx:106` asserts `hourlyBars(...).toHaveLength(24)` (hard literal, independent of backend const) | Asserted value matches spec: exactly 24 buckets, strictly increasing `Start`, last bucket = current truncated local hour (`hourly_test.go:33-45`). |
| UPT-02 (green/yellow/red/gray mapping) | `hourly_test.go:TestBuildHourly_AllStatusValuesMapThrough` (operational/degraded/outage pass through untouched); `PublicStatusPage.tsx:31-35` `hourlyColorVar` maps all 4 statuses to distinct CSS vars; `PublicStatusPage.test.tsx:114` "cada status horário renderiza com a cor correspondente" | Confirmed 1:1 status→color, `no_data` included (not just the 3 "real" statuses). |
| UPT-03 (last-status-wins within an hour) | `hourly_test.go:TestBuildHourly_LastStatusWinsWithinBucket` — 3 conflicting snapshots for the same hour inserted out of chronological order; asserts the bucket resolves to `"outage"` (the one with latest `FetchedAt`), not first/worst/average | Assertion checks the *value*, not just presence — killed by the tie-break mutation below (discrimination sensor #1). |
| UPT-04 (America/Sao_Paulo hour boundaries) | `hourly_test.go:TestBuildHourly_BoundarySnapshotLandsInStartingBucket` (a snapshot exactly on the hour boundary, at a just-past-midnight São Paulo instant, lands in the hour it starts, not the previous one); `public_status_handler_test.go:TestPublicStatusGet_HourlyHistory_KnownHourStatusAppearsAsSingleBucket` computes `wantStart` independently via `time.LoadLocation("America/Sao_Paulo")` and asserts the outage bucket's `Start` matches exactly | Real end-to-end tz correctness, not just unit-level. |
| UPT-05 (hover/focus tooltip: date, hour range, PT-BR label) | `PublicStatusPage.test.tsx:137-148` "cada barra tem tooltip com data, hora e status em português" asserts the literal tooltip string `"24/08, 14h–15h · Degradado"` | Exact string match, not just "tooltip exists". `title` attribute + `tabIndex={0}` confirmed at `PublicStatusPage.tsx:264-270`. |
| UPT-06 (never-polled service → 24 `no_data` bars, not omitted) | `hourly_test.go:TestBuildHourly_EmptySnapshotsYieldsAllNoData`; `public_status_handler_test.go:261` `TestPublicStatusGet_ServiceWithNoSnapshotsEver_AllHourlyBucketsNoData`; preview counterpart `public_status_preview_handler_test.go:108` `TestPublicStatusPreview_ZeroSnapshotService_AllHourlyBucketsNoData`; `PublicStatusPage.test.tsx:152` "serviço sem dados renderiza 24 barras cinzas" | All three layers assert every one of the 24 buckets is `no_data`/gray, row still present (length == 24), not a missing row. |
| UPT-07 (no direct Datadog call to build history) | Static/grep check: `internal/api/public_status_handler.go`, `internal/db/status_snapshot_repository.go`, `internal/history/hourly.go` contain zero references to any Datadog client — `latestSnapshotFetcher` interface only exposes `LatestFetchedAtByService`/`ListRecentByServices` against `db.StatusSnapshotRepository` | Confirmed by direct source inspection; matches design.md's architecture diagram (`status_snapshots` table → repository → pure `BuildHourly`, no Datadog hop). |
| UPT-08 (preview endpoint shows identical composition) | `public_status_preview_handler_test.go:65` `TestPublicStatusPreview_AuthenticatedByID_200SameShapeAsProduction` asserts `len(HourlyHistory) == historyWindowHours` on the preview path; both `Get` handlers call the same `composeResponse` (`public_status_handler.go:151` and preview handler) — single code path, not two parallel implementations | Structural guarantee (shared function) + explicit assertion. |

All 8 ACs have tests that assert the actual spec-defined value (bucket count, exact status per bucket, exact tz-correct `Start`, exact tooltip string, no-Datadog-dependency), not merely "a test exists."

---

## 2. Gate results (all run for real, this session)

| Gate | Command | Result |
| --- | --- | --- |
| Build | `go build ./...` | PASS (no output) |
| Vet | `go vet -tags integration ./...` | PASS (no output) |
| Format | `gofmt -l .` | PASS (no files listed) |
| Go unit (history) | `go test ./internal/history/...` | PASS — 7/7 tests |
| Go integration | `TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable go test -tags integration -count=1 -p 1 ./internal/db/... ./internal/api/... ./internal/cli/... ./internal/poller/...` | PASS — `db`, `api`, `cli`, `poller` all green. No flake observed this run (the documented `TestDeleteAdmin_SelfRemovalAsLastOwner_409`-class flake did not manifest with `-p 1`). |
| Web typecheck | `cd web && npx tsc -b --noEmit` | PASS (no output) |
| Web tests | `cd web && npm run test` | PASS — 42 files / 166 tests, 0 failures |

---

## 3. Discrimination sensor (mutation testing)

Performed in an isolated scratch copy (`cp -r` to a scratch tmp directory, never `git stash`/in-place mutation). Copy discarded after each mutant; real working tree untouched (verified via `git status --porcelain` before/after — unrelated pre-existing untracked/modified files, `Makefile`/`README.md`/`.env.example`/`web/tsconfig.tsbuildinfo`, were already present before this verification session and were not touched by it).

| # | Mutation | Target | Expected to catch | Result |
| --- | --- | --- | --- | --- |
| 1 | `fetchedAtLocal.After(latestSeen[index])` → `.Before(...)` (flips last-status-wins tie-break, UPT-03) | `internal/history/hourly.go:52` | `go test ./internal/history/...` | **Killed** — `TestBuildHourly_LastStatusWinsWithinBucket` failed: got `"operational"`, want `"outage"`. |
| 2 | `const historyWindowHours = 24` → `12` | `internal/api/public_status_handler.go:19` | Any integration test | **Survived** — `go test -tags integration ./internal/api/...` still passed. Root cause: every Go-side assertion compares `len(HourlyHistory)` against the same package constant `historyWindowHours`, not a hardcoded literal `24`, so the comparison is self-referential and can't detect a regression to the constant itself. (The frontend Vitest suite does hardcode `24`, but it renders from an MSW fixture, not the live backend, so it can't catch a backend-side regression either.) |
| 3 | `if service.CurrentStatus == "not_configured" { continue }` → `!=` (inverts the not-configured filter, would flip which services get hidden vs. shown, and which get history queried) | `internal/api/public_status_handler.go:194` | Any integration test | **Killed** — 7 tests failed across both handler test files (`TestPublicStatusGet_NoAuthHeader_200WithServiceStatus`, `TestPublicStatusGet_HourlyHistory_KnownHourStatusAppearsAsSingleBucket`, `TestPublicStatusGet_ServiceWithNoSnapshotsEver_AllHourlyBucketsNoData`, `TestPublicStatusGet_IntegrationInvalid_StillServesLastSnapshot`, `TestPublicStatusGet_NotConfiguredService_HiddenValidServiceShown`, `TestPublicStatusPreview_AuthenticatedByID_200SameShapeAsProduction`, `TestPublicStatusPreview_ZeroSnapshotService_AllHourlyBucketsNoData`). |

2/3 mutants killed. Mutant #2 surviving is a real, if minor, gap — see below.

---

## 4. Accepted-gap regression check

- **No caching layer** (design.md "Risks & Concerns"): confirmed still absent — `composeResponse` calls `ListRecentByServices` on every request, no memoization/cache wrapper added. Matches the documented out-of-scope decision; not a regression.
- **tzdata footprint accepted**: `cmd/vane/main.go:5` still carries `_ "time/tzdata"`. Confirmed present and unchanged; `go build ./...` succeeds, `LoadLocation("America/Sao_Paulo")` is exercised and passes in both unit and integration tests.

---

## 5. Real gaps found (ranked)

1. **Medium — window-length regression not covered by any Go test.** All backend assertions of "24 buckets" compare against the `historyWindowHours` constant itself rather than a hardcoded `24`, so a future accidental edit to that constant (e.g. `24` → `12`) would build, vet, and pass every existing Go test untouched — only a human reading the diff would catch it. Confirmed by discrimination sensor #2. Fix: add one assertion (unit or integration) that hardcodes the literal `24`, decoupled from the constant, e.g. `if len(found.HourlyHistory) != 24 { ... }` in at least one `public_status_handler_test.go` case.
2. **Low — `ListRecentByServices` has no repository-level (`internal/db`) test of its own.** Tasks.md T1's "Done when" explicitly required "at least 3 new subtests (rows within window, rows outside window excluded, empty result)" directly against the repository method, matching the project's existing pattern (e.g. `status_page_repository_test.go`). No such file/test exists — `grep -rln "ListRecentByServices"` only finds it in the two production files (`status_snapshot_repository.go`, `public_status_handler.go`), never in a `_test.go`. The behavior is exercised indirectly and adequately through `internal/api` integration tests (which do cover in-window, out-of-window, and zero-snapshot cases end-to-end), so this is a process/traceability gap rather than a functional one — the task's own stated "done when" bar was not met, but coverage of the underlying behavior is not actually missing.

No other gaps found. Both items are test-coverage precision issues, not functional defects; the feature's actual behavior (bucketing, timezone, tooltip, no-Datadog guarantee, preview parity) is correctly implemented and correctly tested end-to-end.

---

## Fix → re-verify (iteration 1/3)

Both gaps closed, commit `08dd7e0`:

1. **Gap 1 (window-length regression) - fixed.** All 6 `len(found.HourlyHistory) != historyWindowHours` comparisons in `internal/api/public_status_handler_test.go` and `internal/api/public_status_preview_handler_test.go` changed to hardcode the literal `24`, decoupled from the `historyWindowHours` constant. Re-ran discrimination mutant #2 (`historyWindowHours = 24` → `12`) against this fix in an isolated scratch copy (`/tmp/verify-scratch`, discarded after): `go test -tags integration -count=1 -p 1 ./internal/api/...` now fails 5/5 affected tests with `len(HourlyHistory) = 12, want 24` - **mutant now killed**. Real working tree confirmed untouched by the scratch mutation (`git status --porcelain` before/after only shows the legitimate fix files).
2. **Gap 2 (missing `internal/db` repository-level tests) - fixed.** Added `internal/db/status_snapshot_repository_test.go` with 5 dedicated integration subtests against `ListRecentByServices` directly (exceeds T1's "at least 3" bar): row within window returned, row outside window excluded, empty `serviceIDs` returns empty not error, no-matching-service-id returns empty not error, and multi-service ordering (`service_id` then `fetched_at` ascending). All 5 pass: `TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable go test -tags integration -count=1 -p 1 ./internal/db/...` - PASS.

Full gate re-run after both fixes: `go build ./...`, `go vet -tags integration ./...`, `gofmt -l .` all clean; `TEST_DATABASE_URL=... go test -tags integration -count=1 -p 1 ./internal/db/... ./internal/api/...` - PASS, no regressions.

**Result: PASS** (unchanged verdict - both fixes closed real, if minor, gaps; no new issues introduced).
