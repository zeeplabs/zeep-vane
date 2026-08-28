# Accept Invite Page Validation

**Date**: 2026-08-28
**Spec**: `.specs/features/accept-invite-page/spec.md`
**Diff range**: `94db681..0684eed` (original feature `94db681..e507396` + fix commit `0684eed`)
**Verifier**: independent sub-agent (author ≠ verifier) — re-verification, iteration 2 of max 3

> Supersedes the iteration-1 report (FAIL, `94db681..e507396`). This is a full fresh
> re-derivation, not a delta: every AC, edge case, gate and sensor mutation was
> re-run against the current tree, not carried over.

---

## Task Completion

| Task | Status | Notes |
| ---- | ------ | ----- |
| T1 | ✅ Done | `internal/api/admins.go:284-294` issues the session and sets the cookie; `NewAdminsHandler` carries `sessionSecret`/`secureCookies` (`admins.go:48-49,57-65`). `admins_test.go:599-652` asserts cookie shape **and** `auth.VerifySession` → the new admin's own ID. 6 → 7 `TestAcceptInvite_*`, no deletions. Untouched by `0684eed`. |
| T2 | ✅ Done | `internal/cli/routes.go:75` passes `cfg.SessionSecret, cfg.SecureCookies`. Build gate green. Untouched by `0684eed`. |
| T3 | ✅ Done | `web/src/test/msw/handlers.ts:733-757` registers `POST /api/admins/invite/:token/accept` with the four documented shapes; `seedAdminInviteToken` at `handlers.ts:143-145`; state reset in `resetAdmins` (`handlers.ts:136`). Untouched by `0684eed`. |
| T4 | ✅ Done | Previously ⚠️ Partial. `0684eed` closed both unbacked Done-when criteria: "submit disabled while in flight" is now asserted mid-flight (`AcceptInvitePage.test.tsx:134`) and the stale-error clearing is now exercised by an actual second submit (`:141-157`). A dedicated AIP-04 test was added (`:159-184`). Both previously surviving mutants now die (sensor N1, N2 below). |

---

## Spec-Anchored Acceptance Criteria

### P1: Invited admin accepts their invite and lands logged in

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| ------------------------- | -------------------- | ----------------------- | ------ |
| AIP-01: WHEN the invitee submits a matching password THEN call `POST /api/admins/invite/{token}/accept` with `{"password": "<value>"}` | that exact URL + body | Now **direct**, not inferred: `web/src/features/auth/AcceptInvitePage.test.tsx:181` - `expect(requestUrl).toContain(`/api/admins/invite/${rawToken}/accept`)` against the real outgoing request captured at `:165-167`; body shape guarded by the MSW contract (401 on token mismatch `handlers.ts:750-752`, 422 on missing `password` `handlers.ts:739-741`) plus `AcceptInvitePage.test.tsx:51`. Backend: `internal/api/admins_test.go:609-612` - `if rec.Code != http.StatusCreated`. Sensor N5 killed. | ✅ PASS |
| AIP-02: WHEN the call responds `201` THEN full-page navigate to `/`, relying on the response's `Set-Cookie` to authenticate the boot check | `window.location.assign("/")` + a session cookie authenticating the new admin | `AcceptInvitePage.test.tsx:51` - `expect(assignSpy).toHaveBeenCalledWith("/")` (sensor N5 killed, 4 tests fail on `assign("/login")`); `internal/api/admins_test.go:648-652` - `if gotAdminID != created.ID` after `auth.VerifySession(...)`; cookie attributes at `admins_test.go:626-641` (`HttpOnly`, `Secure`, `SameSite=Strict`, `MaxAge == auth.SessionTTL`) | ✅ PASS |
| AIP-03: WHILE the request is in flight THEN disable the submit control and show a loading state, preventing a duplicate submission | submit control disabled *while* submitting | **Gap closed.** `AcceptInvitePage.test.tsx:117-126` holds the mock response open behind a manually-resolved promise (`responseGate`); `:134` - `await waitFor(() => expect(submitButton).toBeDisabled())` and `:135` - `expect(assignSpy).not.toHaveBeenCalled()` prove the disabled state is observed **mid-flight**, not inferred from eventual success; `:137-138` then resolves the gate and asserts `assign("/")`. Sensor N1 (delete `disabled={submitting}`) now **kills exactly this test**. | ✅ PASS |
| AIP-04: The system SHALL never display, log, or store the raw invite `token` beyond the URL path segment | no render/log/persist of the token | **Gap closed for display + transmit.** `AcceptInvitePage.test.tsx:159-184` renders with a distinctive token and asserts `:182` - `expect(JSON.stringify(requestBody)).not.toContain(rawToken)` and `:183` - `expect(document.body.textContent).not.toContain(rawToken)`. Non-shallow: verified by two dedicated mutations — N3 (render `{token}` in the `<h3>`) and N4 (add `token` to the request body) — each kills this test and only this test, so both negative assertions are load-bearing rather than vacuously true. | ✅ PASS (see residual note below) |

