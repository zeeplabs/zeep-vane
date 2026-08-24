# Validation: poller-live-integration-detect

**Result: PASS**

**Diff range verified:** `f0ae01c..7f4993c` (`git show --stat 7f4993c`)
- `.specs/features/poller-live-integration-detect/spec.md` (+94, new)
- `internal/api/integrations_handler.go` (+26/-)
- `internal/api/integrations_handler_test.go` (+112/-)
- `internal/cli/poller_manager.go` (+95, new)
- `internal/cli/routes.go` (+2/-2)
- `internal/cli/routes_test.go` (+2/-1)
- `internal/cli/serve.go` (+16/-13, net +32/-23 incl. comments)

## Per-AC evidence

**PLD-01** (first connect starts poller, no restart): `internal/api/integrations_handler.go:114` — `ConnectDatadog` calls `h.poller.Restart(r.Context())` unconditionally after a successful `UpsertDatadog` (line 103). `internal/cli/poller_manager.go:48-73` — `Restart` builds+starts a fresh poller goroutine from whatever is now in the DB. Test: `TestConnectDatadog_ValidCredentials_RestartsPoller` (`integrations_handler_test.go:422-438`) asserts `poller.calls == 1` on a real first connect. PASS.

**PLD-02** (no integration stored → no poller): `internal/cli/poller_manager.go:58-60` — `Restart` returns `(false, nil)` when `newPollerFromStoredIntegration` reports `started=false` (i.e. `db.ErrNotFound`, `serve.go` diff, old behavior preserved verbatim in `newPollerFromStoredIntegration`, unchanged branch at `serve.go:190-`). `serve.go:76-80` logs "no datadog integration connected yet, poller not started" and does not start a goroutine. No regression — same `errors.Is(err, db.ErrNotFound)` short-circuit as before the fix. PASS.

**PLD-03** (integration already stored at boot → poller starts at boot, unchanged): `serve.go:73-80` — `pollerManager.Restart(ctx)` is called once at boot in place of the old one-shot `newPollerFromStoredIntegration` + manual goroutine wiring; `started=true` path in `poller_manager.go:62-72` spawns the goroutine exactly as `serve.go` used to inline. `internal/cli` integration suite (`routes_test.go`, `serve_test.go` if present) passed unchanged. PASS.

**PLD-04** (shutdown stops whichever poller is running): `serve.go:112-113` (server-error path) and `serve.go:125` (graceful path) both now call `pollerManager.Stop()` instead of `<-pollerDone`. `poller_manager.go:79-95` — `Stop`/`stopLocked` cancel the running poller's context and block on `<-m.done` until its `Run` goroutine actually returns, matching the old guarantee, now also covering a poller started post-boot via `ConnectDatadog`. PASS.

**PLD-05** (key rotation swaps client without restart): Same code path as PLD-01 — `ConnectDatadog` calls `Restart` on every successful `UpsertDatadog`, connect or rotate alike (no branch distinguishing first-connect from rotation in `integrations_handler.go`). `Restart` always tears down the old poller (`stopLocked`, line 52) before building a new one from the just-updated row, so a rotated key is picked up by construction. No dedicated rotation-specific test exists, but the code path is provably shared (single `if`-free call site) with the PLD-01 test. PASS, with the caveat that coverage is only direct for the connect case, not an actual rotate-after-connect sequence.

**PLD-06** (restart failure logged, response still 201): `integrations_handler.go:109-116` — the `Restart` error is only logged (`h.logger.Error(...)`), function falls through to write `201`. Test: `TestConnectDatadog_PollerRestartFails_StillReturns201` (`integrations_handler_test.go:441-464`) injects a `spyPollerRestarter` with a non-nil `err`, asserts `rec.Code == 201`, the integration row is persisted, and the failure was logged. PASS.

## Test suite results (real runs, not cached)

- `go build ./...` — clean (no output).
- `go vet ./...` — clean.
- `go vet -tags integration ./...` — clean.
- `gofmt -l .` — empty.
- `TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable go test -count=1 -race -tags integration ./internal/api/... ./internal/cli/... ./internal/poller/...` — all three packages `ok` (api 23.9s, cli 3.9s, poller 4.3s), including with `-race`.
- One flake observed on a single run of `./internal/api/...` under default parallelism: `TestUpdateAdminRole_SelfDemotionAsLastOwner_409` — unrelated to this feature (admin-role demotion test, not integrations/poller). Reran immediately after and it passed; not investigated further as it's out of scope (analogous to the documented pre-existing `internal/db` flake in `.specs/STATE.md`).
- `internal/db` flake: out of scope per task instructions, not run separately here.

## Discrimination sensor (isolated git worktree, cleaned up afterward)

Worktree created via `git worktree add` off `7f4993c`, never touching the real working tree. Confirmed via `git status --porcelain` diff before/after: identical.

