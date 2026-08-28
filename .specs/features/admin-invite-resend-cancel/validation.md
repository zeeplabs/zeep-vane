# Admin Invite Resend/Cancel Validation

**Date**: 2026-08-28
**Spec**: `.specs/features/admin-invite-resend-cancel/spec.md`
**Diff range**: `fa661cc..7bea522` (10 commits, 14 files) — feature range `fa661cc..f3a3241` plus fix commit `7bea522`
**Verifier**: independent sub-agent (author ≠ verifier), evidence-or-zero
**Iteration**: 2 of max 3 (re-verification after fix commit `7bea522`)

**Overall verdict**: ✅ PASS — all 4 gaps from iteration 1 are closed with cited evidence and empirically re-proven by the discrimination sensor (7/7 mutants killed, including the exact mutation that survived in iteration 1). One non-blocking test-hygiene observation is recorded below.

---

## Task Completion

| Task | Status | Notes |
| ---- | ------ | ----- |
| T1 `Refresh`/`Cancel` on `AdminInviteRepository` | ✅ Done | `internal/db/admin_invites.go:128-170`. Done-when #2 ("both return `ErrNotFound` for unknown / already-accepted / already-canceled") is now fully evidenced for **both** methods: `Refresh` at `internal/db/admin_invites_test.go:340,349,373`; `Cancel` at `:406` (already-accepted), `:429` (already-canceled), `:485` (unknown). Iteration 1's repo-layer blind spot is gone — sensor M7 (dropping `Cancel`'s `AND used_at IS NULL`) now dies inside `internal/db` alone. |
| T2 `List` drops expiry filter | ✅ Done | `internal/db/admin_invites.go:188-194`; test renamed + inverted assertion at `internal/db/admin_invites_test.go:214,279,296`. |
| T3 Wire `SendAdminInvite` into `Invite` | ✅ Done | Wiring at `internal/api/admins.go:68-92,200`; the email's **content** is now asserted — see INVITE-01 AC1 below. |
| T4 `ResendInvite` | ✅ Done | `internal/api/admins.go:293-329`; 8 tests (`internal/api/admins_test.go:1200,1273,1302,1317,1342,1374,1420,1445` region). |
| T5 `CancelInvite` | ✅ Done | `internal/api/admins.go:337-362`; 4 tests at `internal/api/admins_test.go:1440,1484,1495,1520` region. |
| T6 `expired` flag on `List` | ✅ Done | `internal/api/admins.go:380,583`; tests at `internal/api/admins_test.go:1534,1587` region. |
| T7 Routes + constructor call site | ✅ Done | `internal/cli/routes.go:134-135` (both `ownerOnly`); role-auth rows at `internal/cli/routes_test.go:375-387`. |
| T8 Frontend wiring | ✅ Done | `web/src/features/admins/AdminsPage.tsx:137-186`, `hooks.ts:11,15-18,35,70`; tests at `AdminsPage.test.tsx:130,142,154`, `hooks.test.ts:55,68,75,90`. Untouched by `7bea522`; iteration-1 evidence still holds (web suite re-run green). |
| Fix 1 (Blocker) — assert email recipient/role/raw token | ✅ Done | `internal/api/admins_test.go:70-80` (`fakeEmailProvider.lastMessage`), `:340-350`, `:1250-1260`, helper `extractAcceptToken` at `:356-369`. |
| Fix 2 (Major) — malformed `{id}` ⇒ 404 | ✅ Done | `internal/db/admin_invites.go:113-121` (`isInvalidUUIDText`), wired at `:141` (`Refresh`) and `:159` (`Cancel`). |
| Fix 3 (Minor) — P2 AC2 test | ✅ Done | `internal/api/admins_test.go:1317`. |
| Fix 4 (Minor) — repo `Cancel` states + resend TTL value | ✅ Done | `internal/db/admin_invites_test.go:406,429`; TTL window at `internal/api/admins_test.go:1241-1246`. |

---

## Spec-Anchored Acceptance Criteria