**Residual note on AIP-04 (non-blocking, Minor):** the AC's *"log"* sub-clause has no
assertion — the test spies no `console.*`. Code inspection of the whole diff surface
confirms zero `console.*` calls in `AcceptInvitePage.tsx` (grep-clean), and the two
reachable disclosure channels the code actually has (rendered DOM, request body) are
both asserted and both mutation-killed. Recorded as a residual, not a gap: there is no
code path that could log the token for a console spy to catch. Recommended (not
required) hardening: add `vi.spyOn(console, "error")`/`"log"` and assert it is never
called with `rawToken`, per lesson L-025.

### P1: Password confirmation prevents typo lockout

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| ------------------------- | -------------------- | ----------------------- | ------ |
| AIP-05: IF password and confirmation don't match at submit THEN block the request (never call the API) and show an inline mismatch message | no network call + inline mismatch text | `AcceptInvitePage.test.tsx:62-67` - `expect(await screen.findByRole("alert")).toHaveTextContent("As senhas não coincidem.")`, `expect(assignSpy).not.toHaveBeenCalled()`, and `expect(fetchSpy).not.toHaveBeenCalledWith(expect.stringContaining("/api/admins/invite/"), expect.anything())`. Sensor N6 killed. | ✅ PASS |
| AIP-06: WHEN the invitee edits either field after seeing the mismatch message THEN clear the message on the next submit attempt (not persist a stale error) | stale error gone on the next submit | **Gap closed.** `AcceptInvitePage.test.tsx:141-157` now performs a real second submit: mismatch → `:147` asserts the alert **is present** with the mismatch text → `:151-152` corrects the confirm field → `:153` resubmits → `:155` `expect(assignSpy).toHaveBeenCalledWith("/")` and `:156` `expect(screen.queryByRole("alert")).not.toBeInTheDocument()`. The "no alert" assertion **cannot** pass for the wrong reason: `:147`'s `findByRole("alert")` is a hard precondition proving the alert really rendered first, and sensor N6 (disabling the mismatch branch so the alert never renders) fails this test at `:147`. Sensor N2 (delete `setError(null)`) now **kills exactly this test**. | ✅ PASS |

### P2: Clear, distinct error messages for every accept failure

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| ------------------------- | -------------------- | ----------------------- | ------ |
| AIP-07: IF the backend responds `401` THEN show "This invite link is invalid or has expired. Ask your admin to send a new one." | that exact string, form still usable | `AcceptInvitePage.test.tsx:76-80` - `toHaveTextContent("Este link de convite é inválido ou expirou. Peça ao seu administrador para enviar um novo.")` + `expect(screen.getByRole("button", {name: "Ativar conta"})).not.toBeDisabled()`. (pt-BR rendering; `i18n.ts:114` carries the verbatim English.) | ✅ PASS |
| AIP-08: IF the backend responds `422` THEN show the server's exact `error` message verbatim | the server string, unmodified | `AcceptInvitePage.test.tsx:90-92` - `toHaveTextContent("password must be between 8 and 72 characters")`, verbatim match to `handlers.ts:743` | ✅ PASS |
| AIP-09: IF the request fails **below the HTTP layer** (`fetch()` itself rejects) THEN show a generic fallback, never a raw error object or blank screen | `acceptInvite.genericError` text | `AcceptInvitePage.test.tsx:101-110` - `server.use(http.post(..., () => HttpResponse.error()))` then `toHaveTextContent("Não foi possível ativar a conta. Tente novamente.")`. **Spec-precision gap now closed**: `spec.md:86` was reworded by `0684eed` to state that `apiFetch` (`web/src/lib/apiClient.ts:82-84`) converts every non-2xx — 5xx included — into a parsed `ApiError` handled by AC2's branch (`AcceptInvitePage.tsx:52`), so only a true network-level failure reaches `AcceptInvitePage.tsx:55`. Verified the correction propagated: the Independent Test line (`spec.md:88`) and Success Criteria (`spec.md:132`) were both amended in the same commit — no half-corrected restatement left behind (lesson L-013). | ✅ PASS |