| # | Mutant | File:line | Result | Evidence |
|---|---|---|---|---|
| A | `ConnectDatadog` never calls `h.poller.Restart` (call removed) | `internal/api/integrations_handler.go:114-116` | **Killed** | `TestConnectDatadog_ValidCredentials_RestartsPoller` fails (`poller.Restart() calls = 0, want 1`); `TestConnectDatadog_PollerRestartFails_StillReturns201` fails (no log entry found). |
| B | `PollerManager.Restart` made a no-op always returning `(false, nil)`, doing nothing | `internal/cli/poller_manager.go:48-73` | **Survived (gap)** | Full run of `./internal/api/... ./internal/cli/... ./internal/poller/...` (`-count=1`, one unrelated flake in `TestUpdateAdminRole_SelfDemotionAsLastOwner_409` reran clean) all passed with the mutant in place. Root cause: every test that exercises `ConnectDatadog`'s poller-restart behavior uses `spyPollerRestarter` (a test double implementing the `pollerRestarter` interface), never the real `*cli.PollerManager`. There is no test file for `internal/cli/poller_manager.go` at all — `grep -rn "PollerManager" internal/cli/*_test.go` only turns up its construction in `routes_test.go`, never a call to `.Restart()` against a real instance. |
| C | `stopLocked` no longer waits on `<-m.done` before returning (mutex kept, only the wait removed) | `internal/cli/poller_manager.go` (`stopLocked`) | **Survived (gap)** | Same full suite (including `-race`) still `ok` across all three packages. Confirms the task's suspicion: there is no test exercising concurrent/overlapping `Restart`/`Stop` calls against a real `PollerManager`, so the edge-case guarantee ("two pollers never run concurrently... wait for the in-flight Run goroutine to actually return") is asserted only by code comment, not by a test. |

## Findings (ranked by severity)

1. **(Medium) No direct test for `PollerManager` at all.** `internal/cli/poller_manager.go` — the component that actually owns start/stop/mutex/wait semantics — has zero dedicated tests. All handler-level tests substitute `spyPollerRestarter`, so a broken `Restart`/`Stop` implementation (mutants B and C) would ship undetected; the `internal/cli` integration suite only constructs a `PollerManager` to pass into `buildAdminRouter`, never calls `.Restart()`/`.Stop()` on it directly, and never with a real stored integration. Recommend a `poller_manager_test.go` covering: (a) `Restart` with no integration stored returns `(false, nil)` and starts nothing; (b) `Restart` with a stored integration actually starts a goroutine (observable via a poll happening or via a hook); (c) two sequential `Restart` calls tear down the first before starting the second; (d) two *concurrent* `Restart` calls are serialized (the mutex/wait-on-done edge case in the spec's "Edge Cases" section) — this is the one explicitly called out in the spec and is currently unverified by any test.
2. **(Low) PLD-05 (rotation) has no test performing an actual connect-then-rotate sequence.** Coverage relies on the shared code path with PLD-01's test rather than a rotation-specific scenario (e.g. asserting `poller.calls == 2` after two successive `ConnectDatadog` calls, or that the second call's `Restart` call happens after the row update). Low severity because the code path is structurally identical (single unconditional call, no first-connect/rotate branch), so the risk of divergence is low, but it's not empirically exercised.

Both gaps are pre-existing test-coverage omissions in the shipped fix, not correctness defects — `go build`, `go vet`, `gofmt`, and the full test suite (including `-race`) all pass, and AC PLD-01 through PLD-06 are each met by the current code with direct evidence above.

## Fix → re-verify (iteration 1/3)

Both findings addressed directly (author-applied, self-verified against the same mutants the Verifier used - not a fresh independent Verifier pass, since the overall verdict was already PASS and this is incremental coverage, not new behavior):

- **Finding 1 (Medium) — fixed.** Added `internal/cli/poller_manager_test.go` with 4 tests exercising a real `*PollerManager` (not the spy) against a real Postgres-backed integration row: `Restart` with a stored integration starts and tracks a running poller; `Restart` with none stored returns `(false, nil)`; `Stop` cancels and clears tracked state promptly; two sequential `Restart` calls close the first run's `done` channel before the second is tracked as running. All four use a 3600s poll interval so no test ever waits for a real tick or touches the network. Re-ran the exact **Mutant B** (no-op `Restart`) in an isolated `cp -r` scratch copy (never touching the real tree - `git status --porcelain` confirmed unchanged before/after): 3 of the 4 new tests now fail against it (`poller_manager_test.go:70,117,157`). **Mutant B is now killed.**
- **Finding 2 (Low) — fixed.** Added `TestConnectDatadog_RotateKey_RestartsPollerAgain` (`internal/api/integrations_handler_test.go`) - two successive `POST /api/integrations/datadog` calls with different valid credentials, asserting `poller.calls == 2`. Directly exercises the connect-then-rotate sequence PLD-05 describes, not just the shared-code-path inference.
- **Mutant C** (`stopLocked` skips `<-m.done`) - re-tested against the new suite in the same isolated-scratch manner: **still survives** (5/5 repeated runs green with the mutant in place). This is a genuine, currently-unclosed residual gap, not fixed by the above: `ctx` cancellation makes `poller.Run` return near-instantly regardless of poll interval, so a black-box timing assertion can't distinguish "waited for done" from "didn't wait" unless the poller is actually busy (e.g. mid network call) at the moment of cancellation - which isn't reproducible deterministically without either real Datadog network access (unavailable/undesirable in this sandbox and in CI) or refactoring `newPollerFromStoredIntegration`/`Poller` to accept an injectable, artificially-slow stand-in (a larger change than this fix's approved scope). Documented here rather than claimed as closed. Real-world impact is narrow: the only way this manifests is two `Restart` calls landing while a fetch is genuinely in-flight against Datadog, and the worst case is a duplicate/overlapping poll cycle (an extra snapshot write), not data corruption or a crash.

**Updated verdict: PASS**, with one explicitly accepted, documented residual gap (Mutant C / concurrent-teardown timing) left for a future round if it's ever prioritized - not a blocker for this fix, which addresses the actually-reported bug (services stuck on "não configurado") end to end.

Confirmed clean after the fixes: `gofmt -l .` empty, `go vet ./...` and `go vet -tags integration ./...` clean, full suite (`api`, `cli`, `poller`, `-race -count=1`) green including the 5 new tests.