### P1: Invite emails are actually delivered

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --------- | -------------------- | ----------------------- | ------ |
| AC1 — `POST /api/admins` calls `SendAdminInvite` with the invitee's email, role, and the **raw** token | Send invoked with **that** recipient, **that** role, and a working (unhashed) token in `AcceptURL` | `internal/api/admins_test.go:340` — `provider.lastMessage.To != email`; `:343` — `strings.Contains(provider.lastMessage.TextBody, db.RoleOperator)` (the text template renders `as a {{.Role}}`, `internal/email/templates/admin_invite.txt.tmpl:1`, so a wrong role removes the literal); `:346-349` — `extractAcceptToken(...)` pulled out of the sent body is POSTed to `AcceptInvite` and must return `201`, which only holds if `AcceptURL` carries the raw token | ✅ PASS (iteration-1 survivor now killed 3×: sensor M1/M2/M3) |
| AC2 — send succeeds ⇒ `201` + `{"status":"invited","email_sent":true}` | 201, exact body fields | `internal/api/admins_test.go:287` (`rec.Code != http.StatusCreated`), `:327` (`resp["status"] != "invited"`), `:330` (`resp["email_sent"] != true`) | ✅ PASS |
| AC3 — send fails ⇒ still `201` + `email_sent:false`, logged Error, no rollback | 201, `email_sent:false`, invite row intact | `internal/api/admins_test.go:371` onward (provider error) and the `ErrNoActiveProvider` variant — `rec.Code == 201`, `resp["email_sent"] == false`, `invite.usedAt == nil` | ✅ PASS |
| AC4 — raw token never in HTTP response body | no `token` key in body | `internal/api/admins_test.go:333` — `if _, hasToken := resp["token"]; hasToken { t.Error(...) }` | ✅ PASS |

### P1: Owner resends a pending invite

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --------- | -------------------- | ----------------------- | ------ |
| AC1 — new token, new `token_hash`, `expires_at = now()+1h`, email sent, `200` `{"status":"resent","email_sent":bool}` | 200 + exact body; old token dead, new token live; expiry exactly `now()+adminInviteTTL` | `internal/api/admins_test.go:1219` (200), `:1226` (`"resent"`), `:1229` (`email_sent==true`), `:1234` (old token ⇒ 401), `:1244` — `diff := gotExpiresIn - adminInviteTTL; diff < -2s \|\| diff > 2s` (**TTL value now asserted**), `:1250` (recipient), `:1254` (token differs from the pre-resend one), `:1257` (new token accepts ⇒ 201). Repo: `internal/db/admin_invites_test.go:322,325,331,334` | ✅ PASS (iteration-1 spec-precision gap closed; sensor M6) |
| AC2 — unknown / accepted / canceled id ⇒ `404`, no row altered | 404 | `internal/api/admins_test.go:1309` (unknown), `:1372` (already accepted); repo `internal/db/admin_invites_test.go:344,368,392` | ✅ PASS |
| AC3 — `"resent"` audit entry (actor = owner, target = invite id) | one row, action `resent`, actor id | `internal/api/admins_test.go:1262-1270` — `SELECT actor_id, action ... AND action = 'resent'`, then `gotActorID != inviter.ID` | ✅ PASS |
| AC4 — non-owner ⇒ `403` | 403 | `internal/cli/routes_test.go:375-381` + the operator/viewer loop asserting `rec.Code != http.StatusForbidden` | ✅ PASS |
| AC5 — resend racing cancel ⇒ exactly one 200, other 404 (resend/resend **not** required exclusive, per Assumptions) | exactly one success in resend/cancel; resend/resend: no corruption | `internal/db/admin_invites_test.go:470` — `successes != 1` for concurrent `Refresh`+`Cancel`; `internal/api/admins_test.go` concurrent-resend test — both 200, `pendingCount == 1` | ✅ PASS (documented, user-approved deviation verified as written) |

### P1: Owner cancels a pending invite

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --------- | -------------------- | ----------------------- | ------ |
| AC1 — `used_at = now()`, `200` `{"status":"canceled"}` | 200 + exact body; `used_at` set | `internal/api/admins_test.go:1458` (200), `:1465` region (`"canceled"`); repo `internal/db/admin_invites_test.go:483` — `got.UsedAt == nil` ⇒ error | ✅ PASS |
| AC2 — canceled token later submitted to `AcceptInvite` ⇒ `401` | 401 | `internal/api/admins_test.go:1470` — `acceptRec.Code != http.StatusUnauthorized` | ✅ PASS |
| AC3 — id not matching an unused invite ⇒ `404`, no row altered | 404 | `internal/api/admins_test.go:1495` (unknown), `:1520` region (already canceled); repo `:406,429,485` | ✅ PASS |
| AC4 — `"canceled"` audit entry | one row, action `canceled`, actor id | `internal/api/admins_test.go:1475` region; no-duplicate on failed second cancel (`canceledCount != 1`) | ✅ PASS |
| AC5 — non-owner ⇒ `403` | 403 | `internal/cli/routes_test.go:382-387` | ✅ PASS |