**Status**: ✅ All 9 ACs covered with `file:line` evidence; 0 gaps; 0 spec-precision gaps. 1 non-blocking residual (AIP-04's "log" sub-clause).

---

## Discrimination Sensor

Scratch: `git worktree add <scratchpad>/mut2 HEAD` (detached at `0684eed`), `web/node_modules`
symlinked in, mutations applied and tested there, reverted with `git checkout --` between
runs, then `git worktree remove --force`. **No `git stash` used.**
Real-tree `git status --porcelain` baseline before sensor work: `M .specs/LESSONS.md`,
`M .specs/lessons.json`, `?? .specs/features/accept-invite-page/validation.md` — **identical
after cleanup**. Isolation verified. Test command per mutation:
`npx vitest run src/features/auth/AcceptInvitePage.test.tsx` (9 tests).

| Mutation | File:line | Description | Killed? |
| -------- | --------- | ----------- | ------- |
| N1 (re-test of iteration-1's surviving M6) | `AcceptInvitePage.tsx:155` | Removed `disabled={submitting}` from the submit `Button` | ✅ **Killed** — 1 failed / 8 passed; the failing test is exactly `…desabilitado durante a submissão…(AIP-03)` |
| N2 (re-test of iteration-1's surviving M8) | `AcceptInvitePage.tsx:27` | Removed `setError(null)` at the top of `handleSubmit` (stale error persists) | ✅ **Killed** — 1 failed / 8 passed; the failing test is exactly `mensagem de erro anterior some ao reenviar…(AIP-06)` |
| N3 (new — probes AIP-04's DOM assertion) | `AcceptInvitePage.tsx:127` | Leaked the token into the rendered page: `<h3>{t("acceptInvite.title")} {token}</h3>` | ✅ **Killed** — 1 failed / 8 passed; the AIP-04 test, proving `document.body.textContent` is load-bearing |
| N4 (new — probes AIP-04's body assertion) | `AcceptInvitePage.tsx:40` | Leaked the token into the request body: `JSON.stringify({ password, token })` | ✅ **Killed** — 1 failed / 8 passed; the AIP-04 test, proving the body assertion is load-bearing |
| N5 (regression re-test) | `AcceptInvitePage.tsx:48` | `window.location.assign("/")` → `assign("/login")` | ✅ **Killed** — 4 failed / 5 passed (AIP-01/02, AIP-03, AIP-06, AIP-04) — the rewritten tests did not weaken the navigation assertion; they broadened it |
| N6 (regression re-test) | `AcceptInvitePage.tsx:29` | `if (password !== confirmPassword)` → `if (false)` | ✅ **Killed** — 2 failed / 7 passed (AIP-05/06 and the new AIP-06 test) — also proves the AIP-06 test's "no alert" assertion cannot pass vacuously: with no alert ever rendered, it fails at its `findByRole("alert")` precondition |

Backend mutations M1/M2 from iteration 1 (cookie omitted; session issued for the inviter
instead of the invitee) were both killed then and the backend is byte-identical in this
range (`0684eed` touches only `spec.md` and `AcceptInvitePage.test.tsx`), so they were not
re-run.

**Sensor depth**: P0-full (auth / account-creation path) — 6 mutations this iteration, 8 in
iteration 1, all distinct behaviors covered.
**Result**: 6/6 killed — ✅ PASS

---

## Interactive UAT Results

Not performed — deferred to the orchestrator. Both closed gaps were automated-coverage
defects with objective pass/fail criteria (mutant killed / survived), not judgment calls.

---

## Code Quality

| Principle | Status |
| --------- | ------ |
| Minimum code | ✅ `0684eed` is test-and-spec only: **zero production-code lines changed** (`git show 0684eed --stat`: `spec.md` +31/-15 lines net of both files, `AcceptInvitePage.test.tsx` +66). The three surviving-behavior gaps were closed by strengthening assertions, not by rewriting the component — correct, since the component was already right. |
| Surgical changes | ✅ 2 files, both named in the fix plan; no drive-by edits elsewhere |
| No scope creep | ✅ No new helpers, no shared test utilities extracted for 2 call sites, no production refactor smuggled in under a test fix |
| Matches patterns | ✅ The gated-response technique uses the project's existing `server.use` + `HttpResponse` vocabulary; no new test library or abstraction introduced |
| Spec-anchored outcome check (asserted values match spec) | ✅ 9/9 — each AC's asserted value now matches the spec's stated outcome, including AIP-09 after the spec-wording correction |
| Per-layer Coverage Expectation met (domain 1:1 ACs; routes happy+edge+error) | ✅ Backend: happy path + all pre-existing error paths. Frontend: happy, mismatch, 401, 422, network-level failure, in-flight disabled, stale-error clearing, token non-leak, required fields — the Test Coverage Matrix's `AcceptInvitePage` row (`tasks.md:26`) is now fully satisfied, including its explicit "disabled-while-submitting" item |
| Every test maps to a spec requirement — no unclaimed tests | ✅ 9/9 map. The two previously **mis-claimed** tests are fixed: `:113` now actually asserts AIP-03, and AIP-06 has its own dedicated test at `:141` instead of riding on AIP-05's title |
| Documented guidelines followed | ✅ none dedicated — strong defaults applied (per `tasks.md:20`'s Test Coverage Matrix note) |

Non-blocking observations (carried over, unchanged, not regressions from this feature):
- `AcceptInvitePage.tsx:93-102` hardcodes pt-BR marketing copy outside i18n — matches `BootstrapPage.tsx`'s existing precedent.
- The MSW mock's single-use semantics (`handlers.ts:753`) correctly mirror the real `used_at` invariant.

---

## Edge Cases

- [x] **Empty/missing `:token` segment** (`spec.md:94`) — spec says submit it as-is and rely on the backend's 401. No direct test with an empty segment (React Router would not match `/accept-invite/` against `/accept-invite/:token` at all, so the state is unreachable through the app's own routing), but the equivalent "token the backend rejects" path is covered by `AcceptInvitePage.test.tsx:70-81`, which drives an unknown token to a 401 and the generic message — the exact behavior the spec prescribes. Accepted as covered by the unknown-token case.
- [x] **Double-click submit sends only one request** (`spec.md:95`) — the spec explicitly defers this to AIP-03's disabled-while-submitting state, which is now asserted mid-flight and mutation-killed (N1). With `disabled` set for the whole in-flight window, a second click cannot dispatch. Note: the test asserts the *mechanism* (disabled) rather than counting requests; a request-count assertion would be strictly stronger but is not required by the spec's own framing, which names the disabled state as the guarantee.
- [x] **Direct navigation (bookmark/shared URL) renders identically** — every test mounts the route directly via `MemoryRouter initialEntries={[/accept-invite/${token}]}` (`AcceptInvitePage.test.tsx:15`) with no referrer; the page renders and functions. Covered by construction.

---

## Gate Check

- **Gate command** (Build level, `tasks.md:38`): `gofmt -l . && go vet ./... && go test ./... && go test -tags=integration ./...` (with `TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable`) **and** `cd web && npx tsc -b --noEmit && npm run test` **and** `cd web && npm run build` **and** `make build`
- **Result**:
  - `gofmt -l .` — clean (no output); `go vet ./...` — clean
  - Go unit + integration — every package `ok`, exit 0. Go test functions under `internal/`: **448** (unchanged; `0684eed` touches no Go)
  - Web: `tsc -b --noEmit` clean; **206 passed / 206 (47 files)**, 0 failed, 0 skipped
  - `cd web && npm run build` — exit 0; `make build` — exit 0 (`go build -o bin/vane ./cmd/vane`, run from the repo root)
- **Test count before feature**: 447 Go + 197 web = 644
- **Test count after feature (this range)**: 448 Go + 206 web = **654**
- **Delta**: +10 new tests overall (+8 at iteration 1, +2 more in `0684eed`: the new AIP-06 and AIP-04 tests; the AIP-03 test was rewritten in place, not added). **0 deleted.**
- **Skipped tests**: none
- **Failures**: none. The known pre-existing `internal/db TestCompanySettingsMigration_AppliesClean_SeedsSingletonRow` flake did not reproduce.

**Test Integrity Check**: count increased (204 → 206 web) with zero deletions; no assertion
was weakened. The only rewritten test (AIP-03) is strictly **stronger** than before — it
retains its original `assignSpy` success assertion (`:138`) and adds the mid-flight
`toBeDisabled()` (`:134`) and `not.toHaveBeenCalled()` (`:135`) checks. Independently
confirmed by sensor N5, which the rewritten test now also fails.

---

## Fix Plans

All four fixes from the iteration-1 report are verified applied and effective:

| Iteration-1 fix | Status | Evidence |
| --------------- | ------ | -------- |
| Fix 1 — AIP-03 in-flight assertion (M6 survived) | ✅ Closed | `AcceptInvitePage.test.tsx:117-138`; mutant N1 now killed |
| Fix 2 — AIP-06 stale-error clearing (M8 survived) | ✅ Closed | `AcceptInvitePage.test.tsx:141-157`; mutant N2 now killed |
| Fix 3 — AIP-04 no evidence | ✅ Closed (display + transmit; "log" sub-clause a documented residual) | `AcceptInvitePage.test.tsx:159-184`; mutants N3, N4 killed |
| Fix 4 — spec.md AC3 lists an unreachable 5xx cause | ✅ Closed | `spec.md:86,88,132` all corrected in `0684eed` |

No new fix tasks. One optional hardening is recorded as the AIP-04 residual note above
(console spy); it blocks nothing.

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| ----------- | --------------- | ---------- |
| AIP-01 | ✅ Verified | ✅ Verified |
| AIP-02 | ✅ Verified | ✅ Verified |
| AIP-03 | ❌ Needs Fix | ✅ Verified |
| AIP-04 | ❌ Needs Fix | ✅ Verified |
| AIP-05 | ✅ Verified | ✅ Verified |
| AIP-06 | ❌ Needs Fix | ✅ Verified |
| AIP-07 | ✅ Verified | ✅ Verified |
| AIP-08 | ✅ Verified | ✅ Verified |
| AIP-09 | ✅ Verified | ✅ Verified |

---

## Summary

**Overall**: ✅ Ready

**Verdict**: PASS
**Spec-anchored check**: 9/9 ACs matched the spec-defined outcome; 0 gaps; 0 spec-precision gaps
**Sensor**: 6/6 mutations killed (including both mutants that survived iteration 1)
**Gate**: 654 passed, 0 failed, 0 skipped (Go unit + integration, web unit, both builds)

**What works**:
- Both iteration-1 Major gaps are genuinely closed, not papered over. The proof is not the
  new test names but the mutation results: deleting `disabled={submitting}` and deleting
  `setError(null)` each now fail exactly the test that claims that behavior — the precise
  pair that survived last round.
- The new AIP-04 test is non-shallow. Negative assertions are the easiest kind to write
  vacuously, so both were probed with dedicated mutations (N3 renders the token, N4 puts it
  in the body) and both mutations die. The DOM check and the body check each independently
  carry weight.
- The AIP-06 "no alert remains" assertion cannot pass for the wrong reason: it is preceded
  by a hard `findByRole("alert")` precondition, and N6 (which removes the alert entirely)
  fails the test at that precondition rather than sailing past the final assertion.
- The fix touched **zero production code** — the right call, since the component was already
  correct and only the tests were blind to it. Sensor N5/N6 confirm no pre-existing
  assertion was traded away in the rewrite.
- The spec correction was applied consistently across all three places the wrong mechanism
  was stated (AC3, Independent Test, Success Criteria), avoiding the half-corrected-spec
  trap recorded as lesson L-013.

**Issues found**: none blocking. One Minor residual: AIP-04's *"log"* sub-clause has no
assertion (no `console.*` spy). Code inspection is grep-clean of `console.*` on the diff
surface, and both reachable disclosure channels are asserted and mutation-killed, so this
is a hardening opportunity, not an uncovered behavior.

**Next steps**: Mark the feature done. Optionally add the console spy to
`AcceptInvitePage.test.tsx:159-184` when that file is next touched (lesson L-025). Lessons
L-023 through L-026 already capture every grounded failure from iteration 1; this iteration
produced no new grounded gap, so no new lesson is recorded.