### P2: Expired-but-unused invites remain manageable

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --------- | -------------------- | ----------------------- | ------ |
| AC1 — `GET /api/admins` includes every `used_at IS NULL` invite regardless of expiry, tagging `expires_at <= now()` with `"expired": true` | expired invite present with `expired:true`; fresh one without | `internal/api/admins_test.go:1534,1587` regions — expired row present and `item.Expired == true`, used invite excluded, fresh invite `Expired == false`, active admins never flagged. Repo `internal/db/admin_invites_test.go:296`. Frontend `web/src/features/admins/AdminsPage.test.tsx:161,165` | ✅ PASS |
| AC2 — resending an already-expired invite behaves identically to a fresh one | 200 + expiry pushed into the future | `internal/api/admins_test.go:1317` — `TestResendInvite_AlreadyExpiredInvite_200_RefreshesExpiry`, seeds `createTestInvite(..., -1*time.Hour)`, asserts `:1333` `rec.Code == 200` and `:1337` `got.expiresAt.After(time.Now())` | ✅ PASS (iteration-1 gap closed) |

### Edge Cases

| Edge case | Spec-defined outcome | Evidence | Result |
| --------- | -------------------- | -------- | ------ |
| Email provider times out / errors ⇒ `email_sent:false`, never 500 | 201/200 with `email_sent:false` | `internal/api/admins_test.go:371` (invite) and `:1273` (resend) regions | ✅ PASS |
| `{id}` in resend/cancel is not a valid UUID ⇒ `404` | 404 | **Now correct.** `internal/db/admin_invites.go:118-121` — `isInvalidUUIDText` maps SQLSTATE `22P02` (`pgerrcode.InvalidTextRepresentation`) to `ErrNotFound`, wired into `Refresh` (`:141`) and `Cancel` (`:159`), so both handlers take their existing 404 branch. Handler tests: `internal/api/admins_test.go:1342` (`TestResendInvite_MalformedID_404`, asserts `:1349` `rec.Code == 404`) and `:1484` (`TestCancelInvite_MalformedID_404`, asserts `:1491`). Repo tests: `internal/db/admin_invites_test.go:397,452` | ✅ PASS (iteration-1 gap closed; sensor M4/M5) |
| Resend/cancel on an already-accepted invite ⇒ `404`, no "already accepted" special-casing | 404 | `internal/api/admins_test.go:1372`; repo `internal/db/admin_invites_test.go:368` (Refresh), `:406` (Cancel) | ✅ PASS |

**Status**: ✅ All 20 ACs + 3 edge cases covered and matched to their spec-defined outcome. 0 gaps, 0 spec-precision gaps.

---

## Discrimination Sensor

Scratch: `git worktree add <scratchpad>/sensor HEAD`, all mutations applied and reverted there, then `git worktree remove --force`. Real-tree `git status --porcelain` baseline (`M .specs/LESSONS.md`, `M .specs/features/admin-invite-resend-cancel/spec.md`, `M .specs/lessons.json`, `?? .specs/features/admin-invite-resend-cancel/validation.md`) was **byte-identical before and after** the sensor run. No `git stash` used. All runs `-count=1` (no cached results).

| # | File:line | Mutation | Tests run | Killed? |
| - | --------- | -------- | --------- | ------- |
| M1 | `internal/api/admins.go:87` | `sendAdminInviteEmail`: recipient `to` → `"attacker@example.com"` | `-run 'TestInviteAdmin\|TestResendInvite' ./internal/api/...` | ✅ Killed — `admins_test.go:341` **and** `:1251` both fail |
| M2 | `internal/api/admins.go:82` | `Role: role` → `Role: "owner"` (hardcoded) | same | ✅ Killed — `admins_test.go:344`: `TextBody = "...as a owner..."`, want it to mention `"operator"` |
| M3 | `internal/api/admins.go:83` | `AcceptURL` embeds `hashAdminInviteToken(rawToken)` instead of the raw token | same | ✅ Killed — `admins_test.go:349` and `:1259`: accepting the emailed token returns 401, want 201 |
| M4 | `internal/db/admin_invites.go:120` | `isInvalidUUIDText`: compare against `pgerrcode.NoData` instead of `InvalidTextRepresentation` (guard never fires; still compiles, still uses the import) | `-run MalformedID ./internal/api/... ./internal/db/...` | ✅ Killed — all 4 malformed-id tests fail (`admins_test.go:1349,1491` → 500 want 404; `admin_invites_test.go:402,457` → wrapped 22P02 want `ErrNotFound`) |
| M5 | `internal/db/admin_invites.go:159-161` | Removed the `isInvalidUUIDText` branch from `Cancel` only (left `Refresh`'s intact) | same | ✅ Killed — `admins_test.go:1491` and `admin_invites_test.go:457` fail; `Refresh` side stays green, proving the two guards are independently covered |
| M6 | `internal/api/admins.go:308` | `Refresh(..., time.Now().Add(adminInviteTTL))` → `adminInviteTTL*2` | `-run TestResendInvite ./internal/api/...` | ✅ Killed — `admins_test.go:1245`: `expires_at - now() = 1h59m59s, want 1h0m0s (±2s)` |
| M7 | `internal/db/admin_invites.go:156` | `Cancel`: dropped `AND used_at IS NULL` from the UPDATE | `-run TestAdminInviteRepository_Cancel ./internal/db/...` | ✅ Killed **at the repository layer** — `admin_invites_test.go:425,448`. In iteration 1 this same mutation left `internal/db` green (only the API layer caught it); the blind spot is closed. |

**Sensor depth**: P0-full (7 mutations — auth/invite path; ≥5 required, all branches of the fixed code plus a re-run of iteration 1's survivor)
**Result**: 7/7 killed — ✅ PASS

Iteration 1's surviving mutant (recipient + role + hashed token, injected as one combined fault) was deliberately re-run here **split into three independent mutations** (M1, M2, M3), so each new assertion is individually shown to discriminate rather than being carried by one of its siblings.

---

## Interactive UAT Results

Not performed. The user-facing surface (two buttons + an "Expirado" tag) is unchanged by `7bea522` and is covered by behavioral component tests (`web/src/features/admins/AdminsPage.test.tsx:130,142,154`); no novel interaction pattern warranting human judgment.

---

## Code Quality

| Principle | Status |
| --------- | ------ |
| Minimum code | ✅ The fix adds exactly one 5-line predicate (`isInvalidUUIDText`) and one test helper (`extractAcceptToken`); everything else is assertions. No new endpoint, column, config, or abstraction. |
| Surgical changes | ✅ `7bea522` touches 3 files, all named in the fix plan: `internal/api/admins_test.go`, `internal/db/admin_invites.go`, `internal/db/admin_invites_test.go`. Production-code delta is 20 lines in one file. |
| No scope creep | ✅ No handler-side UUID parsing was added (the fix plan's alternative); the repo-layer SQLSTATE mapping keeps the `id`-shape contract in exactly one place, where the typed column lives. |
| Matches patterns | ✅ `isInvalidUUIDText` mirrors the existing `errors.Is(err, pgx.ErrNoRows) → ErrNotFound` mapping already used in the same file; the new repo tests mirror `Refresh`'s already-accepted/already-canceled pair verbatim. |
| Didn't "improve" unrelated code | ⚠️ Unchanged from iteration 1 and already accepted: `internal/cli/routes.go` reorders ~13 handler constructions (declaration-order necessity, behavior-neutral). `7bea522` itself touches nothing unrelated. |
| Would a senior engineer approve? | ✅ Yes. One non-blocking nit recorded below. |
| Tests map to ACs, non-shallow | ✅ Every new test names its requirement in the test name or a comment; the email-content assertions round-trip through the real `AcceptInvite` handler rather than string-matching a token the test already knows. |
| Spec-anchored outcome check | ✅ 20/20 — including the two previously vague ones (email content, resend TTL), both now asserting the spec's literal value. |
| Per-layer Coverage Expectation | ✅ Routes: happy + 403 + 404-unknown + 404-settled + 404-malformed + email-failure for both new routes. Domain: `Refresh` and `Cancel` each cover success, unknown, already-accepted, already-canceled, malformed, plus the concurrency race. |
| Every test maps to a spec requirement | ✅ All 7 tests added by `7bea522` map to the 4 iteration-1 fix tasks, which map to INVITE-01/03/05/07. No unclaimed tests. |
| Documented guidelines followed | ✅ none — no `AGENTS.md`/coverage config in this repo; strong defaults applied (as tasks.md's own matrix records). |

**Non-blocking observation (Minor, test hygiene — not a gate failure):** `TestInviteAdmin_Owner_201_CreatesInviteAndAuditEntry` (`internal/api/admins_test.go:347`) and `TestResendInvite_Owner_200_NewTokenWorksOldTokenRejected` (`:1257`) now complete a real invite acceptance, which **creates an `admins` row that neither test cleans up** — their `t.Cleanup` only runs `DELETE FROM admin_invites WHERE email = $1` (`:283`, `:1214`). The established pattern one screen away, `TestAcceptInvite_ValidToken_201_ActivatesAccountWithInvitedRole:560`, does register `DELETE FROM admins WHERE email = $1`. Verified harmless today: no assertion in `internal/api/admins_test.go` depends on an admin-table count (no `len(...)` assertions on list responses), and `CountActiveOwners` tests are baseline-relative and ignore `operator` rows (`internal/db/admin_repository_test.go:317`). But it leaks two rows per full-suite run into the shared integration database — the same class of shared-state accumulation behind the known `company_settings` flake. Suggested follow-up (does not block this feature): add the missing `DELETE FROM admins WHERE email = $1` cleanup to both tests.

**Design caveat (accepted, no action):** `isInvalidUUIDText` maps *any* `22P02` from those two statements to `ErrNotFound`. In practice only `$1` can produce it — `Refresh`'s other parameters are a Go `string` (`token_hash`, text column, cannot raise 22P02) and a `time.Time` (`expires_at`, a malformed value would raise `22007`, not `22P02`) — so the mapping cannot currently mask an unrelated fault. Worth revisiting only if either query gains another text-to-typed-column parameter.

---

## Edge Cases

- [x] Email provider times out/errors ⇒ `email_sent:false`, never 500 — covered at both invite and resend
- [x] Malformed (non-UUID) `{id}` on resend/cancel ⇒ 404 — **fixed and covered at both layers**
- [x] Resend/cancel on an already-accepted invite ⇒ 404, no special-casing — covered at both layers for both methods

---

## Gate Check

- **Gate command (Build)**: `gofmt -l . && go vet ./... && go test ./... && go test -tags=integration ./...` (with `TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable`), plus `cd web && npx tsc -b --noEmit && npm run test`, `cd web && npm run build`, `make build`

| Step | Result |
| ---- | ------ |
| `gofmt -l .` | ✅ clean (no output) |
| `go vet ./...` | ✅ clean |
| `go test ./...` (unit) | ✅ pass — no failures across all packages |
| `go test -tags=integration -count=1 ./...` | ⚠️ 536 run / **535 passed, 1 failed, 0 skipped** — the single failure is the documented pre-existing flake (below) |
| `npx tsc -b --noEmit` | ✅ clean |
| `npm run test` | ✅ 46 files, **197 tests passed, 0 failed, 0 skipped** |
| `npm run build` | ✅ built in 1.11s (`dist/assets/index-DNwh_TJd.js`, 425.91 kB) |
| `make build` | ✅ `go build -o bin/vane ./cmd/vane` succeeded |

**Failure triage — known pre-existing flake, NOT this feature:**
`internal/db TestCompanySettingsMigration_AppliesClean_SeedsSingletonRow`. Root-caused in iteration 1: the test asserts the shared `company_settings` singleton is blank without resetting it first, so leftover state from a concurrent package run makes it fail; the file is **not in this diff** and it was reproduced failing at the pre-feature baseline `fa661cc`. Re-confirmed here: it **passes in isolation** — `go test -tags=integration -count=1 ./internal/db/... -run TestCompanySettingsMigration` ⇒ `ok github.com/zeeplabs/zeep-vane/internal/db 0.403s`. A separate cached full-suite run in this same session also passed it. Already captured as lesson **L-022**. Not counted against this feature's gate.

**Test integrity:**

| Layer | Baseline `fa661cc` | Iteration 1 `f3a3241` | Now `7bea522` | Delta vs. baseline |
| ----- | ------------------ | --------------------- | ------------- | ------------------ |
| `internal/db/admin_invites_test.go` top-level `func Test` | 7 | 14 | **18** | +11 |
| `internal/api/admins_test.go` top-level `func Test` | 25 | 36 | **39** | +14 |
| `internal/cli` `adminManagementRouteCases` rows | 4 | 6 | 6 | +2 |
| Web suite total | 192 | 197 | 197 | +5 |

`7bea522` adds 7 tests (`TestResendInvite_AlreadyExpiredInvite_200_RefreshesExpiry`, `TestResendInvite_MalformedID_404`, `TestCancelInvite_MalformedID_404`, `TestAdminInviteRepository_Refresh_MalformedID_ErrNotFound`, `TestAdminInviteRepository_Cancel_{AlreadyAccepted,AlreadyCanceled,MalformedID}_ErrNotFound`) and **deletes none**. No assertion was weakened; two existing tests were *strengthened* (`TestInviteAdmin_Owner_201...` and `TestResendInvite_Owner_200...` each gained recipient/role/raw-token and, for resend, TTL-window assertions). The only signature change is `fakeEmailProvider.Send` now recording its argument (`internal/api/admins_test.go:76-79`) — additive.

---

## Fix Plans

None required. All four iteration-1 fix tasks are verified closed:

| Iteration-1 fix | Priority | Status |
| --------------- | -------- | ------ |
| Fix 1 — email recipient/role/raw token never asserted | Blocker | ✅ Closed — sensor M1/M2/M3 all killed |
| Fix 2 — malformed `{id}` returned 500 instead of 404 | Major | ✅ Closed — sensor M4/M5 killed; 4 new tests at both layers |
| Fix 3 — P2 AC2 (resend an expired invite) uncovered | Minor | ✅ Closed — `internal/api/admins_test.go:1317` |
| Fix 4 — repo `Cancel` settled-state blind spot + resend TTL unasserted | Minor | ✅ Closed — sensor M6/M7 killed |

Two non-blocking follow-ups, neither in scope for this feature (see Code Quality): the missing `DELETE FROM admins` cleanup in the two strengthened tests, and the pre-existing `company_settings` singleton flake (worth its own ticket).

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| ----------- | --------------- | ---------- |
| INVITE-01 | ❌ Needs Fix | ✅ Verified |
| INVITE-02 | ✅ Verified | ✅ Verified |
| INVITE-03 | ❌ Needs Fix | ✅ Verified |
| INVITE-04 | ✅ Verified | ✅ Verified |
| INVITE-05 | ❌ Needs Fix | ✅ Verified |
| INVITE-06 | ✅ Verified | ✅ Verified |
| INVITE-07 | ❌ Needs Fix | ✅ Verified |
| INVITE-08 | ✅ Verified | ✅ Verified |
| INVITE-09 | ✅ Verified | ✅ Verified |

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 20/20 ACs + 3/3 edge cases matched their spec-defined outcome; 0 gaps, 0 spec-precision gaps
**Sensor**: 7/7 mutations killed (P0-full depth)
**Gate**: 535/536 Go integration passed (1 pre-existing flake, passes isolated), all Go unit passed, 197/197 web passed, `gofmt`/`go vet` clean, both builds green

**What works**: `POST /api/admins` sends a real invite email whose recipient, role, and *working raw accept link* are now proven by round-tripping the emailed token back through `AcceptInvite`; atomic `Refresh`/`Cancel` with the correct `used_at IS NULL` guard and `ErrNotFound` semantics for unknown, already-accepted, already-canceled **and malformed** ids at both the repository and HTTP layers; resend mints a fresh token, kills the old one, and resets expiry to exactly `now()+adminInviteTTL`; cancel makes the token permanently un-acceptable; both routes are owner-gated and audited exactly once; expired-but-unused invites stay listed with `expired:true` and resend cleanly; the `email_sent` non-blocking contract holds in every failure mode; the frontend buttons and "Expirado" tag are wired and behaviorally tested.

**Issues found**: none blocking. One Minor test-hygiene nit (two strengthened tests leak an `admins` row each — verified harmless to current assertions) and one accepted design caveat (`22P02` mapping breadth), both documented above with follow-up suggestions.

**Next steps**: feature is done. Optionally file the two follow-ups (test cleanup nit; `company_settings` singleton reset) as separate tickets.
